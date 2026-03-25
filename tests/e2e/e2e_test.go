package e2e

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/atoolz/turnis/internal/alert"
	"github.com/atoolz/turnis/internal/config"
	"github.com/atoolz/turnis/internal/escalation"
	"github.com/atoolz/turnis/internal/schedule"
	"github.com/atoolz/turnis/internal/store"
)

// fakeNotifier records notifications for assertions.
type fakeNotifier struct {
	mu      sync.Mutex
	calls   []notifyCall
	succeed bool
}

type notifyCall struct {
	Channel string
	Address string
	AlertID string
	UserID  string
}

func (f *fakeNotifier) Notify(_ context.Context, channel, address string, a escalation.Alert, u escalation.User, _, _ string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, notifyCall{
		Channel: channel,
		Address: address,
		AlertID: a.ID,
		UserID:  u.ID,
	})
	if !f.succeed {
		return false, fmt.Errorf("fake notifier failure")
	}
	return true, nil
}

func (f *fakeNotifier) Calls() []notifyCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]notifyCall, len(f.calls))
	copy(cp, f.calls)
	return cp
}

// storeAdapter adapts *store.DB to escalation.Store.
type storeAdapter struct {
	db *store.DB
}

func (a *storeAdapter) GetAlert(ctx context.Context, id string) (escalation.Alert, error) {
	al, err := a.db.GetAlert(ctx, id)
	if err != nil {
		return escalation.Alert{}, err
	}
	return escalation.Alert{
		ID:            al.ID,
		IntegrationID: al.IntegrationID,
		Title:         al.Title,
		Message:       al.Message,
		Severity:      string(al.Severity),
		Status:        string(al.Status),
	}, nil
}

func (a *storeAdapter) GetIntegration(ctx context.Context, id string) (escalation.Integration, error) {
	i, err := a.db.GetIntegration(ctx, id)
	if err != nil {
		return escalation.Integration{}, err
	}
	return escalation.Integration{
		ID:                 i.ID,
		EscalationPolicyID: i.EscalationPolicyID,
	}, nil
}

func (a *storeAdapter) GetPolicy(ctx context.Context, id string) (*escalation.Policy, error) {
	return a.db.GetPolicy(ctx, id)
}

func (a *storeAdapter) GetSchedule(ctx context.Context, id string) (escalation.Schedule, error) {
	s, err := a.db.GetSchedule(ctx, id)
	if err != nil {
		return escalation.Schedule{}, err
	}

	sched := escalation.Schedule{
		ID:             s.ID,
		Timezone:       s.Timezone,
		RotationType:   escalation.RotationType(s.RotationType),
		RotationLength: s.RotationLength,
		HandoffTime:    s.HandoffTime,
		HandoffDay:     s.HandoffDay,
		CreatedAt:      s.CreatedAt,
	}

	for _, l := range s.Layers {
		layer := escalation.Layer{Priority: l.Priority}
		for _, p := range l.Participants {
			layer.Participants = append(layer.Participants, escalation.Participant{UserID: p.UserID})
		}
		sched.Layers = append(sched.Layers, layer)
	}

	return sched, nil
}

func (a *storeAdapter) GetOverrides(ctx context.Context, scheduleID string) ([]escalation.Override, error) {
	ovs, err := a.db.GetOverrides(ctx, scheduleID)
	if err != nil {
		return nil, err
	}
	result := make([]escalation.Override, len(ovs))
	for i, o := range ovs {
		result[i] = escalation.Override{
			UserID:    o.UserID,
			StartTime: o.StartTime,
			EndTime:   o.EndTime,
		}
	}
	return result, nil
}

func (a *storeAdapter) GetUser(ctx context.Context, id string) (escalation.User, error) {
	u, err := a.db.GetUser(ctx, id)
	if err != nil {
		return escalation.User{}, err
	}
	return escalation.User{
		ID:        u.ID,
		Name:      u.Name,
		Email:     u.Email,
		Phone:     u.Phone,
		SlackID:   u.SlackID,
		NtfyTopic: u.NtfyTopic,
	}, nil
}

func (a *storeAdapter) GetNotificationRules(ctx context.Context, userID string) ([]escalation.NotificationRule, error) {
	rules, err := a.db.ListNotificationRules(ctx, userID)
	if err != nil {
		return nil, err
	}
	result := make([]escalation.NotificationRule, len(rules))
	for i, r := range rules {
		result[i] = escalation.NotificationRule{
			Channel:   r.Channel,
			Priority:  r.Priority,
			StartTime: r.StartTime,
			EndTime:   r.EndTime,
			Timezone:  r.Timezone,
		}
	}
	return result, nil
}

func (a *storeAdapter) RecordDelivery(ctx context.Context, alertID, userID, channel, address string, success bool, failureReason string) error {
	_, err := a.db.RecordDelivery(ctx, alertID, userID, channel, address, success, failureReason)
	return err
}

func (a *storeAdapter) MarkDeliveryEscalated(ctx context.Context, alertID string) error {
	return a.db.MarkDeliveryEscalated(ctx, alertID)
}

func setupTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.New(config.DatabaseConfig{
		Driver: "sqlite",
		DSN:    fmt.Sprintf("file:%s?mode=memory&cache=private", t.Name()),
	})
	require.NoError(t, err)
	require.NoError(t, db.Migrate())
	t.Cleanup(func() { db.Close() })
	return db
}

func TestFullLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx := context.Background()
	db := setupTestDB(t)

	// 1. Create team
	team, err := db.CreateTeam(ctx, "e2e-team", "#e2e-oncall")
	require.NoError(t, err)

	// 2. Create user
	user, err := db.CreateUser(ctx, "Alice", "alice@e2e.test", "+15551234567", "U_ALICE", "", team.ID)
	require.NoError(t, err)

	// 3. Create schedule with one participant
	sched, err := db.CreateSchedule(ctx, &schedule.Schedule{
		Name:   "e2e-primary",
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

	// 4. Create escalation policy pointing at the schedule
	policy, err := db.CreatePolicy(ctx, &escalation.Policy{
		Name:   "e2e-policy",
		TeamID: team.ID,
		Repeat: 1,
		Steps: []escalation.Step{
			{
				StepOrder:        0,
				TimeoutSeconds:   1, // 1 second so the test does not wait long
				NotifyScheduleID: sched.ID,
				NotifyChannel:    "slack",
			},
		},
	})
	require.NoError(t, err)

	// 5. Create integration linked to the policy
	integration, err := db.CreateIntegration(ctx, "e2e-webhook", team.ID, "webhook", policy.ID)
	require.NoError(t, err)

	// 6. Fire an alert
	a, err := db.CreateAlert(ctx, integration.ID, alert.IncomingAlert{
		Title:    "E2E test alert",
		Message:  "Something is broken",
		Severity: alert.SeverityCritical,
		Source:   "e2e-test",
	})
	require.NoError(t, err)
	assert.Equal(t, alert.StatusFiring, a.Status)

	// 7. Set up fake notifier and engine
	notifier := &fakeNotifier{succeed: true}
	sa := &storeAdapter{db: db}
	engine := escalation.NewEngine(sa, notifier, "http://localhost:8080")
	defer engine.Shutdown()

	// 8. Enqueue the alert for escalation
	engine.Enqueue(a.ID)

	// 9. Wait for escalation step to fire (give it some time for async processing)
	require.Eventually(t, func() bool {
		return len(notifier.Calls()) >= 1
	}, 5*time.Second, 50*time.Millisecond, "expected at least one notification call")

	// 10. Verify notification was sent to the right user
	calls := notifier.Calls()
	assert.Equal(t, "slack", calls[0].Channel)
	assert.Equal(t, a.ID, calls[0].AlertID)
	assert.Equal(t, user.ID, calls[0].UserID)

	// 11. Verify delivery attempt was recorded
	deliveries, err := db.ListRecentDeliveries(ctx, 10)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(deliveries), 1)

	found := false
	for _, d := range deliveries {
		if d.AlertID == a.ID && d.UserID == user.ID {
			found = true
			assert.Equal(t, "slack", d.Channel)
			break
		}
	}
	assert.True(t, found, "delivery attempt for the alert should be recorded")

	// 12. Acknowledge alert
	acked, err := db.AcknowledgeAlert(ctx, a.ID, user.ID)
	require.NoError(t, err)
	assert.Equal(t, alert.StatusAcknowledged, acked.Status)

	// 13. Tell the engine the alert was acknowledged
	engine.Acknowledge(a.ID)

	// 14. Verify engine stopped tracking the alert
	assert.Equal(t, 0, engine.ActiveCount(), "engine should have no active escalations after ack")
}

func TestFullLifecycle_ResolveStopsEscalation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx := context.Background()
	db := setupTestDB(t)

	team, err := db.CreateTeam(ctx, "resolve-team", "")
	require.NoError(t, err)
	user, err := db.CreateUser(ctx, "Bob", "bob@e2e.test", "", "", "", team.ID)
	require.NoError(t, err)

	sched, err := db.CreateSchedule(ctx, &schedule.Schedule{
		Name:   "resolve-sched",
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

	policy, err := db.CreatePolicy(ctx, &escalation.Policy{
		Name:   "resolve-policy",
		TeamID: team.ID,
		Repeat: 1,
		Steps: []escalation.Step{
			{
				StepOrder:        0,
				TimeoutSeconds:   60, // Long timeout so it does not escalate before we resolve
				NotifyScheduleID: sched.ID,
				NotifyChannel:    "slack",
			},
		},
	})
	require.NoError(t, err)

	integration, err := db.CreateIntegration(ctx, "resolve-hook", team.ID, "webhook", policy.ID)
	require.NoError(t, err)

	a, err := db.CreateAlert(ctx, integration.ID, alert.IncomingAlert{
		Title:    "resolve test",
		Severity: alert.SeverityWarning,
	})
	require.NoError(t, err)

	notifier := &fakeNotifier{succeed: true}
	sa := &storeAdapter{db: db}
	engine := escalation.NewEngine(sa, notifier, "http://localhost:8080")
	defer engine.Shutdown()

	engine.Enqueue(a.ID)

	// Wait for notification
	require.Eventually(t, func() bool {
		return len(notifier.Calls()) >= 1
	}, 5*time.Second, 50*time.Millisecond)

	// Resolve the alert
	resolved, err := db.ResolveAlert(ctx, a.ID)
	require.NoError(t, err)
	assert.Equal(t, alert.StatusResolved, resolved.Status)

	engine.Resolve(a.ID)
	assert.Equal(t, 0, engine.ActiveCount())
}

func TestE2E_WebhookIngestAndDedup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx := context.Background()
	db := setupTestDB(t)

	team, err := db.CreateTeam(ctx, "dedup-team", "")
	require.NoError(t, err)

	integration, err := db.CreateIntegration(ctx, "dedup-hook", team.ID, "webhook", "")
	require.NoError(t, err)

	incoming := alert.IncomingAlert{
		Title:       "Disk full",
		Severity:    alert.SeverityCritical,
		Source:      "node-exporter",
		Fingerprint: "disk-full-abc123",
	}

	// First alert should be created
	a1, err := db.CreateAlert(ctx, integration.ID, incoming)
	require.NoError(t, err)
	assert.Equal(t, alert.StatusFiring, a1.Status)

	// Simulate dedup check: look for existing alerts with same fingerprint
	existing, err := db.GetAlertsByFingerprint(ctx, integration.ID, incoming.Fingerprint)
	require.NoError(t, err)

	dup := alert.Deduplicate(existing, incoming)
	require.NotNil(t, dup, "second firing with same fingerprint should match existing alert")
	assert.Equal(t, a1.ID, dup.ID)

	// After resolving, dedup should NOT match
	_, err = db.ResolveAlert(ctx, a1.ID)
	require.NoError(t, err)

	existing2, err := db.GetAlertsByFingerprint(ctx, integration.ID, incoming.Fingerprint)
	require.NoError(t, err)

	dup2 := alert.Deduplicate(existing2, incoming)
	assert.Nil(t, dup2, "resolved alert should not match dedup")
}

func TestE2E_OverrideChangesOnCall(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx := context.Background()
	db := setupTestDB(t)

	team, err := db.CreateTeam(ctx, "override-team", "")
	require.NoError(t, err)

	alice, err := db.CreateUser(ctx, "Alice Primary", "alice@override.test", "", "", "", team.ID)
	require.NoError(t, err)

	bob, err := db.CreateUser(ctx, "Bob Override", "bob@override.test", "", "", "", team.ID)
	require.NoError(t, err)

	sched, err := db.CreateSchedule(ctx, &schedule.Schedule{
		Name:   "override-sched",
		TeamID: team.ID,
		Layers: []schedule.Layer{
			{
				Priority: 1,
				Participants: []schedule.Participant{
					{UserID: alice.ID, Position: 0},
				},
			},
		},
	})
	require.NoError(t, err)

	// Without override, Alice is on-call
	overrides, err := db.GetOverrides(ctx, sched.ID)
	require.NoError(t, err)

	onCall := schedule.WhosOnCall(sched, overrides, time.Now())
	assert.Equal(t, alice.ID, onCall, "Alice should be on-call without override")

	// Create override for Bob covering now
	now := time.Now().UTC()
	_, err = db.CreateOverride(ctx, sched.ID, bob.ID, now.Add(-1*time.Hour), now.Add(24*time.Hour), "Alice is sick")
	require.NoError(t, err)

	// With override, Bob is on-call
	overrides2, err := db.GetOverrides(ctx, sched.ID)
	require.NoError(t, err)

	onCall2 := schedule.WhosOnCall(sched, overrides2, time.Now())
	assert.Equal(t, bob.ID, onCall2, "Bob should be on-call with active override")
}

func TestE2E_MultiStepEscalation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx := context.Background()
	db := setupTestDB(t)

	team, err := db.CreateTeam(ctx, "multi-team", "")
	require.NoError(t, err)

	alice, err := db.CreateUser(ctx, "Alice Step1", "alice@multi.test", "", "U_ALICE_M", "", team.ID)
	require.NoError(t, err)

	bob, err := db.CreateUser(ctx, "Bob Step2", "bob@multi.test", "+15559990000", "", "", team.ID)
	require.NoError(t, err)

	sched1, err := db.CreateSchedule(ctx, &schedule.Schedule{
		Name:   "multi-sched-1",
		TeamID: team.ID,
		Layers: []schedule.Layer{
			{
				Priority: 1,
				Participants: []schedule.Participant{
					{UserID: alice.ID, Position: 0},
				},
			},
		},
	})
	require.NoError(t, err)

	sched2, err := db.CreateSchedule(ctx, &schedule.Schedule{
		Name:   "multi-sched-2",
		TeamID: team.ID,
		Layers: []schedule.Layer{
			{
				Priority: 1,
				Participants: []schedule.Participant{
					{UserID: bob.ID, Position: 0},
				},
			},
		},
	})
	require.NoError(t, err)

	policy, err := db.CreatePolicy(ctx, &escalation.Policy{
		Name:   "multi-step-policy",
		TeamID: team.ID,
		Repeat: 1,
		Steps: []escalation.Step{
			{
				StepOrder:        0,
				TimeoutSeconds:   1,
				NotifyScheduleID: sched1.ID,
				NotifyChannel:    "slack",
			},
			{
				StepOrder:        1,
				TimeoutSeconds:   1,
				NotifyScheduleID: sched2.ID,
				NotifyChannel:    "sms",
			},
		},
	})
	require.NoError(t, err)

	integration, err := db.CreateIntegration(ctx, "multi-hook", team.ID, "webhook", policy.ID)
	require.NoError(t, err)

	a, err := db.CreateAlert(ctx, integration.ID, alert.IncomingAlert{
		Title:    "Multi-step test",
		Severity: alert.SeverityCritical,
	})
	require.NoError(t, err)

	notifier := &fakeNotifier{succeed: true}
	sa := &storeAdapter{db: db}
	engine := escalation.NewEngine(sa, notifier, "http://localhost:8080")
	defer engine.Shutdown()

	engine.Enqueue(a.ID)

	// Wait for both steps to fire (step 0 fires immediately, step 1 after 1s timeout)
	require.Eventually(t, func() bool {
		return len(notifier.Calls()) >= 2
	}, 10*time.Second, 100*time.Millisecond, "expected at least 2 notification calls for 2 escalation steps")

	calls := notifier.Calls()

	// Step 0 should notify Alice via slack
	assert.Equal(t, "slack", calls[0].Channel)
	assert.Equal(t, alice.ID, calls[0].UserID)
	assert.Equal(t, a.ID, calls[0].AlertID)

	// Step 1 should notify Bob via sms
	assert.Equal(t, "sms", calls[1].Channel)
	assert.Equal(t, bob.ID, calls[1].UserID)
	assert.Equal(t, a.ID, calls[1].AlertID)
}

func TestE2E_ConcurrentAlerts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx := context.Background()
	db := setupTestDB(t)

	team, err := db.CreateTeam(ctx, "concurrent-team", "")
	require.NoError(t, err)

	user, err := db.CreateUser(ctx, "Concurrent User", "concurrent@test.test", "", "U_CONC", "", team.ID)
	require.NoError(t, err)

	sched, err := db.CreateSchedule(ctx, &schedule.Schedule{
		Name:   "concurrent-sched",
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

	policy, err := db.CreatePolicy(ctx, &escalation.Policy{
		Name:   "concurrent-policy",
		TeamID: team.ID,
		Repeat: 1,
		Steps: []escalation.Step{
			{
				StepOrder:        0,
				TimeoutSeconds:   60,
				NotifyScheduleID: sched.ID,
				NotifyChannel:    "slack",
			},
		},
	})
	require.NoError(t, err)

	integration, err := db.CreateIntegration(ctx, "concurrent-hook", team.ID, "webhook", policy.ID)
	require.NoError(t, err)

	notifier := &fakeNotifier{succeed: true}
	sa := &storeAdapter{db: db}
	engine := escalation.NewEngine(sa, notifier, "http://localhost:8080")
	defer engine.Shutdown()

	const alertCount = 10
	alertIDs := make([]string, alertCount)

	// Create all alerts first
	for i := 0; i < alertCount; i++ {
		a, err := db.CreateAlert(ctx, integration.ID, alert.IncomingAlert{
			Title:    fmt.Sprintf("Concurrent alert %d", i),
			Severity: alert.SeverityWarning,
			Source:   "load-test",
		})
		require.NoError(t, err)
		alertIDs[i] = a.ID
	}

	// Enqueue all concurrently
	var wg sync.WaitGroup
	for _, id := range alertIDs {
		wg.Add(1)
		go func(alertID string) {
			defer wg.Done()
			engine.Enqueue(alertID)
		}(id)
	}
	wg.Wait()

	// Wait for all notifications
	require.Eventually(t, func() bool {
		return len(notifier.Calls()) >= alertCount
	}, 15*time.Second, 100*time.Millisecond,
		"expected at least %d notifications, got %d", alertCount, len(notifier.Calls()))

	calls := notifier.Calls()
	assert.GreaterOrEqual(t, len(calls), alertCount)

	// Verify every alert ID was notified
	notified := make(map[string]bool)
	for _, c := range calls {
		notified[c.AlertID] = true
	}
	for _, id := range alertIDs {
		assert.True(t, notified[id], "alert %s should have been notified", id)
	}
}
