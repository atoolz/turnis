package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/atoolz/turnis/internal/schedule"
	"github.com/atoolz/turnis/internal/store"
)

func listSchedulesHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		schedules, err := db.ListSchedules(r.Context())
		if err != nil {
			slog.Error("failed to list schedules", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to list schedules")
			return
		}
		if schedules == nil {
			schedules = []schedule.Schedule{}
		}
		writeJSON(w, http.StatusOK, schedules)
	}
}

func createScheduleHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input schedule.Schedule
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

		created, err := db.CreateSchedule(r.Context(), &input)
		if err != nil {
			slog.Error("failed to create schedule", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to create schedule")
			return
		}

		writeJSON(w, http.StatusCreated, created)
	}
}

func getScheduleHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scheduleID := chi.URLParam(r, "scheduleID")

		s, err := db.GetSchedule(r.Context(), scheduleID)
		if err != nil {
			slog.Error("failed to get schedule", "error", err, "schedule_id", scheduleID)
			writeError(w, http.StatusNotFound, "schedule not found")
			return
		}

		writeJSON(w, http.StatusOK, s)
	}
}

func whosOnCallHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scheduleID := r.URL.Query().Get("schedule_id")
		teamID := r.URL.Query().Get("team_id")

		if scheduleID == "" && teamID == "" {
			writeError(w, http.StatusBadRequest, "schedule_id or team_id query parameter is required")
			return
		}

		now := time.Now()
		type onCallResult struct {
			ScheduleID   string `json:"schedule_id"`
			ScheduleName string `json:"schedule_name"`
			UserID       string `json:"user_id"`
		}

		var results []onCallResult

		if scheduleID != "" {
			s, err := db.GetSchedule(r.Context(), scheduleID)
			if err != nil {
				slog.Error("failed to get schedule", "error", err, "schedule_id", scheduleID)
				writeError(w, http.StatusNotFound, "schedule not found")
				return
			}

			overrides, err := db.GetOverrides(r.Context(), s.ID)
			if err != nil {
				slog.Error("failed to get overrides", "error", err, "schedule_id", s.ID)
				writeError(w, http.StatusInternalServerError, "failed to get overrides")
				return
			}

			userID := schedule.WhosOnCall(s, overrides, now)
			results = append(results, onCallResult{
				ScheduleID:   s.ID,
				ScheduleName: s.Name,
				UserID:       userID,
			})
		} else {
			schedules, err := db.GetSchedulesByTeam(r.Context(), teamID)
			if err != nil {
				slog.Error("failed to get schedules by team", "error", err, "team_id", teamID)
				writeError(w, http.StatusInternalServerError, "failed to get schedules")
				return
			}

			for i := range schedules {
				overrides, err := db.GetOverrides(r.Context(), schedules[i].ID)
				if err != nil {
					slog.Error("failed to get overrides", "error", err, "schedule_id", schedules[i].ID)
					continue
				}

				userID := schedule.WhosOnCall(&schedules[i], overrides, now)
				results = append(results, onCallResult{
					ScheduleID:   schedules[i].ID,
					ScheduleName: schedules[i].Name,
					UserID:       userID,
				})
			}
		}

		if results == nil {
			results = []onCallResult{}
		}
		writeJSON(w, http.StatusOK, results)
	}
}

func createOverrideHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scheduleID := chi.URLParam(r, "scheduleID")

		var input struct {
			UserID    string    `json:"user_id"`
			StartTime time.Time `json:"start_time"`
			EndTime   time.Time `json:"end_time"`
			Reason    string    `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if input.UserID == "" {
			writeError(w, http.StatusBadRequest, "user_id is required")
			return
		}
		if input.StartTime.IsZero() {
			writeError(w, http.StatusBadRequest, "start_time is required")
			return
		}
		if input.EndTime.IsZero() {
			writeError(w, http.StatusBadRequest, "end_time is required")
			return
		}
		if !input.EndTime.After(input.StartTime) {
			writeError(w, http.StatusBadRequest, "end_time must be after start_time")
			return
		}
		if !input.EndTime.After(time.Now()) {
			writeError(w, http.StatusBadRequest, "end_time must be in the future")
			return
		}

		override, err := db.CreateOverride(r.Context(), scheduleID, input.UserID, input.StartTime, input.EndTime, input.Reason)
		if err != nil {
			slog.Error("failed to create override", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to create override")
			return
		}

		if auditErr := db.RecordAudit(r.Context(), input.UserID, "override.created", "schedule", scheduleID, map[string]string{
			"override_id": override.ID,
		}); auditErr != nil {
			slog.Error("failed to record audit for override creation", "error", auditErr)
		}

		writeJSON(w, http.StatusCreated, override)
	}
}
