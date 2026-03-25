package api

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/slack-go/slack"

	"github.com/atoolz/turnis/internal/escalation"
	"github.com/atoolz/turnis/internal/schedule"
	"github.com/atoolz/turnis/internal/store"
)

// SlackCommandRequest represents the parsed fields from a Slack slash command.
type SlackCommandRequest struct {
	Command     string
	Text        string
	UserID      string
	UserName    string
	ChannelID   string
	ResponseURL string
}

// ParseSlackCommand parses a slash command from form-encoded body bytes.
func ParseSlackCommand(body []byte) (SlackCommandRequest, error) {
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return SlackCommandRequest{}, fmt.Errorf("parsing form: %w", err)
	}

	return SlackCommandRequest{
		Command:     values.Get("command"),
		Text:        strings.TrimSpace(values.Get("text")),
		UserID:      values.Get("user_id"),
		UserName:    values.Get("user_name"),
		ChannelID:   values.Get("channel_id"),
		ResponseURL: values.Get("response_url"),
	}, nil
}

func slackCommandsHandler(db *store.DB, engine *escalation.Engine, slackClient *slack.Client, signingSecret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}

		if err := verifySlackSignature(signingSecret, r, body); err != nil {
			slog.Warn("slack command signature verification failed", "error", err)
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}

		cmd, err := ParseSlackCommand(body)
		if err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		HandleSlackCommand(r.Context(), db, engine, slackClient, cmd, w)
	}
}

// HandleSlackCommand executes the slash command logic. Shared between HTTP and Socket Mode.
func HandleSlackCommand(ctx context.Context, db *store.DB, engine *escalation.Engine, _ *slack.Client, cmd SlackCommandRequest, w http.ResponseWriter) {
	switch cmd.Command {
	case "/oncall":
		handleOnCall(ctx, db, cmd, w)
	case "/override":
		handleOverride(ctx, db, cmd, w)
	case "/ack":
		handleAck(ctx, db, engine, cmd, w)
	default:
		writeSlackEphemeral(w, fmt.Sprintf("Unknown command: %s", cmd.Command))
	}
}

func handleOnCall(ctx context.Context, db *store.DB, cmd SlackCommandRequest, w http.ResponseWriter) {
	schedules, err := db.ListSchedules(ctx)
	if err != nil {
		slog.Error("failed to list schedules", "error", err)
		writeSlackEphemeral(w, "Failed to fetch schedules.")
		return
	}

	if len(schedules) == 0 {
		writeSlackEphemeral(w, "No schedules configured.")
		return
	}

	now := time.Now()
	filter := strings.ToLower(cmd.Text)

	var lines []string
	for _, s := range schedules {
		if filter != "" && !strings.Contains(strings.ToLower(s.Name), filter) {
			continue
		}

		full, err := db.GetSchedule(ctx, s.ID)
		if err != nil {
			slog.Error("failed to get schedule", "schedule_id", s.ID, "error", err)
			continue
		}

		overrides, err := db.GetOverrides(ctx, s.ID)
		if err != nil {
			slog.Error("failed to get overrides", "schedule_id", s.ID, "error", err)
			continue
		}

		userID := schedule.WhosOnCall(full, overrides, now)
		if userID == "" {
			lines = append(lines, fmt.Sprintf("*%s*: no one on-call", s.Name))
			continue
		}

		user, err := db.GetUser(ctx, userID)
		if err != nil {
			lines = append(lines, fmt.Sprintf("*%s*: %s", s.Name, userID))
			continue
		}

		if user.SlackID != "" {
			lines = append(lines, fmt.Sprintf("*%s*: <@%s>", s.Name, user.SlackID))
		} else {
			lines = append(lines, fmt.Sprintf("*%s*: %s", s.Name, user.Name))
		}
	}

	if len(lines) == 0 {
		writeSlackEphemeral(w, "No matching schedules found.")
		return
	}

	writeSlackEphemeral(w, strings.Join(lines, "\n"))
}

func handleOverride(ctx context.Context, db *store.DB, cmd SlackCommandRequest, w http.ResponseWriter) {
	// Expected format: start @user until <time>
	text := cmd.Text

	if !strings.HasPrefix(strings.ToLower(text), "start ") {
		writeSlackEphemeral(w, "Usage: /override start @user until <time>")
		return
	}
	text = text[6:] // strip "start "

	untilIdx := strings.Index(strings.ToLower(text), " until ")
	if untilIdx < 0 {
		writeSlackEphemeral(w, "Usage: /override start @user until <time>")
		return
	}

	userPart := strings.TrimSpace(text[:untilIdx])
	timePart := strings.TrimSpace(text[untilIdx+7:])

	slackUserID := extractSlackUserID(userPart)
	if slackUserID == "" {
		writeSlackEphemeral(w, "Could not parse user mention. Use @user format.")
		return
	}

	endTime, err := parseFlexibleTime(timePart)
	if err != nil {
		writeSlackEphemeral(w, fmt.Sprintf("Could not parse time: %s", err))
		return
	}

	// Verify the calling user exists in Turnis and belongs to a team.
	callingUser, err := db.GetUserBySlackID(ctx, cmd.UserID)
	if err != nil {
		writeSlackEphemeral(w, "Your Slack account is not linked to a Turnis user.")
		return
	}

	user, err := db.GetUserBySlackID(ctx, slackUserID)
	if err != nil {
		writeSlackEphemeral(w, fmt.Sprintf("User <@%s> not found in Turnis.", slackUserID))
		return
	}

	if user.TeamID == "" {
		writeSlackEphemeral(w, "Cannot create override: target user has no team assigned.")
		return
	}

	// Only allow overrides for the calling user's own team.
	if callingUser.TeamID != user.TeamID {
		writeSlackEphemeral(w, "You can only create overrides for your own team's schedules.")
		return
	}

	schedules, err := db.ListSchedules(ctx)
	if err != nil || len(schedules) == 0 {
		writeSlackEphemeral(w, "No schedules found.")
		return
	}

	var targetSchedule *schedule.Schedule
	for _, s := range schedules {
		if s.TeamID == user.TeamID {
			targetSchedule = &s
			break
		}
	}
	if targetSchedule == nil {
		writeSlackEphemeral(w, "No schedule found for user's team.")
		return
	}

	now := time.Now().UTC()
	override, err := db.CreateOverride(ctx, targetSchedule.ID, user.ID, now, endTime, fmt.Sprintf("Slack override by %s", cmd.UserName))
	if err != nil {
		slog.Error("failed to create override", "error", err)
		writeSlackEphemeral(w, "Failed to create override.")
		return
	}

	msg := fmt.Sprintf("<@%s> is now on-call for *%s* until %s (override %s)",
		slackUserID, targetSchedule.Name, endTime.Format("Mon Jan 2 15:04 MST"), override.ID[:8])
	writeSlackInChannel(w, msg)
}

func handleAck(ctx context.Context, db *store.DB, engine *escalation.Engine, cmd SlackCommandRequest, w http.ResponseWriter) {
	alertID := strings.TrimSpace(cmd.Text)
	if alertID == "" {
		writeSlackEphemeral(w, "Usage: /ack <alert-id>")
		return
	}

	user, err := db.GetUserBySlackID(ctx, cmd.UserID)
	if err != nil {
		writeSlackEphemeral(w, "Your Slack account is not linked to a Turnis user.")
		return
	}

	_, err = db.AcknowledgeAlert(ctx, alertID, user.ID)
	if err != nil {
		slog.Error("failed to ack alert via slash command", "alert_id", alertID, "error", err)
		writeSlackEphemeral(w, "Failed to acknowledge alert. It may not exist or is not in firing status.")
		return
	}

	engine.Acknowledge(alertID)
	writeSlackEphemeral(w, fmt.Sprintf("Alert %s acknowledged.", alertID))
}

// extractSlackUserID extracts a user ID from Slack mention format.
// Handles: <@U12345|name>, <@U12345>, or raw U-prefixed ID.
func extractSlackUserID(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "<@") && strings.HasSuffix(s, ">") {
		inner := s[2 : len(s)-1]
		if pipeIdx := strings.Index(inner, "|"); pipeIdx >= 0 {
			return inner[:pipeIdx]
		}
		return inner
	}
	if strings.HasPrefix(s, "U") && len(s) >= 9 {
		return s
	}
	return ""
}

// parseFlexibleTime tries multiple time formats.
// For partial formats (no year, weekday-only), it resolves to the next future occurrence.
func parseFlexibleTime(s string) (time.Time, error) {
	now := time.Now()

	// Try "Monday 15:04" separately: resolve to next occurrence of that weekday.
	if t, err := time.Parse("Monday 15:04", s); err == nil {
		targetDay := t.Weekday()
		hour, min := t.Hour(), t.Minute()
		next := time.Date(now.Year(), now.Month(), now.Day(), hour, min, 0, 0, time.Local)
		for next.Weekday() != targetDay || !next.After(now) {
			next = next.AddDate(0, 0, 1)
		}
		return next, nil
	}

	// RFC3339 carries its own timezone offset, parse as-is.
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}

	// Full date formats without timezone: interpret in local time.
	localFormats := []string{
		"2006-01-02T15:04",
		"2006-01-02 15:04",
		"2006-01-02",
	}
	for _, f := range localFormats {
		if t, err := time.Parse(f, s); err == nil {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, time.Local)
			return t, nil
		}
	}

	// "Jan 2 15:04": inject current year, advance to next year if in the past.
	if t, err := time.Parse("Jan 2 15:04", s); err == nil {
		t = time.Date(now.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, time.Local)
		if !t.After(now) {
			t = t.AddDate(1, 0, 0)
		}
		return t, nil
	}

	// Relative durations: "4h", "30m"
	if d, err := time.ParseDuration(s); err == nil {
		return now.Add(d), nil
	}

	return time.Time{}, fmt.Errorf("unrecognized time format: %q", s)
}

func writeSlackEphemeral(w http.ResponseWriter, text string) {
	writeJSON(w, http.StatusOK, map[string]string{
		"response_type": "ephemeral",
		"text":          text,
	})
}

func writeSlackInChannel(w http.ResponseWriter, text string) {
	writeJSON(w, http.StatusOK, map[string]string{
		"response_type": "in_channel",
		"text":          text,
	})
}
