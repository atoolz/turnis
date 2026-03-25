package web

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/atoolz/turnis/internal/alert"
	"github.com/atoolz/turnis/internal/config"
	"github.com/atoolz/turnis/internal/schedule"
	"github.com/atoolz/turnis/internal/store"
)

func setupWebTest(t *testing.T) (*httptest.Server, *store.DB) {
	t.Helper()
	db, err := store.New(config.DatabaseConfig{
		Driver: "sqlite",
		DSN:    fmt.Sprintf("file:%s?mode=memory&cache=private", t.Name()),
	})
	require.NoError(t, err)
	require.NoError(t, db.Migrate())
	t.Cleanup(func() { db.Close() })

	handler := NewHandler(db)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return srv, db
}

func get(t *testing.T, srv *httptest.Server, path string) *http.Response {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	require.NoError(t, err)
	return resp
}

func bodyString(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(b)
}

// --- Home ---

func TestHome_EmptyDB(t *testing.T) {
	srv, _ := setupWebTest(t)

	resp := get(t, srv, "/")
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/html")

	body := bodyString(t, resp)
	assert.Contains(t, body, "No schedules configured")
	assert.Contains(t, body, "Dashboard")
}

func TestHome_WithData(t *testing.T) {
	srv, db := setupWebTest(t)
	ctx := context.Background()

	team, err := db.CreateTeam(ctx, "web-team", "#oncall")
	require.NoError(t, err)

	user, err := db.CreateUser(ctx, "Alice Web", "alice@web.test", "", "", "", team.ID)
	require.NoError(t, err)

	_, err = db.CreateSchedule(ctx, &schedule.Schedule{
		Name:   "web-primary",
		TeamID: team.ID,
		Layers: []schedule.Layer{
			{
				Priority: 1,
				Participants: []schedule.Participant{
					{UserID: user.ID, Position: 0},
				},
			},
		},
	})
	require.NoError(t, err)

	resp := get(t, srv, "/")
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body := bodyString(t, resp)
	assert.Contains(t, body, "web-primary")
	assert.Contains(t, body, "Alice Web")
}

// --- Schedules ---

func TestSchedules_List(t *testing.T) {
	srv, db := setupWebTest(t)
	ctx := context.Background()

	team, err := db.CreateTeam(ctx, "sched-team", "")
	require.NoError(t, err)

	user, err := db.CreateUser(ctx, "Bob Sched", "bob@sched.test", "", "", "", team.ID)
	require.NoError(t, err)

	_, err = db.CreateSchedule(ctx, &schedule.Schedule{
		Name:   "sched-alpha",
		TeamID: team.ID,
		Layers: []schedule.Layer{
			{
				Priority: 1,
				Participants: []schedule.Participant{
					{UserID: user.ID, Position: 0},
				},
			},
		},
	})
	require.NoError(t, err)

	resp := get(t, srv, "/schedules")
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/html")

	body := bodyString(t, resp)
	assert.Contains(t, body, "sched-alpha")
	assert.Contains(t, body, "Bob Sched")
	assert.Contains(t, body, "Schedules")
}

func TestSchedules_Detail(t *testing.T) {
	srv, db := setupWebTest(t)
	ctx := context.Background()

	team, err := db.CreateTeam(ctx, "detail-team", "")
	require.NoError(t, err)

	user, err := db.CreateUser(ctx, "Carol Detail", "carol@detail.test", "", "", "", team.ID)
	require.NoError(t, err)

	sched, err := db.CreateSchedule(ctx, &schedule.Schedule{
		Name:   "detail-sched",
		TeamID: team.ID,
		Layers: []schedule.Layer{
			{
				Priority: 1,
				Participants: []schedule.Participant{
					{UserID: user.ID, Position: 0},
				},
			},
		},
	})
	require.NoError(t, err)

	resp := get(t, srv, "/schedules/"+sched.ID)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/html")

	body := bodyString(t, resp)
	assert.Contains(t, body, "detail-sched")
	assert.Contains(t, body, "Carol Detail")
	assert.Contains(t, body, "Layers")
	assert.Contains(t, body, "Overrides")
}

func TestSchedules_DetailNotFound(t *testing.T) {
	srv, _ := setupWebTest(t)

	resp := get(t, srv, "/schedules/nonexistent-id")
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// --- Alerts ---

func TestAlerts_EmptyList(t *testing.T) {
	srv, _ := setupWebTest(t)

	resp := get(t, srv, "/alerts")
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/html")

	body := bodyString(t, resp)
	assert.Contains(t, body, "Alerts")
	assert.Contains(t, body, "No alerts match the current filters")
}

func TestAlerts_WithData(t *testing.T) {
	srv, db := setupWebTest(t)
	ctx := context.Background()

	team, err := db.CreateTeam(ctx, "alert-team", "")
	require.NoError(t, err)

	integration, err := db.CreateIntegration(ctx, "alert-hook", team.ID, "webhook", "")
	require.NoError(t, err)

	_, err = db.CreateAlert(ctx, integration.ID, alert.IncomingAlert{
		Title:    "CPU on fire",
		Severity: alert.SeverityCritical,
		Source:   "prometheus",
	})
	require.NoError(t, err)

	resp := get(t, srv, "/alerts")
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body := bodyString(t, resp)
	assert.Contains(t, body, "CPU on fire")
	assert.Contains(t, body, "critical")
	assert.Contains(t, body, "prometheus")
}

func TestAlerts_FilterByStatus(t *testing.T) {
	srv, db := setupWebTest(t)
	ctx := context.Background()

	team, err := db.CreateTeam(ctx, "filter-team", "")
	require.NoError(t, err)

	integration, err := db.CreateIntegration(ctx, "filter-hook", team.ID, "webhook", "")
	require.NoError(t, err)

	firingAlert, err := db.CreateAlert(ctx, integration.ID, alert.IncomingAlert{
		Title:    "Still firing alert",
		Severity: alert.SeverityWarning,
	})
	require.NoError(t, err)

	resolvedAlert, err := db.CreateAlert(ctx, integration.ID, alert.IncomingAlert{
		Title:    "Already resolved alert",
		Severity: alert.SeverityInfo,
	})
	require.NoError(t, err)
	_, err = db.ResolveAlert(ctx, resolvedAlert.ID)
	require.NoError(t, err)

	// Filter by firing status
	resp := get(t, srv, "/alerts?status=firing")
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body := bodyString(t, resp)
	assert.Contains(t, body, firingAlert.Title)
	assert.NotContains(t, body, resolvedAlert.Title)

	// Filter by resolved status
	resp2 := get(t, srv, "/alerts?status=resolved")
	body2 := bodyString(t, resp2)
	assert.Contains(t, body2, resolvedAlert.Title)
	assert.NotContains(t, body2, firingAlert.Title)
}

// --- Teams ---

func TestTeams_List(t *testing.T) {
	srv, db := setupWebTest(t)
	ctx := context.Background()

	_, err := db.CreateTeam(ctx, "infra-team", "#infra-oncall")
	require.NoError(t, err)

	resp := get(t, srv, "/teams")
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/html")

	body := bodyString(t, resp)
	assert.Contains(t, body, "infra-team")
	assert.Contains(t, body, "#infra-oncall")
	assert.Contains(t, body, "Teams")
}

func TestTeams_Create(t *testing.T) {
	srv, _ := setupWebTest(t)

	form := url.Values{}
	form.Set("name", "new-team")
	form.Set("slack_channel", "#new-oncall")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.PostForm(srv.URL+"/teams", form)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)
	assert.Equal(t, "/teams", resp.Header.Get("Location"))
}

func TestTeams_Detail(t *testing.T) {
	srv, db := setupWebTest(t)
	ctx := context.Background()

	team, err := db.CreateTeam(ctx, "detail-team", "#detail-chan")
	require.NoError(t, err)

	user, err := db.CreateUser(ctx, "Dan Detail", "dan@detail.test", "+15551234567", "", "", team.ID)
	require.NoError(t, err)

	_, err = db.CreateIntegration(ctx, "detail-webhook", team.ID, "webhook", "")
	require.NoError(t, err)

	resp := get(t, srv, "/teams/"+team.ID)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/html")

	body := bodyString(t, resp)
	assert.Contains(t, body, "detail-team")
	assert.Contains(t, body, user.Name)
	assert.Contains(t, body, "detail-webhook")
	assert.Contains(t, body, "Integrations")
	assert.Contains(t, body, "Members")
}

func TestTeams_DetailNotFound(t *testing.T) {
	srv, _ := setupWebTest(t)

	resp := get(t, srv, "/teams/nonexistent-id")
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// --- Users ---

func TestUsers_List(t *testing.T) {
	srv, db := setupWebTest(t)
	ctx := context.Background()

	team, err := db.CreateTeam(ctx, "user-team", "")
	require.NoError(t, err)

	_, err = db.CreateUser(ctx, "Eve Users", "eve@users.test", "", "", "", team.ID)
	require.NoError(t, err)

	resp := get(t, srv, "/users")
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/html")

	body := bodyString(t, resp)
	assert.Contains(t, body, "Eve Users")
	assert.Contains(t, body, "eve@users.test")
	assert.Contains(t, body, "Users")
}

func TestUsers_Create(t *testing.T) {
	srv, _ := setupWebTest(t)

	form := url.Values{}
	form.Set("name", "Frank New")
	form.Set("email", "frank@new.test")
	form.Set("phone", "+15559876543")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.PostForm(srv.URL+"/users", form)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)
	assert.Equal(t, "/users", resp.Header.Get("Location"))
}

// --- Partials ---

func TestOnCallPartial(t *testing.T) {
	srv, db := setupWebTest(t)
	ctx := context.Background()

	team, err := db.CreateTeam(ctx, "partial-team", "")
	require.NoError(t, err)

	user, err := db.CreateUser(ctx, "Grace Partial", "grace@partial.test", "", "", "", team.ID)
	require.NoError(t, err)

	_, err = db.CreateSchedule(ctx, &schedule.Schedule{
		Name:   "partial-sched",
		TeamID: team.ID,
		Layers: []schedule.Layer{
			{
				Priority: 1,
				Participants: []schedule.Participant{
					{UserID: user.ID, Position: 0},
				},
			},
		},
	})
	require.NoError(t, err)

	resp := get(t, srv, "/web/oncall")
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/html")

	body := bodyString(t, resp)
	// Partial should not include full layout (no <!DOCTYPE html>)
	assert.NotContains(t, body, "<!DOCTYPE html>")
	assert.Contains(t, body, "partial-sched")
	assert.Contains(t, body, "Grace Partial")
}

// --- Static ---

func TestStaticCSS(t *testing.T) {
	srv, _ := setupWebTest(t)

	resp := get(t, srv, "/static/app.css")
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/css")

	body := bodyString(t, resp)
	assert.Contains(t, body, ".badge")
}

// --- Override creation via web form ---

func TestSchedules_CreateOverride(t *testing.T) {
	srv, db := setupWebTest(t)
	ctx := context.Background()

	team, err := db.CreateTeam(ctx, "override-team", "")
	require.NoError(t, err)

	user, err := db.CreateUser(ctx, "Hank Override", "hank@override.test", "", "", "", team.ID)
	require.NoError(t, err)

	sched, err := db.CreateSchedule(ctx, &schedule.Schedule{
		Name:   "override-sched",
		TeamID: team.ID,
		Layers: []schedule.Layer{
			{
				Priority: 1,
				Participants: []schedule.Participant{
					{UserID: user.ID, Position: 0},
				},
			},
		},
	})
	require.NoError(t, err)

	form := url.Values{}
	form.Set("user_id", user.ID)
	form.Set("end_time", time.Now().Add(24*time.Hour).Format("2006-01-02T15:04"))
	form.Set("reason", "vacation cover")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.PostForm(srv.URL+"/schedules/"+sched.ID+"/overrides", form)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)
	assert.Equal(t, "/schedules/"+sched.ID, resp.Header.Get("Location"))
}
