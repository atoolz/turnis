package store

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/atoolz/turnis/internal/alert"
	"github.com/atoolz/turnis/internal/config"
	"github.com/atoolz/turnis/internal/escalation"
	"github.com/atoolz/turnis/internal/schedule"
)

func setupTestDB(t *testing.T) *DB {
	t.Helper()

	// Each test gets its own isolated in-memory database.
	// PRAGMA foreign_keys is enabled by store.New() automatically.
	db, err := New(config.DatabaseConfig{
		Driver: "sqlite",
		DSN:    fmt.Sprintf("file:%s?mode=memory&cache=private", t.Name()),
	})
	require.NoError(t, err)

	require.NoError(t, db.Migrate())
	t.Cleanup(func() { db.Close() })
	return db
}

// --- Teams ---

func TestTeams_CRUD(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	// Create
	team, err := db.CreateTeam(ctx, "platform", "#platform-oncall")
	require.NoError(t, err)
	assert.NotEmpty(t, team.ID)
	assert.Equal(t, "platform", team.Name)
	assert.Equal(t, "#platform-oncall", team.SlackChannel)

	// List
	teams, err := db.ListTeams(ctx)
	require.NoError(t, err)
	assert.Len(t, teams, 1)
	assert.Equal(t, team.ID, teams[0].ID)

	// Get
	got, err := db.GetTeam(ctx, team.ID)
	require.NoError(t, err)
	assert.Equal(t, team.ID, got.ID)
	assert.Equal(t, "platform", got.Name)

	// Delete
	err = db.DeleteTeam(ctx, team.ID)
	require.NoError(t, err)

	teams, err = db.ListTeams(ctx)
	require.NoError(t, err)
	assert.Len(t, teams, 0)
}

func TestTeams_DeleteNonexistent(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	err := db.DeleteTeam(ctx, "nonexistent")
	assert.Error(t, err)
}

// --- Users ---

func TestUsers_CRUD(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	team, err := db.CreateTeam(ctx, "backend", "")
	require.NoError(t, err)

	// Create
	user, err := db.CreateUser(ctx, "Alice", "alice@example.com", "+1234", "U001", "alice-topic", team.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, user.ID)
	assert.Equal(t, "Alice", user.Name)
	assert.Equal(t, team.ID, user.TeamID)

	// Create another user without team
	user2, err := db.CreateUser(ctx, "Bob", "bob@example.com", "", "", "", "")
	require.NoError(t, err)

	// List all
	users, err := db.ListUsers(ctx, "")
	require.NoError(t, err)
	assert.Len(t, users, 2)

	// List filtered by team
	users, err = db.ListUsers(ctx, team.ID)
	require.NoError(t, err)
	assert.Len(t, users, 1)
	assert.Equal(t, user.ID, users[0].ID)

	// Get
	got, err := db.GetUser(ctx, user.ID)
	require.NoError(t, err)
	assert.Equal(t, "alice@example.com", got.Email)

	// Update
	updated, err := db.UpdateUser(ctx, user.ID, "Alice Updated", "alice2@example.com", "+5678", "U002", "new-topic", team.ID)
	require.NoError(t, err)
	assert.Equal(t, "Alice Updated", updated.Name)
	assert.Equal(t, "alice2@example.com", updated.Email)

	// Delete
	err = db.DeleteUser(ctx, user.ID)
	require.NoError(t, err)
	err = db.DeleteUser(ctx, user2.ID)
	require.NoError(t, err)

	users, err = db.ListUsers(ctx, "")
	require.NoError(t, err)
	assert.Len(t, users, 0)
}

// --- Schedules ---

func TestSchedules_CRUD(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	team, err := db.CreateTeam(ctx, "infra", "")
	require.NoError(t, err)

	u1, err := db.CreateUser(ctx, "Alice", "alice@example.com", "", "", "", team.ID)
	require.NoError(t, err)
	u2, err := db.CreateUser(ctx, "Bob", "bob@example.com", "", "", "", team.ID)
	require.NoError(t, err)

	// Create schedule with layers and participants
	s := &schedule.Schedule{
		Name:   "primary-oncall",
		TeamID: team.ID,
		Layers: []schedule.Layer{
			{
				Priority: 1,
				Participants: []schedule.Participant{
					{UserID: u1.ID, Position: 0},
					{UserID: u2.ID, Position: 1},
				},
			},
		},
	}

	created, err := db.CreateSchedule(ctx, s)
	require.NoError(t, err)
	assert.NotEmpty(t, created.ID)
	assert.Equal(t, "primary-oncall", created.Name)
	assert.Equal(t, schedule.RotationWeekly, created.RotationType)

	// List
	schedules, err := db.ListSchedules(ctx)
	require.NoError(t, err)
	assert.Len(t, schedules, 1)

	// Get with layers loaded
	got, err := db.GetSchedule(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, got.ID)
	require.Len(t, got.Layers, 1)
	assert.Equal(t, 1, got.Layers[0].Priority)
	require.Len(t, got.Layers[0].Participants, 2)
	assert.Equal(t, u1.ID, got.Layers[0].Participants[0].UserID)
	assert.Equal(t, u2.ID, got.Layers[0].Participants[1].UserID)
}

// --- Integrations ---

func TestIntegrations_CRUD(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	team, err := db.CreateTeam(ctx, "platform", "")
	require.NoError(t, err)

	// Create (token should be auto-generated)
	integration, err := db.CreateIntegration(ctx, "grafana-webhook", team.ID, "webhook", "")
	require.NoError(t, err)
	assert.NotEmpty(t, integration.ID)
	assert.NotEmpty(t, integration.Token)
	assert.Len(t, integration.Token, 64) // 32 bytes -> 64 hex chars

	// Get by token
	got, err := db.GetIntegrationByToken(ctx, integration.Token)
	require.NoError(t, err)
	assert.Equal(t, integration.ID, got.ID)

	// Get by ID
	got, err = db.GetIntegration(ctx, integration.ID)
	require.NoError(t, err)
	assert.Equal(t, "grafana-webhook", got.Name)

	// Delete
	err = db.DeleteIntegration(ctx, integration.ID)
	require.NoError(t, err)

	integrations, err := db.ListIntegrations(ctx)
	require.NoError(t, err)
	assert.Len(t, integrations, 0)
}

// --- Policies ---

func TestPolicies_CRUD(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	team, err := db.CreateTeam(ctx, "platform", "")
	require.NoError(t, err)

	p := &escalation.Policy{
		Name:   "critical-escalation",
		TeamID: team.ID,
		Repeat: 2,
		Steps: []escalation.Step{
			{StepOrder: 0, TimeoutSeconds: 300, NotifyChannel: "slack"},
			{StepOrder: 1, TimeoutSeconds: 600, NotifyChannel: "sms"},
		},
	}

	created, err := db.CreatePolicy(ctx, p)
	require.NoError(t, err)
	assert.NotEmpty(t, created.ID)
	assert.Equal(t, 2, created.Repeat)

	// Get with steps loaded
	got, err := db.GetPolicy(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, "critical-escalation", got.Name)
	require.Len(t, got.Steps, 2)
	assert.Equal(t, 0, got.Steps[0].StepOrder)
	assert.Equal(t, 300, got.Steps[0].TimeoutSeconds)
	assert.Equal(t, "slack", got.Steps[0].NotifyChannel)
	assert.Equal(t, 1, got.Steps[1].StepOrder)
	assert.Equal(t, "sms", got.Steps[1].NotifyChannel)

	// Delete
	err = db.DeletePolicy(ctx, created.ID)
	require.NoError(t, err)

	policies, err := db.ListPolicies(ctx)
	require.NoError(t, err)
	assert.Len(t, policies, 0)
}

// --- Alerts ---

func TestAlerts_CRUD(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	team, err := db.CreateTeam(ctx, "platform", "")
	require.NoError(t, err)
	integration, err := db.CreateIntegration(ctx, "prometheus", team.ID, "webhook", "")
	require.NoError(t, err)
	user, err := db.CreateUser(ctx, "Alice", "alice@test.com", "", "", "", team.ID)
	require.NoError(t, err)

	// Create
	a, err := db.CreateAlert(ctx, integration.ID, alert.IncomingAlert{
		Title:       "CPU > 90%",
		Message:     "CPU usage is critical",
		Severity:    alert.SeverityCritical,
		Source:      "prometheus",
		Fingerprint: "cpu-high-001",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, a.ID)
	assert.Equal(t, alert.StatusFiring, a.Status)
	assert.Equal(t, "cpu-high-001", a.Fingerprint)

	// List all
	alerts, err := db.ListAlerts(ctx, "", "")
	require.NoError(t, err)
	assert.Len(t, alerts, 1)

	// List filtered by status
	alerts, err = db.ListAlerts(ctx, string(alert.StatusFiring), "")
	require.NoError(t, err)
	assert.Len(t, alerts, 1)

	alerts, err = db.ListAlerts(ctx, string(alert.StatusResolved), "")
	require.NoError(t, err)
	assert.Len(t, alerts, 0)

	// Get by fingerprint
	fps, err := db.GetAlertsByFingerprint(ctx, integration.ID, "cpu-high-001")
	require.NoError(t, err)
	assert.Len(t, fps, 1)

	// Acknowledge
	acked, err := db.AcknowledgeAlert(ctx, a.ID, user.ID)
	require.NoError(t, err)
	assert.Equal(t, alert.StatusAcknowledged, acked.Status)
	assert.Equal(t, user.ID, acked.AcknowledgedBy)
	assert.NotNil(t, acked.AcknowledgedAt)

	// Resolve
	resolved, err := db.ResolveAlert(ctx, a.ID)
	require.NoError(t, err)
	assert.Equal(t, alert.StatusResolved, resolved.Status)
	assert.NotNil(t, resolved.ResolvedAt)

	// Cannot acknowledge already-resolved alert
	_, err = db.AcknowledgeAlert(ctx, a.ID, user.ID)
	assert.Error(t, err)
}

// --- Delivery ---

func TestDelivery_RecordAndEscalate(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	team, err := db.CreateTeam(ctx, "platform", "")
	require.NoError(t, err)
	integration, err := db.CreateIntegration(ctx, "prom", team.ID, "webhook", "")
	require.NoError(t, err)
	user, err := db.CreateUser(ctx, "Alice", "alice@test.com", "", "", "", team.ID)
	require.NoError(t, err)
	a, err := db.CreateAlert(ctx, integration.ID, alert.IncomingAlert{
		Title: "disk full",
	})
	require.NoError(t, err)

	// Record successful delivery
	d, err := db.RecordDelivery(ctx, a.ID, user.ID, "slack", "U001", true, "")
	require.NoError(t, err)
	assert.NotEmpty(t, d.ID)
	assert.NotNil(t, d.DeliveredAt)
	assert.Nil(t, d.FailedAt)

	// Record failed delivery
	d2, err := db.RecordDelivery(ctx, a.ID, user.ID, "sms", "+1234", false, "timeout")
	require.NoError(t, err)
	assert.NotNil(t, d2.FailedAt)
	assert.Equal(t, "timeout", d2.FailureReason)

	// Mark escalated
	err = db.MarkDeliveryEscalated(ctx, a.ID)
	require.NoError(t, err)
}

// --- Cascade Deletes ---

func TestCascadeDelete_ScheduleDeletesCascadesToLayers(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	team, err := db.CreateTeam(ctx, "platform", "")
	require.NoError(t, err)
	user, err := db.CreateUser(ctx, "Alice", "alice@test.com", "", "", "", team.ID)
	require.NoError(t, err)

	s := &schedule.Schedule{
		Name:   "test",
		TeamID: team.ID,
		Layers: []schedule.Layer{
			{
				Priority: 1,
				Participants: []schedule.Participant{
					{UserID: user.ID, Position: 0},
				},
			},
		},
	}
	created, err := db.CreateSchedule(ctx, s)
	require.NoError(t, err)

	// Delete the schedule directly via SQL
	_, err = db.conn.ExecContext(ctx, `DELETE FROM schedules WHERE id = ?`, created.ID)
	require.NoError(t, err)

	// Layers should be cascade deleted
	var layerCount int
	err = db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM schedule_layers WHERE schedule_id = ?`, created.ID).Scan(&layerCount)
	require.NoError(t, err)
	assert.Equal(t, 0, layerCount)

	// Participants should also be cascade deleted (layer cascade)
	var partCount int
	err = db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM schedule_participants WHERE layer_id = ?`, created.Layers[0].ID).Scan(&partCount)
	require.NoError(t, err)
	assert.Equal(t, 0, partCount)
}

func TestCascadeDelete_PolicyDeletesCascadesToSteps(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	team, err := db.CreateTeam(ctx, "platform", "")
	require.NoError(t, err)

	p := &escalation.Policy{
		Name:   "test-policy",
		TeamID: team.ID,
		Steps: []escalation.Step{
			{StepOrder: 0, TimeoutSeconds: 300, NotifyChannel: "slack"},
		},
	}
	created, err := db.CreatePolicy(ctx, p)
	require.NoError(t, err)

	err = db.DeletePolicy(ctx, created.ID)
	require.NoError(t, err)

	var stepCount int
	err = db.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM escalation_steps WHERE policy_id = ?`, created.ID).Scan(&stepCount)
	require.NoError(t, err)
	assert.Equal(t, 0, stepCount)
}

// --- Foreign Key Constraints ---

func TestForeignKeys_ScheduleRequiresValidTeam(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	s := &schedule.Schedule{
		Name:   "orphan",
		TeamID: "nonexistent-team",
	}
	_, err := db.CreateSchedule(ctx, s)
	assert.Error(t, err, "should fail due to FK constraint on team_id")
}

func TestForeignKeys_UserFKOnTeam(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	_, err := db.CreateUser(ctx, "Orphan", "orphan@test.com", "", "", "", "nonexistent-team")
	assert.Error(t, err, "should fail due to FK constraint on team_id")
}
