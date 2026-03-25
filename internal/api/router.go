package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/slack-go/slack"

	"github.com/atoolz/turnis/internal/config"
	"github.com/atoolz/turnis/internal/escalation"
	"github.com/atoolz/turnis/internal/store"
)

func NewRouter(db *store.DB, cfg *config.Config, engine *escalation.Engine, slackClient *slack.Client) http.Handler {
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
			r.Get("/{teamID}", getTeamHandler(db))
			r.Delete("/{teamID}", deleteTeamHandler(db))
		})

		r.Route("/users", func(r chi.Router) {
			r.Get("/", listUsersHandler(db))
			r.Post("/", createUserHandler(db))
			r.Get("/{userID}", getUserHandler(db))
			r.Put("/{userID}", updateUserHandler(db))
			r.Delete("/{userID}", deleteUserHandler(db))

			r.Route("/{userID}/notification-rules", func(r chi.Router) {
				r.Get("/", listNotificationRulesHandler(db))
				r.Post("/", createNotificationRuleHandler(db))
				r.Delete("/{ruleID}", deleteNotificationRuleHandler(db))
			})
		})

		r.Route("/schedules", func(r chi.Router) {
			r.Get("/", listSchedulesHandler(db))
			r.Post("/", createScheduleHandler(db))
			r.Get("/on-call", whosOnCallHandler(db))
			r.Get("/{scheduleID}", getScheduleHandler(db))
			r.Post("/{scheduleID}/overrides", createOverrideHandler(db))
		})

		r.Route("/alerts", func(r chi.Router) {
			r.Post("/", ingestAlertHandler(db, cfg, engine))
			r.Get("/", listAlertsHandler(db))
			r.Post("/{alertID}/ack", ackAlertHandler(db, engine))
			r.Post("/{alertID}/resolve", resolveAlertHandler(db, engine))
		})

		r.Route("/integrations", func(r chi.Router) {
			r.Get("/", listIntegrationsHandler(db))
			r.Post("/", createIntegrationHandler(db))
			r.Delete("/{integrationID}", deleteIntegrationHandler(db))
		})

		r.Route("/escalation-policies", func(r chi.Router) {
			r.Get("/", listPoliciesHandler(db))
			r.Post("/", createPolicyHandler(db))
			r.Get("/{policyID}", getPolicyHandler(db))
			r.Delete("/{policyID}", deletePolicyHandler(db))
		})
	})

	// TwiML endpoints are called by Twilio when a voice call connects.
	// They rely on the 128-bit UUID alertID for access control.
	// TODO(#26): Add Twilio request signature validation when API auth lands.
	r.Get("/twiml/{alertID}", twimlHandler(db))
	r.Post("/twiml/{alertID}/gather", twimlGatherHandler(db, engine))

	r.Post("/webhook/{token}", webhookIngestHandler(db, cfg, engine))

	if slackClient != nil && cfg.Slack.SigningSecret != "" {
		r.Post("/slack/interactions", slackInteractionHandler(db, engine, slackClient, cfg.Slack.SigningSecret))
		r.Post("/slack/commands", slackCommandsHandler(db, engine, slackClient, cfg.Slack.SigningSecret))
	}

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
