package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/atoolz/turnis/internal/store"
)

func listTeamsHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		teams, err := db.ListTeams(r.Context())
		if err != nil {
			slog.Error("failed to list teams", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to list teams")
			return
		}
		if teams == nil {
			teams = []store.Team{}
		}
		writeJSON(w, http.StatusOK, teams)
	}
}

func createTeamHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Name         string `json:"name"`
			SlackChannel string `json:"slack_channel"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if input.Name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}

		team, err := db.CreateTeam(r.Context(), input.Name, input.SlackChannel)
		if err != nil {
			slog.Error("failed to create team", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to create team")
			return
		}

		if auditErr := db.RecordAudit(r.Context(), "", "team.created", "team", team.ID, map[string]string{
			"name": team.Name,
		}); auditErr != nil {
			slog.Error("failed to record audit for team creation", "error", auditErr)
		}

		writeJSON(w, http.StatusCreated, team)
	}
}

func getTeamHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		teamID := chi.URLParam(r, "teamID")

		team, err := db.GetTeam(r.Context(), teamID)
		if err != nil {
			slog.Error("failed to get team", "error", err, "team_id", teamID)
			writeError(w, http.StatusNotFound, "team not found")
			return
		}

		writeJSON(w, http.StatusOK, team)
	}
}

func deleteTeamHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		teamID := chi.URLParam(r, "teamID")

		if err := db.DeleteTeam(r.Context(), teamID); err != nil {
			slog.Error("failed to delete team", "error", err, "team_id", teamID)
			writeError(w, http.StatusNotFound, "team not found")
			return
		}

		if auditErr := db.RecordAudit(r.Context(), "", "team.deleted", "team", teamID, nil); auditErr != nil {
			slog.Error("failed to record audit for team deletion", "error", auditErr)
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
