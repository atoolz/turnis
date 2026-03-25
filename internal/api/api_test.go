package api_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/atoolz/turnis/internal/alert"
	"github.com/atoolz/turnis/internal/api"
	"github.com/atoolz/turnis/internal/config"
	"github.com/atoolz/turnis/internal/escalation"
	"github.com/atoolz/turnis/internal/schedule"
	"github.com/atoolz/turnis/internal/store"
)

const testAPIKey = "test-api-key-for-turnis-testing-1234"

func setupTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.New(config.DatabaseConfig{
		Driver: "sqlite",
		DSN:    fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()),
	})
	require.NoError(t, err)
	require.NoError(t, db.Migrate())
	// Allow a second connection so the background goroutine in APIKeyAuth
	// (UpdateAPIKeyLastUsed) does not block the main request goroutine.
	db.Conn().SetMaxOpenConns(2)
	t.Cleanup(func() { db.Close() })
	return db
}

func setupTestServer(t *testing.T) (*httptest.Server, *store.DB) {
	t.Helper()
	db := setupTestDB(t)

	// Create an API key for authenticating requests.
	h := sha256.Sum256([]byte(testAPIKey))
	hash := hex.EncodeToString(h[:])
	_, err := db.CreateAPIKey(t.Context(), hash, "test-key", "")
	require.NoError(t, err)

	cfg := &config.Config{}
	router := api.NewRouter(db, cfg, nil, nil)
	srv := httptest.NewServer(router)
	t.Cleanup(func() { srv.Close() })
	return srv, db
}

func authRequest(method, url string, body io.Reader) *http.Request {
	req, _ := http.NewRequest(method, url, body)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func doJSON(t *testing.T, req *http.Request, target any) *http.Response {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	if target != nil {
		err = json.Unmarshal(body, target)
		require.NoError(t, err)
	}
	return resp
}

func jsonBody(v any) *bytes.Buffer {
	b, _ := json.Marshal(v)
	return bytes.NewBuffer(b)
}

// --- Status ---

func TestStatus(t *testing.T) {
	srv, _ := setupTestServer(t)

	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/status", nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "ok", body["status"])
	assert.Equal(t, "turnis", body["service"])
}

// --- Auth ---

func TestAuth_MissingHeader(t *testing.T) {
	srv, _ := setupTestServer(t)

	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/teams", nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestAuth_InvalidKey(t *testing.T) {
	srv, _ := setupTestServer(t)

	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/teams", nil)
	req.Header.Set("Authorization", "Bearer invalid-key")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// --- Teams API ---

func TestTeams_CreateAndGet(t *testing.T) {
	srv, _ := setupTestServer(t)

	body := jsonBody(map[string]string{"name": "platform", "slack_channel": "#platform"})
	req := authRequest("POST", srv.URL+"/api/v1/teams", body)

	var team store.Team
	resp := doJSON(t, req, &team)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	assert.NotEmpty(t, team.ID)
	assert.Equal(t, "platform", team.Name)
	assert.Equal(t, "#platform", team.SlackChannel)

	// GET specific team
	req = authRequest("GET", srv.URL+"/api/v1/teams/"+team.ID, nil)
	var got store.Team
	resp = doJSON(t, req, &got)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, team.ID, got.ID)
	assert.Equal(t, "platform", got.Name)
}

func TestTeams_CreateMissingName(t *testing.T) {
	srv, _ := setupTestServer(t)

	body := jsonBody(map[string]string{"slack_channel": "#test"})
	req := authRequest("POST", srv.URL+"/api/v1/teams", body)

	var errResp map[string]string
	resp := doJSON(t, req, &errResp)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Contains(t, errResp["error"], "name is required")
}

func TestTeams_List(t *testing.T) {
	srv, _ := setupTestServer(t)

	// Create two teams
	for _, name := range []string{"alpha", "beta"} {
		req := authRequest("POST", srv.URL+"/api/v1/teams", jsonBody(map[string]string{"name": name}))
		resp := doJSON(t, req, nil)
		assert.Equal(t, http.StatusCreated, resp.StatusCode)
	}

	req := authRequest("GET", srv.URL+"/api/v1/teams", nil)
	var teams []store.Team
	resp := doJSON(t, req, &teams)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Len(t, teams, 2)
}

func TestTeams_GetNonexistent(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := authRequest("GET", srv.URL+"/api/v1/teams/nonexistent-id", nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestTeams_Delete(t *testing.T) {
	srv, _ := setupTestServer(t)

	// Create
	req := authRequest("POST", srv.URL+"/api/v1/teams", jsonBody(map[string]string{"name": "to-delete"}))
	var team store.Team
	doJSON(t, req, &team)

	// Delete
	req = authRequest("DELETE", srv.URL+"/api/v1/teams/"+team.ID, nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	// Verify gone
	req = authRequest("GET", srv.URL+"/api/v1/teams/"+team.ID, nil)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// --- Users API ---

func createTeam(t *testing.T, srv *httptest.Server) store.Team {
	t.Helper()
	req := authRequest("POST", srv.URL+"/api/v1/teams", jsonBody(map[string]string{"name": "test-team"}))
	var team store.Team
	resp := doJSON(t, req, &team)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	return team
}

func TestUsers_CreateWithAllFields(t *testing.T) {
	srv, _ := setupTestServer(t)
	team := createTeam(t, srv)

	body := jsonBody(map[string]string{
		"name":       "Alice",
		"email":      "alice@example.com",
		"phone":      "+1234567890",
		"slack_id":   "U001",
		"ntfy_topic": "alice-oncall",
		"team_id":    team.ID,
	})
	req := authRequest("POST", srv.URL+"/api/v1/users", body)

	var user store.User
	resp := doJSON(t, req, &user)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	assert.NotEmpty(t, user.ID)
	assert.Equal(t, "Alice", user.Name)
	assert.Equal(t, "alice@example.com", user.Email)
	assert.Equal(t, "+1234567890", user.Phone)
	assert.Equal(t, "U001", user.SlackID)
	assert.Equal(t, "alice-oncall", user.NtfyTopic)
	assert.Equal(t, team.ID, user.TeamID)
}

func TestUsers_CreateMissingFields(t *testing.T) {
	srv, _ := setupTestServer(t)

	tests := []struct {
		name string
		body map[string]string
		want string
	}{
		{"missing name", map[string]string{"email": "test@test.com"}, "name is required"},
		{"missing email", map[string]string{"name": "Bob"}, "email is required"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := authRequest("POST", srv.URL+"/api/v1/users", jsonBody(tc.body))
			var errResp map[string]string
			resp := doJSON(t, req, &errResp)
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
			assert.Contains(t, errResp["error"], tc.want)
		})
	}
}

func TestUsers_ListAll(t *testing.T) {
	srv, _ := setupTestServer(t)

	for _, name := range []string{"Alice", "Bob"} {
		req := authRequest("POST", srv.URL+"/api/v1/users", jsonBody(map[string]string{
			"name":  name,
			"email": name + "@test.com",
		}))
		resp := doJSON(t, req, nil)
		require.Equal(t, http.StatusCreated, resp.StatusCode)
	}

	req := authRequest("GET", srv.URL+"/api/v1/users", nil)
	var users []store.User
	resp := doJSON(t, req, &users)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Len(t, users, 2)
}

func TestUsers_ListByTeam(t *testing.T) {
	srv, _ := setupTestServer(t)
	team := createTeam(t, srv)

	// User with team
	req := authRequest("POST", srv.URL+"/api/v1/users", jsonBody(map[string]string{
		"name": "Alice", "email": "alice@test.com", "team_id": team.ID,
	}))
	doJSON(t, req, nil)

	// User without team
	req = authRequest("POST", srv.URL+"/api/v1/users", jsonBody(map[string]string{
		"name": "Bob", "email": "bob@test.com",
	}))
	doJSON(t, req, nil)

	// Filter by team
	req = authRequest("GET", srv.URL+"/api/v1/users?team_id="+team.ID, nil)
	var users []store.User
	resp := doJSON(t, req, &users)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Len(t, users, 1)
	assert.Equal(t, "Alice", users[0].Name)
}

func TestUsers_GetSpecific(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := authRequest("POST", srv.URL+"/api/v1/users", jsonBody(map[string]string{
		"name": "Alice", "email": "alice@test.com",
	}))
	var user store.User
	doJSON(t, req, &user)

	req = authRequest("GET", srv.URL+"/api/v1/users/"+user.ID, nil)
	var got store.User
	resp := doJSON(t, req, &got)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, user.ID, got.ID)
	assert.Equal(t, "Alice", got.Name)
}

func TestUsers_Update(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := authRequest("POST", srv.URL+"/api/v1/users", jsonBody(map[string]string{
		"name": "Alice", "email": "alice@test.com",
	}))
	var user store.User
	doJSON(t, req, &user)

	req = authRequest("PUT", srv.URL+"/api/v1/users/"+user.ID, jsonBody(map[string]string{
		"name": "Alice Updated", "email": "alice2@test.com", "phone": "+999",
	}))
	var updated store.User
	resp := doJSON(t, req, &updated)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "Alice Updated", updated.Name)
	assert.Equal(t, "alice2@test.com", updated.Email)
	assert.Equal(t, "+999", updated.Phone)
}

func TestUsers_Delete(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := authRequest("POST", srv.URL+"/api/v1/users", jsonBody(map[string]string{
		"name": "Alice", "email": "alice@test.com",
	}))
	var user store.User
	doJSON(t, req, &user)

	req = authRequest("DELETE", srv.URL+"/api/v1/users/"+user.ID, nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	// Verify gone
	req = authRequest("GET", srv.URL+"/api/v1/users/"+user.ID, nil)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// --- Schedules API ---

func createTeamAndUser(t *testing.T, srv *httptest.Server) (store.Team, store.User) {
	t.Helper()
	team := createTeam(t, srv)

	req := authRequest("POST", srv.URL+"/api/v1/users", jsonBody(map[string]string{
		"name": "Alice", "email": "alice@test.com", "team_id": team.ID,
	}))
	var user store.User
	resp := doJSON(t, req, &user)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	return team, user
}

func TestSchedules_Create(t *testing.T) {
	srv, _ := setupTestServer(t)
	team, user := createTeamAndUser(t, srv)

	body := jsonBody(map[string]any{
		"name":    "primary",
		"team_id": team.ID,
		"layers": []map[string]any{
			{
				"priority": 1,
				"participants": []map[string]any{
					{"user_id": user.ID, "position": 0},
				},
			},
		},
	})
	req := authRequest("POST", srv.URL+"/api/v1/schedules", body)

	var sched schedule.Schedule
	resp := doJSON(t, req, &sched)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	assert.NotEmpty(t, sched.ID)
	assert.Equal(t, "primary", sched.Name)
	assert.Equal(t, team.ID, sched.TeamID)
}

func TestSchedules_List(t *testing.T) {
	srv, _ := setupTestServer(t)
	team, user := createTeamAndUser(t, srv)

	body := jsonBody(map[string]any{
		"name":    "primary",
		"team_id": team.ID,
		"layers": []map[string]any{
			{"priority": 1, "participants": []map[string]any{
				{"user_id": user.ID, "position": 0},
			}},
		},
	})
	req := authRequest("POST", srv.URL+"/api/v1/schedules", body)
	doJSON(t, req, nil)

	req = authRequest("GET", srv.URL+"/api/v1/schedules", nil)
	var schedules []schedule.Schedule
	resp := doJSON(t, req, &schedules)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Len(t, schedules, 1)
}

func TestSchedules_GetWithLayers(t *testing.T) {
	srv, _ := setupTestServer(t)
	team, user := createTeamAndUser(t, srv)

	body := jsonBody(map[string]any{
		"name":    "primary",
		"team_id": team.ID,
		"layers": []map[string]any{
			{"priority": 1, "participants": []map[string]any{
				{"user_id": user.ID, "position": 0},
			}},
		},
	})
	req := authRequest("POST", srv.URL+"/api/v1/schedules", body)
	var created schedule.Schedule
	doJSON(t, req, &created)

	req = authRequest("GET", srv.URL+"/api/v1/schedules/"+created.ID, nil)
	var got schedule.Schedule
	resp := doJSON(t, req, &got)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, created.ID, got.ID)
	require.Len(t, got.Layers, 1)
	assert.Equal(t, 1, got.Layers[0].Priority)
	require.Len(t, got.Layers[0].Participants, 1)
	assert.Equal(t, user.ID, got.Layers[0].Participants[0].UserID)
}

func TestSchedules_OnCall(t *testing.T) {
	srv, _ := setupTestServer(t)
	team, user := createTeamAndUser(t, srv)

	body := jsonBody(map[string]any{
		"name":    "primary",
		"team_id": team.ID,
		"layers": []map[string]any{
			{"priority": 1, "participants": []map[string]any{
				{"user_id": user.ID, "position": 0},
			}},
		},
	})
	req := authRequest("POST", srv.URL+"/api/v1/schedules", body)
	var created schedule.Schedule
	doJSON(t, req, &created)

	req = authRequest("GET", srv.URL+"/api/v1/schedules/on-call?schedule_id="+created.ID, nil)
	var results []map[string]string
	resp := doJSON(t, req, &results)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	require.Len(t, results, 1)
	assert.Equal(t, created.ID, results[0]["schedule_id"])
	assert.Equal(t, user.ID, results[0]["user_id"])
}

func TestSchedules_CreateOverride(t *testing.T) {
	srv, _ := setupTestServer(t)
	team, user := createTeamAndUser(t, srv)

	// Create another user for the override
	req := authRequest("POST", srv.URL+"/api/v1/users", jsonBody(map[string]string{
		"name": "Bob", "email": "bob@test.com", "team_id": team.ID,
	}))
	var bob store.User
	doJSON(t, req, &bob)

	// Create schedule
	body := jsonBody(map[string]any{
		"name":    "primary",
		"team_id": team.ID,
		"layers": []map[string]any{
			{"priority": 1, "participants": []map[string]any{
				{"user_id": user.ID, "position": 0},
			}},
		},
	})
	req = authRequest("POST", srv.URL+"/api/v1/schedules", body)
	var sched schedule.Schedule
	doJSON(t, req, &sched)

	// Create override
	start := time.Now().Add(1 * time.Hour)
	end := time.Now().Add(24 * time.Hour)
	overrideBody := jsonBody(map[string]any{
		"user_id":    bob.ID,
		"start_time": start.Format(time.RFC3339),
		"end_time":   end.Format(time.RFC3339),
		"reason":     "vacation swap",
	})
	req = authRequest("POST", srv.URL+"/api/v1/schedules/"+sched.ID+"/overrides", overrideBody)
	var override schedule.Override
	resp := doJSON(t, req, &override)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	assert.NotEmpty(t, override.ID)
	assert.Equal(t, bob.ID, override.UserID)
	assert.Equal(t, sched.ID, override.ScheduleID)
	assert.Equal(t, "vacation swap", override.Reason)
}

// --- Alerts API ---

func createIntegration(t *testing.T, srv *httptest.Server, teamID string) store.Integration {
	t.Helper()
	req := authRequest("POST", srv.URL+"/api/v1/integrations", jsonBody(map[string]string{
		"name":    "test-webhook",
		"team_id": teamID,
		"type":    "webhook",
	}))
	var integration store.Integration
	resp := doJSON(t, req, &integration)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	return integration
}

func TestAlerts_Ingest(t *testing.T) {
	srv, _ := setupTestServer(t)
	team := createTeam(t, srv)
	integration := createIntegration(t, srv, team.ID)

	body := jsonBody(map[string]any{
		"integration_id": integration.ID,
		"alert": map[string]any{
			"title":       "CPU > 90%",
			"message":     "CPU usage is critical",
			"severity":    "critical",
			"source":      "prometheus",
			"fingerprint": "cpu-001",
		},
	})
	req := authRequest("POST", srv.URL+"/api/v1/alerts", body)

	var a alert.Alert
	resp := doJSON(t, req, &a)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	assert.NotEmpty(t, a.ID)
	assert.Equal(t, "CPU > 90%", a.Title)
	assert.Equal(t, alert.StatusFiring, a.Status)
	assert.Equal(t, alert.SeverityCritical, a.Severity)
}

func TestAlerts_IngestMissingTitle(t *testing.T) {
	srv, _ := setupTestServer(t)
	team := createTeam(t, srv)
	integration := createIntegration(t, srv, team.ID)

	body := jsonBody(map[string]any{
		"integration_id": integration.ID,
		"alert":          map[string]any{"message": "no title"},
	})
	req := authRequest("POST", srv.URL+"/api/v1/alerts", body)

	var errResp map[string]string
	resp := doJSON(t, req, &errResp)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Contains(t, errResp["error"], "title is required")
}

func TestAlerts_List(t *testing.T) {
	srv, _ := setupTestServer(t)
	team := createTeam(t, srv)
	integration := createIntegration(t, srv, team.ID)

	// Ingest two alerts
	for _, title := range []string{"alert-1", "alert-2"} {
		body := jsonBody(map[string]any{
			"integration_id": integration.ID,
			"alert":          map[string]any{"title": title},
		})
		req := authRequest("POST", srv.URL+"/api/v1/alerts", body)
		doJSON(t, req, nil)
	}

	req := authRequest("GET", srv.URL+"/api/v1/alerts", nil)
	var alerts []alert.Alert
	resp := doJSON(t, req, &alerts)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Len(t, alerts, 2)
}

func TestAlerts_ListByStatus(t *testing.T) {
	srv, _ := setupTestServer(t)
	team := createTeam(t, srv)
	integration := createIntegration(t, srv, team.ID)

	body := jsonBody(map[string]any{
		"integration_id": integration.ID,
		"alert":          map[string]any{"title": "test-alert"},
	})
	req := authRequest("POST", srv.URL+"/api/v1/alerts", body)
	doJSON(t, req, nil)

	// Filter by firing
	req = authRequest("GET", srv.URL+"/api/v1/alerts?status=firing", nil)
	var firing []alert.Alert
	resp := doJSON(t, req, &firing)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Len(t, firing, 1)

	// Filter by resolved (none)
	req = authRequest("GET", srv.URL+"/api/v1/alerts?status=resolved", nil)
	var resolved []alert.Alert
	resp = doJSON(t, req, &resolved)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Len(t, resolved, 0)
}

func TestAlerts_Acknowledge(t *testing.T) {
	srv, _ := setupTestServer(t)
	team := createTeam(t, srv)
	integration := createIntegration(t, srv, team.ID)

	// Create user
	req := authRequest("POST", srv.URL+"/api/v1/users", jsonBody(map[string]string{
		"name": "Alice", "email": "alice@test.com", "team_id": team.ID,
	}))
	var user store.User
	doJSON(t, req, &user)

	// Ingest alert
	body := jsonBody(map[string]any{
		"integration_id": integration.ID,
		"alert":          map[string]any{"title": "test-alert"},
	})
	req = authRequest("POST", srv.URL+"/api/v1/alerts", body)
	var a alert.Alert
	doJSON(t, req, &a)

	// Acknowledge
	req = authRequest("POST", srv.URL+"/api/v1/alerts/"+a.ID+"/ack", jsonBody(map[string]string{
		"user_id": user.ID,
	}))
	var acked alert.Alert
	resp := doJSON(t, req, &acked)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, alert.StatusAcknowledged, acked.Status)
	assert.Equal(t, user.ID, acked.AcknowledgedBy)
	assert.NotNil(t, acked.AcknowledgedAt)
}

func TestAlerts_Resolve(t *testing.T) {
	srv, _ := setupTestServer(t)
	team := createTeam(t, srv)
	integration := createIntegration(t, srv, team.ID)

	body := jsonBody(map[string]any{
		"integration_id": integration.ID,
		"alert":          map[string]any{"title": "test-alert"},
	})
	req := authRequest("POST", srv.URL+"/api/v1/alerts", body)
	var a alert.Alert
	doJSON(t, req, &a)

	// Resolve
	req = authRequest("POST", srv.URL+"/api/v1/alerts/"+a.ID+"/resolve", bytes.NewBuffer([]byte("{}")))
	var resolved alert.Alert
	resp := doJSON(t, req, &resolved)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, alert.StatusResolved, resolved.Status)
	assert.NotNil(t, resolved.ResolvedAt)
}

// --- Webhook API ---

func TestWebhook_IngestViaToken(t *testing.T) {
	srv, _ := setupTestServer(t)
	team := createTeam(t, srv)
	integration := createIntegration(t, srv, team.ID)

	body := jsonBody(map[string]any{
		"title":    "webhook alert",
		"severity": "warning",
	})
	req, _ := http.NewRequest("POST", srv.URL+"/webhook/"+integration.Token, body)
	req.Header.Set("Content-Type", "application/json")

	var a alert.Alert
	resp := doJSON(t, req, &a)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	assert.Equal(t, "webhook alert", a.Title)
	assert.Equal(t, alert.StatusFiring, a.Status)
}

func TestWebhook_InvalidToken(t *testing.T) {
	srv, _ := setupTestServer(t)

	body := jsonBody(map[string]any{"title": "webhook alert"})
	req, _ := http.NewRequest("POST", srv.URL+"/webhook/invalid-token-value", body)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestWebhook_Deduplication(t *testing.T) {
	srv, _ := setupTestServer(t)
	team := createTeam(t, srv)
	integration := createIntegration(t, srv, team.ID)

	payload := map[string]any{
		"title":       "dup alert",
		"fingerprint": "dedup-001",
	}

	// First ingestion: should create
	req, _ := http.NewRequest("POST", srv.URL+"/webhook/"+integration.Token, jsonBody(payload))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	// Second ingestion: should deduplicate
	req, _ = http.NewRequest("POST", srv.URL+"/webhook/"+integration.Token, jsonBody(payload))
	req.Header.Set("Content-Type", "application/json")
	var dedupResp map[string]any
	resp = doJSON(t, req, &dedupResp)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, true, dedupResp["deduplicated"])
}

// --- Escalation Policies API ---

func TestPolicies_Create(t *testing.T) {
	srv, _ := setupTestServer(t)
	team := createTeam(t, srv)

	body := jsonBody(map[string]any{
		"name":    "critical-esc",
		"team_id": team.ID,
		"repeat":  2,
		"steps": []map[string]any{
			{"step_order": 0, "timeout_seconds": 300, "notify_channel": "slack"},
			{"step_order": 1, "timeout_seconds": 600, "notify_channel": "sms"},
		},
	})
	req := authRequest("POST", srv.URL+"/api/v1/escalation-policies", body)

	var policy escalation.Policy
	resp := doJSON(t, req, &policy)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	assert.NotEmpty(t, policy.ID)
	assert.Equal(t, "critical-esc", policy.Name)
	assert.Equal(t, 2, policy.Repeat)
}

func TestPolicies_List(t *testing.T) {
	srv, _ := setupTestServer(t)
	team := createTeam(t, srv)

	body := jsonBody(map[string]any{
		"name":    "test-policy",
		"team_id": team.ID,
		"steps":   []map[string]any{{"step_order": 0, "timeout_seconds": 300, "notify_channel": "slack"}},
	})
	req := authRequest("POST", srv.URL+"/api/v1/escalation-policies", body)
	doJSON(t, req, nil)

	req = authRequest("GET", srv.URL+"/api/v1/escalation-policies", nil)
	var policies []escalation.Policy
	resp := doJSON(t, req, &policies)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Len(t, policies, 1)
}

func TestPolicies_GetWithSteps(t *testing.T) {
	srv, _ := setupTestServer(t)
	team := createTeam(t, srv)

	body := jsonBody(map[string]any{
		"name":    "test-policy",
		"team_id": team.ID,
		"repeat":  3,
		"steps": []map[string]any{
			{"step_order": 0, "timeout_seconds": 300, "notify_channel": "slack"},
			{"step_order": 1, "timeout_seconds": 600, "notify_channel": "sms"},
		},
	})
	req := authRequest("POST", srv.URL+"/api/v1/escalation-policies", body)
	var created escalation.Policy
	doJSON(t, req, &created)

	req = authRequest("GET", srv.URL+"/api/v1/escalation-policies/"+created.ID, nil)
	var got escalation.Policy
	resp := doJSON(t, req, &got)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "test-policy", got.Name)
	assert.Equal(t, 3, got.Repeat)
	require.Len(t, got.Steps, 2)
	assert.Equal(t, 0, got.Steps[0].StepOrder)
	assert.Equal(t, 300, got.Steps[0].TimeoutSeconds)
	assert.Equal(t, "slack", got.Steps[0].NotifyChannel)
	assert.Equal(t, 1, got.Steps[1].StepOrder)
	assert.Equal(t, "sms", got.Steps[1].NotifyChannel)
}

func TestPolicies_Delete(t *testing.T) {
	srv, _ := setupTestServer(t)
	team := createTeam(t, srv)

	body := jsonBody(map[string]any{
		"name":    "to-delete",
		"team_id": team.ID,
		"steps":   []map[string]any{{"step_order": 0, "timeout_seconds": 300, "notify_channel": "slack"}},
	})
	req := authRequest("POST", srv.URL+"/api/v1/escalation-policies", body)
	var policy escalation.Policy
	doJSON(t, req, &policy)

	req = authRequest("DELETE", srv.URL+"/api/v1/escalation-policies/"+policy.ID, nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	// Verify gone
	req = authRequest("GET", srv.URL+"/api/v1/escalation-policies/"+policy.ID, nil)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// --- Integrations API ---

func TestIntegrations_Create(t *testing.T) {
	srv, _ := setupTestServer(t)
	team := createTeam(t, srv)

	body := jsonBody(map[string]string{
		"name":    "grafana-webhook",
		"team_id": team.ID,
		"type":    "webhook",
	})
	req := authRequest("POST", srv.URL+"/api/v1/integrations", body)

	var integration store.Integration
	resp := doJSON(t, req, &integration)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	assert.NotEmpty(t, integration.ID)
	assert.NotEmpty(t, integration.Token)
	assert.Len(t, integration.Token, 64) // 32 bytes hex-encoded
	assert.Equal(t, "grafana-webhook", integration.Name)
	assert.Equal(t, "webhook", integration.Type)
}

func TestIntegrations_List(t *testing.T) {
	srv, _ := setupTestServer(t)
	team := createTeam(t, srv)

	for _, name := range []string{"int-1", "int-2"} {
		req := authRequest("POST", srv.URL+"/api/v1/integrations", jsonBody(map[string]string{
			"name": name, "team_id": team.ID,
		}))
		doJSON(t, req, nil)
	}

	req := authRequest("GET", srv.URL+"/api/v1/integrations", nil)
	var integrations []store.Integration
	resp := doJSON(t, req, &integrations)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Len(t, integrations, 2)
}

func TestIntegrations_Delete(t *testing.T) {
	srv, _ := setupTestServer(t)
	team := createTeam(t, srv)
	integration := createIntegration(t, srv, team.ID)

	req := authRequest("DELETE", srv.URL+"/api/v1/integrations/"+integration.ID, nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	// Verify list is empty
	req = authRequest("GET", srv.URL+"/api/v1/integrations", nil)
	var integrations []store.Integration
	resp = doJSON(t, req, &integrations)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Len(t, integrations, 0)
}
