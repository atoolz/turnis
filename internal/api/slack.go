package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/slack-go/slack"

	"github.com/atoolz/turnis/internal/escalation"
	"github.com/atoolz/turnis/internal/store"
)

func slackInteractionHandler(db *store.DB, engine *escalation.Engine, slackClient *slack.Client, signingSecret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Read body once for both signature verification and payload parsing.
		// This avoids depending on r.Body state after signature check.
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}

		if err := verifySlackSignature(signingSecret, r, body); err != nil {
			slog.Warn("slack signature verification failed", "error", err)
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}

		values, err := url.ParseQuery(string(body))
		if err != nil {
			http.Error(w, "invalid form body", http.StatusBadRequest)
			return
		}
		payloadStr := values.Get("payload")
		if payloadStr == "" {
			http.Error(w, "missing payload", http.StatusBadRequest)
			return
		}

		var callback slack.InteractionCallback
		if err := json.Unmarshal([]byte(payloadStr), &callback); err != nil {
			slog.Error("failed to parse slack interaction payload", "error", err)
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}

		if callback.Type != slack.InteractionTypeBlockActions {
			w.WriteHeader(http.StatusOK)
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
				if _, err := db.AcknowledgeAlert(r.Context(), alertID, callback.User.ID); err != nil {
					slog.Error("slack ack failed", "alert_id", alertID, "error", err)
					continue
				}
				engine.Acknowledge(alertID)
				statusText = fmt.Sprintf("\u2705 Acknowledged by @%s at %s", slackUser, now)
			} else if isResolve {
				if _, err := db.ResolveAlert(r.Context(), alertID); err != nil {
					slog.Error("slack resolve failed", "alert_id", alertID, "error", err)
					continue
				}
				engine.Resolve(alertID)
				statusText = fmt.Sprintf("\u2611\uFE0F Resolved by @%s at %s", slackUser, now)
			}

			updatedBlocks := ReplaceActionsWithStatus(callback.Message.Blocks.BlockSet, statusText)
			_, _, _, err := slackClient.UpdateMessageContext(
				r.Context(),
				callback.Channel.ID,
				callback.Message.Timestamp,
				slack.MsgOptionBlocks(updatedBlocks...),
			)
			if err != nil {
				slog.Error("failed to update slack message", "error", err)
			}
		}

		w.WriteHeader(http.StatusOK)
	}
}

// ReplaceActionsWithStatus replaces action blocks with a context block showing the given status text.
func ReplaceActionsWithStatus(blocks []slack.Block, statusText string) []slack.Block {
	var result []slack.Block
	for _, b := range blocks {
		if b.BlockType() == slack.MBTAction {
			result = append(result, slack.NewContextBlock("",
				slack.NewTextBlockObject(slack.MarkdownType, statusText, false, false),
			))
			continue
		}
		result = append(result, b)
	}
	return result
}

func verifySlackSignature(signingSecret string, r *http.Request, body []byte) error {
	timestamp := r.Header.Get("X-Slack-Request-Timestamp")
	if timestamp == "" {
		return fmt.Errorf("missing X-Slack-Request-Timestamp header")
	}

	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp: %w", err)
	}

	if math.Abs(float64(time.Now().Unix()-ts)) > 300 {
		return fmt.Errorf("request timestamp too old")
	}

	sigBaseString := fmt.Sprintf("v0:%s:%s", timestamp, string(body))
	mac := hmac.New(sha256.New, []byte(signingSecret))
	mac.Write([]byte(sigBaseString))
	expected := "v0=" + hex.EncodeToString(mac.Sum(nil))

	actual := r.Header.Get("X-Slack-Signature")
	if actual == "" {
		return fmt.Errorf("missing X-Slack-Signature header")
	}

	if !hmac.Equal([]byte(expected), []byte(actual)) {
		return fmt.Errorf("signature mismatch")
	}

	return nil
}
