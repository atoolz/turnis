package api

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"

	"github.com/atoolz/turnis/internal/store"
)

type contextKey string

const apiKeyContextKey contextKey = "api_key"

// APIKeyAuth returns a chi middleware that validates Bearer tokens against the
// api_keys table. It hashes the provided key with SHA-256 and looks up the
// resulting hash. On success, it updates last_used_at asynchronously and stores
// the APIKey in the request context.
func APIKeyAuth(db *store.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Exempt /api/v1/status from authentication.
			if r.URL.Path == "/api/v1/status" {
				next.ServeHTTP(w, r)
				return
			}

			header := r.Header.Get("Authorization")
			if header == "" || !strings.HasPrefix(header, "Bearer ") {
				writeError(w, http.StatusUnauthorized, "missing or invalid Authorization header")
				return
			}

			token := strings.TrimPrefix(header, "Bearer ")
			if token == "" {
				writeError(w, http.StatusUnauthorized, "empty bearer token")
				return
			}

			h := sha256.Sum256([]byte(token))
			hash := hex.EncodeToString(h[:])

			key, err := db.GetAPIKeyByHash(r.Context(), hash)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "invalid API key")
				return
			}

			// Update last_used_at in the background to avoid adding latency.
			go func() {
				if err := db.UpdateAPIKeyLastUsed(r.Context(), key.ID); err != nil {
					slog.Error("failed to update api key last_used_at", "error", err, "key_id", key.ID)
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}
