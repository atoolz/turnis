package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/atoolz/turnis/internal/store"
)

func listIntegrationsHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		integrations, err := db.ListIntegrations(r.Context())
		if err != nil {
			slog.Error("failed to list integrations", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to list integrations")
			return
		}
		if integrations == nil {
			integrations = []store.Integration{}
		}
		writeJSON(w, http.StatusOK, integrations)
	}
}

func createIntegrationHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Name               string `json:"name"`
			TeamID             string `json:"team_id"`
			Type               string `json:"type"`
			EscalationPolicyID string `json:"escalation_policy_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if input.Name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
		if input.TeamID == "" {
			writeError(w, http.StatusBadRequest, "team_id is required")
			return
		}

		integration, err := db.CreateIntegration(r.Context(), input.Name, input.TeamID, input.Type, input.EscalationPolicyID)
		if err != nil {
			slog.Error("failed to create integration", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to create integration")
			return
		}

		if auditErr := db.RecordAudit(r.Context(), "", "integration.created", "integration", integration.ID, map[string]string{
			"name": integration.Name,
		}); auditErr != nil {
			slog.Error("failed to record audit for integration creation", "error", auditErr)
		}

		writeJSON(w, http.StatusCreated, integration)
	}
}

func deleteIntegrationHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		integrationID := chi.URLParam(r, "integrationID")

		if err := db.DeleteIntegration(r.Context(), integrationID); err != nil {
			slog.Error("failed to delete integration", "error", err, "integration_id", integrationID)
			writeError(w, http.StatusNotFound, "integration not found")
			return
		}

		if auditErr := db.RecordAudit(r.Context(), "", "integration.deleted", "integration", integrationID, nil); auditErr != nil {
			slog.Error("failed to record audit for integration deletion", "error", auditErr)
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
