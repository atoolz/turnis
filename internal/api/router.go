package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/atoolz/turnis/internal/config"
	"github.com/atoolz/turnis/internal/store"
)

func NewRouter(db *store.DB, cfg *config.Config) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Heartbeat("/healthz"))

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/status", statusHandler)

		r.Route("/teams", func(r chi.Router) {
			r.Get("/", listTeamsHandler(db))
			r.Post("/", createTeamHandler(db))
		})

		r.Route("/schedules", func(r chi.Router) {
			r.Get("/", listSchedulesHandler(db))
			r.Post("/", createScheduleHandler(db))
			r.Get("/on-call", whosOnCallHandler(db))
		})

		r.Route("/alerts", func(r chi.Router) {
			r.Post("/", ingestAlertHandler(db, cfg))
			r.Get("/", listAlertsHandler(db))
			r.Post("/{alertID}/ack", ackAlertHandler(db))
			r.Post("/{alertID}/resolve", resolveAlertHandler(db))
		})

		r.Route("/integrations", func(r chi.Router) {
			r.Get("/", listIntegrationsHandler(db))
			r.Post("/", createIntegrationHandler(db))
		})

		r.Route("/escalation-policies", func(r chi.Router) {
			r.Get("/", listPoliciesHandler(db))
			r.Post("/", createPolicyHandler(db))
		})
	})

	r.Route("/webhook/{token}", func(r chi.Router) {
		r.Post("/", webhookIngestHandler(db, cfg))
	})

	return r
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"service": "turnis",
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("failed to encode JSON response", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// Placeholder handlers - to be implemented

func listTeamsHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, []any{})
	}
}

func createTeamHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotImplemented, "not implemented yet")
	}
}

func listSchedulesHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, []any{})
	}
}

func createScheduleHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotImplemented, "not implemented yet")
	}
}

func whosOnCallHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotImplemented, "not implemented yet")
	}
}

func ingestAlertHandler(db *store.DB, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotImplemented, "not implemented yet")
	}
}

func listAlertsHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, []any{})
	}
}

func ackAlertHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotImplemented, "not implemented yet")
	}
}

func resolveAlertHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotImplemented, "not implemented yet")
	}
}

func listIntegrationsHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, []any{})
	}
}

func createIntegrationHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotImplemented, "not implemented yet")
	}
}

func listPoliciesHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, []any{})
	}
}

func createPolicyHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotImplemented, "not implemented yet")
	}
}

func webhookIngestHandler(db *store.DB, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotImplemented, "not implemented yet")
	}
}
