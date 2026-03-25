package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"

	"github.com/atoolz/turnis/internal/api"
	"github.com/atoolz/turnis/internal/escalation"
	"github.com/atoolz/turnis/internal/store"
)

// startSocketMode launches a Socket Mode listener that routes slash commands
// and interactions to the same handlers used by the HTTP endpoints.
func startSocketMode(ctx context.Context, botClient *slack.Client, _ string, db *store.DB, engine *escalation.Engine) {
	smClient := socketmode.New(botClient)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-smClient.Events:
				if !ok {
					return
				}
				handleSocketEvent(ctx, smClient, evt, botClient, db, engine)
			}
		}
	}()

	go func() {
		if err := smClient.RunContext(ctx); err != nil {
			// RunContext returns when context is cancelled, which is normal shutdown.
			if ctx.Err() == nil {
				slog.Error("socket mode error", "error", err)
			}
		}
	}()

	slog.Info("slack socket mode started")
}

func handleSocketEvent(ctx context.Context, sm *socketmode.Client, evt socketmode.Event, botClient *slack.Client, db *store.DB, engine *escalation.Engine) {
	switch evt.Type {
	case socketmode.EventTypeSlashCommand:
		data, ok := evt.Data.(slack.SlashCommand)
		if !ok {
			return
		}
		sm.Ack(*evt.Request)

		cmd := api.SlackCommandRequest{
			Command:     data.Command,
			Text:        strings.TrimSpace(data.Text),
			UserID:      data.UserID,
			UserName:    data.UserName,
			ChannelID:   data.ChannelID,
			ResponseURL: data.ResponseURL,
		}

		rw := &socketResponseWriter{}
		api.HandleSlackCommand(ctx, db, engine, botClient, cmd, rw)

		// Post the response back via response_url if we captured one
		if rw.body != nil && data.ResponseURL != "" {
			postToResponseURL(data.ResponseURL, rw.body)
		}

	case socketmode.EventTypeInteractive:
		callback, ok := evt.Data.(slack.InteractionCallback)
		if !ok {
			return
		}
		sm.Ack(*evt.Request)

		handleSocketInteraction(ctx, db, engine, botClient, callback)
	}
}

func handleSocketInteraction(ctx context.Context, db *store.DB, engine *escalation.Engine, slackClient *slack.Client, callback slack.InteractionCallback) {
	if callback.Type != slack.InteractionTypeBlockActions {
		return
	}

	for _, action := range callback.ActionCallback.BlockActions {
		actionID := action.ActionID
		slackUser := callback.User.Name

		var alertID string
		var isAck, isResolve bool

		if strings.HasPrefix(actionID, "ack_") {
			alertID = strings.TrimPrefix(actionID, "ack_")
			isAck = true
		} else if strings.HasPrefix(actionID, "resolve_") {
			alertID = strings.TrimPrefix(actionID, "resolve_")
			isResolve = true
		} else {
			continue
		}

		now := time.Now().UTC().Format("15:04")
		var statusText string

		if isAck {
			if _, err := db.AcknowledgeAlert(ctx, alertID, callback.User.ID); err != nil {
				slog.Error("socket mode ack failed", "alert_id", alertID, "error", err)
				continue
			}
			engine.Acknowledge(alertID)
			statusText = fmt.Sprintf("\u2705 Acknowledged by @%s at %s", slackUser, now)
		} else if isResolve {
			if _, err := db.ResolveAlert(ctx, alertID); err != nil {
				slog.Error("socket mode resolve failed", "alert_id", alertID, "error", err)
				continue
			}
			engine.Resolve(alertID)
			statusText = fmt.Sprintf("\u2611\uFE0F Resolved by @%s at %s", slackUser, now)
		}

		updatedBlocks := api.ReplaceActionsWithStatus(callback.Message.Blocks.BlockSet, statusText)
		_, _, _, err := slackClient.UpdateMessageContext(
			ctx,
			callback.Channel.ID,
			callback.Message.Timestamp,
			slack.MsgOptionBlocks(updatedBlocks...),
		)
		if err != nil {
			slog.Error("failed to update slack message via socket mode", "error", err)
		}
	}
}

// socketResponseWriter captures the JSON response that HandleSlackCommand writes,
// so we can forward it via response_url in Socket Mode.
type socketResponseWriter struct {
	statusCode int
	headers    http.Header
	body       []byte
}

func (w *socketResponseWriter) Header() http.Header {
	if w.headers == nil {
		w.headers = make(http.Header)
	}
	return w.headers
}

func (w *socketResponseWriter) Write(b []byte) (int, error) {
	w.body = append(w.body, b...)
	return len(b), nil
}

func (w *socketResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
}

func postToResponseURL(responseURL string, body []byte) {
	// Validate the URL to prevent SSRF. Slack response URLs are always
	// https://*.slack.com paths.
	u, err := url.Parse(responseURL)
	if err != nil || u.Scheme != "https" || !strings.HasSuffix(u.Host, ".slack.com") {
		slog.Warn("postToResponseURL: rejected non-Slack URL", "url", responseURL)
		return
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(responseURL, "application/json", strings.NewReader(string(body)))
	if err != nil {
		slog.Error("failed to post to slack response_url", "error", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		slog.Warn("postToResponseURL: non-OK response from Slack", "status", resp.StatusCode, "url", responseURL)
	}
}
