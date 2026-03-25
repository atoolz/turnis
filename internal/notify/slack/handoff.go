package slack

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	slackapi "github.com/slack-go/slack"

	"github.com/atoolz/turnis/internal/schedule"
	"github.com/atoolz/turnis/internal/store"
)

// HandoffMonitor polls schedules and announces on-call handoffs in Slack.
type HandoffMonitor struct {
	store  *store.DB
	client *slackapi.Client

	mu          sync.Mutex
	lastOnCall  map[string]string // schedule ID -> user ID
	pollInterval time.Duration
}

// NewHandoffMonitor creates a new monitor that checks for on-call changes.
func NewHandoffMonitor(db *store.DB, client *slackapi.Client) *HandoffMonitor {
	return &HandoffMonitor{
		store:        db,
		client:       client,
		lastOnCall:   make(map[string]string),
		pollInterval: 60 * time.Second,
	}
}

// Run starts the polling loop. It blocks until the context is cancelled.
func (m *HandoffMonitor) Run(ctx context.Context) {
	slog.Info("handoff monitor started", "interval", m.pollInterval)

	ticker := time.NewTicker(m.pollInterval)
	defer ticker.Stop()

	// Run once immediately on startup to seed the lastOnCall map without announcing.
	m.seed(ctx)

	for {
		select {
		case <-ctx.Done():
			slog.Info("handoff monitor stopped")
			return
		case <-ticker.C:
			m.check(ctx)
		}
	}
}

// seed populates the initial on-call state without sending notifications.
func (m *HandoffMonitor) seed(ctx context.Context) {
	schedules, err := m.store.ListSchedules(ctx)
	if err != nil {
		slog.Error("handoff monitor: failed to list schedules for seeding", "error", err)
		return
	}

	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, s := range schedules {
		full, err := m.store.GetSchedule(ctx, s.ID)
		if err != nil {
			continue
		}
		overrides, err := m.store.GetOverrides(ctx, s.ID)
		if err != nil {
			continue
		}
		userID := schedule.WhosOnCall(full, overrides, now)
		if userID != "" {
			m.lastOnCall[s.ID] = userID
		}
	}
}

func (m *HandoffMonitor) check(ctx context.Context) {
	schedules, err := m.store.ListSchedules(ctx)
	if err != nil {
		slog.Error("handoff monitor: failed to list schedules", "error", err)
		return
	}

	now := time.Now()

	for _, s := range schedules {
		full, err := m.store.GetSchedule(ctx, s.ID)
		if err != nil {
			slog.Error("handoff monitor: failed to get schedule", "schedule_id", s.ID, "error", err)
			continue
		}

		overrides, err := m.store.GetOverrides(ctx, s.ID)
		if err != nil {
			slog.Error("handoff monitor: failed to get overrides", "schedule_id", s.ID, "error", err)
			continue
		}

		currentUserID := schedule.WhosOnCall(full, overrides, now)

		m.mu.Lock()
		if currentUserID == "" {
			delete(m.lastOnCall, s.ID)
			m.mu.Unlock()
			continue
		}
		previousUserID := m.lastOnCall[s.ID]
		changed := currentUserID != previousUserID
		m.lastOnCall[s.ID] = currentUserID
		m.mu.Unlock()

		if !changed {
			continue
		}

		// Find the Slack channel for the team
		channel := m.resolveChannel(ctx, s.TeamID)
		if channel == "" {
			slog.Debug("handoff monitor: no slack channel for team", "team_id", s.TeamID, "schedule", s.Name)
			continue
		}

		user, err := m.store.GetUser(ctx, currentUserID)
		if err != nil {
			slog.Error("handoff monitor: failed to get user", "user_id", currentUserID, "error", err)
			continue
		}

		untilStr := m.calculateUntilTime(full, now)
		userName := user.Name
		if user.SlackID != "" {
			userName = fmt.Sprintf("<@%s>", user.SlackID)
		}

		msg := fmt.Sprintf("%s is now on-call for %s until %s", userName, s.Name, untilStr)
		_, _, err = m.client.PostMessageContext(ctx, channel,
			slackapi.MsgOptionText(msg, false),
		)
		if err != nil {
			slog.Error("handoff monitor: failed to post handoff message", "channel", channel, "error", err)
		} else {
			slog.Info("handoff monitor: announced on-call change", "schedule", s.Name, "user", user.Name)
		}
	}
}

func (m *HandoffMonitor) resolveChannel(ctx context.Context, teamID string) string {
	if teamID == "" {
		return ""
	}
	team, err := m.store.GetTeam(ctx, teamID)
	if err != nil {
		return ""
	}
	return team.SlackChannel
}

// calculateUntilTime computes the next handoff time based on the schedule's rotation settings.
func (m *HandoffMonitor) calculateUntilTime(s *schedule.Schedule, now time.Time) string {
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		loc = time.UTC
	}
	localNow := now.In(loc)

	hour, minute := parseHandoffTime(s.HandoffTime)

	rotLen := s.RotationLength
	if rotLen <= 0 {
		rotLen = 1
	}

	var next time.Time
	switch s.RotationType {
	case schedule.RotationDaily:
		next = time.Date(localNow.Year(), localNow.Month(), localNow.Day(), hour, minute, 0, 0, loc)
		next = next.AddDate(0, 0, rotLen)
		if !next.After(localNow) {
			next = next.AddDate(0, 0, rotLen)
		}
	case schedule.RotationWeekly:
		if s.HandoffDay == "" {
			slog.Warn("weekly schedule has no handoff_day set, defaulting to Monday", "schedule_id", s.ID)
		}
		targetDay := parseWeekday(s.HandoffDay)
		next = time.Date(localNow.Year(), localNow.Month(), localNow.Day(), hour, minute, 0, 0, loc)
		for next.Weekday() != targetDay || !next.After(localNow) {
			next = next.AddDate(0, 0, 1)
		}
	default:
		next = time.Date(localNow.Year(), localNow.Month(), localNow.Day(), hour, minute, 0, 0, loc)
		next = next.AddDate(0, 0, rotLen)
		if !next.After(localNow) {
			next = next.AddDate(0, 0, rotLen)
		}
	}

	return next.Format("Monday 15:04")
}

func parseHandoffTime(t string) (int, int) {
	parts := strings.SplitN(t, ":", 2)
	if len(parts) != 2 {
		return 9, 0
	}
	var h, m int
	fmt.Sscanf(parts[0], "%d", &h)
	fmt.Sscanf(parts[1], "%d", &m)
	return h, m
}

func parseWeekday(day string) time.Weekday {
	days := map[string]time.Weekday{
		"sunday": time.Sunday, "monday": time.Monday, "tuesday": time.Tuesday,
		"wednesday": time.Wednesday, "thursday": time.Thursday,
		"friday": time.Friday, "saturday": time.Saturday,
	}
	if d, ok := days[strings.ToLower(day)]; ok {
		return d
	}
	return time.Monday
}
