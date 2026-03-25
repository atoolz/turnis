package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/atoolz/turnis/internal/escalation"
	"github.com/atoolz/turnis/internal/store"
)

func listPoliciesHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		policies, err := db.ListPolicies(r.Context())
		if err != nil {
			slog.Error("failed to list policies", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to list policies")
			return
		}
		if policies == nil {
			policies = []escalation.Policy{}
		}
		writeJSON(w, http.StatusOK, policies)
	}
}

func createPolicyHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input escalation.Policy
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

		policy, err := db.CreatePolicy(r.Context(), &input)
		if err != nil {
			slog.Error("failed to create policy", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to create escalation policy")
			return
		}

		writeJSON(w, http.StatusCreated, policy)
	}
}

func getPolicyHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		policyID := chi.URLParam(r, "policyID")

		policy, err := db.GetPolicy(r.Context(), policyID)
		if err != nil {
			slog.Error("failed to get policy", "error", err, "policy_id", policyID)
			writeError(w, http.StatusNotFound, "escalation policy not found")
			return
		}

		writeJSON(w, http.StatusOK, policy)
	}
}

func deletePolicyHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		policyID := chi.URLParam(r, "policyID")

		if err := db.DeletePolicy(r.Context(), policyID); err != nil {
			slog.Error("failed to delete policy", "error", err, "policy_id", policyID)
			writeError(w, http.StatusNotFound, "escalation policy not found")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
