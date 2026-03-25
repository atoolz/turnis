package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/atoolz/turnis/internal/store"
)

func listNotificationRulesHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := chi.URLParam(r, "userID")

		rules, err := db.ListNotificationRules(r.Context(), userID)
		if err != nil {
			slog.Error("failed to list notification rules", "error", err, "user_id", userID)
			writeError(w, http.StatusInternalServerError, "failed to list notification rules")
			return
		}
		if rules == nil {
			rules = []store.NotificationRule{}
		}
		writeJSON(w, http.StatusOK, rules)
	}
}

func createNotificationRuleHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := chi.URLParam(r, "userID")

		var input struct {
			Channel   string `json:"channel"`
			Priority  int    `json:"priority"`
			StartTime string `json:"start_time"`
			EndTime   string `json:"end_time"`
			Timezone  string `json:"timezone"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if input.Channel == "" {
			writeError(w, http.StatusBadRequest, "channel is required")
			return
		}

		validChannels := map[string]bool{"slack": true, "sms": true, "voice": true, "push": true, "email": true, "webhook": true}
		if !validChannels[input.Channel] {
			writeError(w, http.StatusBadRequest, "invalid channel: must be one of slack, sms, voice, push, email, webhook")
			return
		}

		if input.Timezone != "" {
			if _, err := time.LoadLocation(input.Timezone); err != nil {
				writeError(w, http.StatusBadRequest, "invalid timezone")
				return
			}
		}

		if input.StartTime != "" || input.EndTime != "" {
			if !isValidHHMM(input.StartTime) || !isValidHHMM(input.EndTime) {
				writeError(w, http.StatusBadRequest, "start_time and end_time must both be HH:MM format (e.g., 09:00)")
				return
			}
		}

		rule, err := db.CreateNotificationRule(r.Context(), userID, input.Channel, input.Priority, input.StartTime, input.EndTime, input.Timezone)
		if err != nil {
			slog.Error("failed to create notification rule", "error", err, "user_id", userID)
			writeError(w, http.StatusInternalServerError, "failed to create notification rule")
			return
		}

		writeJSON(w, http.StatusCreated, rule)
	}
}

func deleteNotificationRuleHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ruleID := chi.URLParam(r, "ruleID")

		if err := db.DeleteNotificationRule(r.Context(), ruleID); err != nil {
			if strings.Contains(err.Error(), "not found") {
				writeError(w, http.StatusNotFound, "notification rule not found")
			} else {
				slog.Error("failed to delete notification rule", "error", err, "rule_id", ruleID)
				writeError(w, http.StatusInternalServerError, "failed to delete notification rule")
			}
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// isValidHHMM validates "HH:MM" format (00:00 to 23:59).
func isValidHHMM(s string) bool {
	if len(s) != 5 || s[2] != ':' {
		return false
	}
	for _, i := range []int{0, 1, 3, 4} {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	h := int(s[0]-'0')*10 + int(s[1]-'0')
	m := int(s[3]-'0')*10 + int(s[4]-'0')
	return h <= 23 && m <= 59
}
