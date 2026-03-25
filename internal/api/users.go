package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/atoolz/turnis/internal/store"
)

func listUsersHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		teamID := r.URL.Query().Get("team_id")

		users, err := db.ListUsers(r.Context(), teamID)
		if err != nil {
			slog.Error("failed to list users", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to list users")
			return
		}
		if users == nil {
			users = []store.User{}
		}
		writeJSON(w, http.StatusOK, users)
	}
}

func createUserHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Name      string `json:"name"`
			Email     string `json:"email"`
			Phone     string `json:"phone"`
			SlackID   string `json:"slack_id"`
			NtfyTopic string `json:"ntfy_topic"`
			TeamID    string `json:"team_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if input.Name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
		if input.Email == "" {
			writeError(w, http.StatusBadRequest, "email is required")
			return
		}

		user, err := db.CreateUser(r.Context(), input.Name, input.Email, input.Phone, input.SlackID, input.NtfyTopic, input.TeamID)
		if err != nil {
			slog.Error("failed to create user", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to create user")
			return
		}

		if auditErr := db.RecordAudit(r.Context(), "", "user.created", "user", user.ID, map[string]string{
			"name":  user.Name,
			"email": user.Email,
		}); auditErr != nil {
			slog.Error("failed to record audit for user creation", "error", auditErr)
		}

		writeJSON(w, http.StatusCreated, user)
	}
}

func getUserHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := chi.URLParam(r, "userID")

		user, err := db.GetUser(r.Context(), userID)
		if err != nil {
			slog.Error("failed to get user", "error", err, "user_id", userID)
			writeError(w, http.StatusNotFound, "user not found")
			return
		}

		writeJSON(w, http.StatusOK, user)
	}
}

func updateUserHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := chi.URLParam(r, "userID")

		var input struct {
			Name      string `json:"name"`
			Email     string `json:"email"`
			Phone     string `json:"phone"`
			SlackID   string `json:"slack_id"`
			NtfyTopic string `json:"ntfy_topic"`
			TeamID    string `json:"team_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if input.Name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
		if input.Email == "" {
			writeError(w, http.StatusBadRequest, "email is required")
			return
		}

		user, err := db.UpdateUser(r.Context(), userID, input.Name, input.Email, input.Phone, input.SlackID, input.NtfyTopic, input.TeamID)
		if err != nil {
			slog.Error("failed to update user", "error", err, "user_id", userID)
			writeError(w, http.StatusNotFound, "user not found")
			return
		}

		if auditErr := db.RecordAudit(r.Context(), "", "user.updated", "user", userID, map[string]string{
			"name":  user.Name,
			"email": user.Email,
		}); auditErr != nil {
			slog.Error("failed to record audit for user update", "error", auditErr)
		}

		writeJSON(w, http.StatusOK, user)
	}
}

func deleteUserHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := chi.URLParam(r, "userID")

		if err := db.DeleteUser(r.Context(), userID); err != nil {
			slog.Error("failed to delete user", "error", err, "user_id", userID)
			writeError(w, http.StatusNotFound, "user not found")
			return
		}

		if auditErr := db.RecordAudit(r.Context(), "", "user.deleted", "user", userID, nil); auditErr != nil {
			slog.Error("failed to record audit for user deletion", "error", auditErr)
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
