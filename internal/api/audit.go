package api

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/atoolz/turnis/internal/store"
)

func listAuditLogHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resourceType := r.URL.Query().Get("resource_type")
		resourceID := r.URL.Query().Get("resource_id")
		limitStr := r.URL.Query().Get("limit")

		var limit int
		if limitStr != "" {
			var err error
			limit, err = strconv.Atoi(limitStr)
			if err != nil || limit < 0 {
				writeError(w, http.StatusBadRequest, "limit must be a positive integer")
				return
			}
		}

		entries, err := db.ListAuditLog(r.Context(), resourceType, resourceID, limit)
		if err != nil {
			slog.Error("failed to list audit log", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to list audit log")
			return
		}
		if entries == nil {
			entries = []store.AuditEntry{}
		}
		writeJSON(w, http.StatusOK, entries)
	}
}
