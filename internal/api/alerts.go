package api

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/atoolz/turnis/internal/alert"
	"github.com/atoolz/turnis/internal/config"
	"github.com/atoolz/turnis/internal/store"
)

func ingestAlertHandler(db *store.DB, _ *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			IntegrationID string              `json:"integration_id"`
			Alert         alert.IncomingAlert `json:"alert"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if input.IntegrationID == "" {
			writeError(w, http.StatusBadRequest, "integration_id is required")
			return
		}
		if input.Alert.Title == "" {
			writeError(w, http.StatusBadRequest, "alert.title is required")
			return
		}

		if _, err := db.GetIntegration(r.Context(), input.IntegrationID); err != nil {
			if err == sql.ErrNoRows {
				writeError(w, http.StatusBadRequest, "integration not found")
			} else {
				slog.Error("failed to verify integration", "error", err)
				writeError(w, http.StatusInternalServerError, "failed to verify integration")
			}
			return
		}

		if input.Alert.Fingerprint != "" {
			existing, err := db.GetAlertsByFingerprint(r.Context(), input.IntegrationID, input.Alert.Fingerprint)
			if err != nil {
				slog.Error("failed to check deduplication", "error", err)
			} else if dup := alert.Deduplicate(existing, input.Alert); dup != nil {
				writeJSON(w, http.StatusOK, map[string]any{
					"deduplicated": true,
					"alert":        dup,
				})
				return
			}
		}

		a, err := db.CreateAlert(r.Context(), input.IntegrationID, input.Alert)
		if err != nil {
			slog.Error("failed to create alert", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to create alert")
			return
		}

		writeJSON(w, http.StatusCreated, a)
	}
}

func listAlertsHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := r.URL.Query().Get("status")
		integrationID := r.URL.Query().Get("integration_id")

		alerts, err := db.ListAlerts(r.Context(), status, integrationID)
		if err != nil {
			slog.Error("failed to list alerts", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to list alerts")
			return
		}
		if alerts == nil {
			alerts = []alert.Alert{}
		}
		writeJSON(w, http.StatusOK, alerts)
	}
}

func ackAlertHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		alertID := chi.URLParam(r, "alertID")

		var input struct {
			UserID string `json:"user_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if input.UserID == "" {
			writeError(w, http.StatusBadRequest, "user_id is required")
			return
		}

		a, err := db.AcknowledgeAlert(r.Context(), alertID, input.UserID)
		if err != nil {
			slog.Error("failed to acknowledge alert", "error", err, "alert_id", alertID)
			writeError(w, http.StatusNotFound, "alert not found or not in firing status")
			return
		}

		writeJSON(w, http.StatusOK, a)
	}
}

func resolveAlertHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		alertID := chi.URLParam(r, "alertID")

		a, err := db.ResolveAlert(r.Context(), alertID)
		if err != nil {
			slog.Error("failed to resolve alert", "error", err, "alert_id", alertID)
			writeError(w, http.StatusNotFound, "alert not found or already resolved")
			return
		}

		writeJSON(w, http.StatusOK, a)
	}
}

func webhookIngestHandler(db *store.DB, _ *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := chi.URLParam(r, "token")
		if token == "" {
			writeError(w, http.StatusUnauthorized, "missing webhook token")
			return
		}

		// Token validation is done by the SQL lookup (WHERE token = ?).
		// Timing attacks on webhook URLs are accepted risk since the token
		// is a 256-bit random value in the URL path.
		integration, err := db.GetIntegrationByToken(r.Context(), token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid webhook token")
			return
		}

		var incoming alert.IncomingAlert
		if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if incoming.Title == "" {
			writeError(w, http.StatusBadRequest, "title is required")
			return
		}

		if incoming.Fingerprint != "" {
			existing, err := db.GetAlertsByFingerprint(r.Context(), integration.ID, incoming.Fingerprint)
			if err != nil {
				slog.Error("failed to check deduplication", "error", err)
			} else if dup := alert.Deduplicate(existing, incoming); dup != nil {
				writeJSON(w, http.StatusOK, map[string]any{
					"deduplicated": true,
					"alert":        dup,
				})
				return
			}
		}

		a, err := db.CreateAlert(r.Context(), integration.ID, incoming)
		if err != nil {
			slog.Error("failed to create alert from webhook", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to create alert")
			return
		}

		writeJSON(w, http.StatusCreated, a)
	}
}
