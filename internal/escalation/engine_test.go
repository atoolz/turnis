package escalation

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Fake Store ---

type fakeStore struct {
	mu sync.Mutex

	alerts            map[string]Alert
	integrations      map[string]Integration
	policies          map[string]*Policy
	schedules         map[string]Schedule
	overrides         map[string][]Override
	users             map[string]User
	notificationRules map[string][]NotificationRule

	deliveries       []deliveryRecord
	escalatedAlerts  []string
}

type deliveryRecord struct {
	alertID, userID, channel, address string
	success                          bool
	failureReason                    string
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		alerts:            make(map[string]Alert),
		integrations:      make(map[string]Integration),
		policies:          make(map[string]*Policy),
		schedules:         make(map[string]Schedule),
		overrides:         make(map[string][]Override),
		users:             make(map[string]User),
		notificationRules: make(map[string][]NotificationRule),
	}
}

func (s *fakeStore) GetAlert(_ context.Context, id string) (Alert, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.alerts[id]
	if !ok {
		return Alert{}, fmt.Errorf("alert %q not found", id)
	}
	return a, nil
}

func (s *fakeStore) GetIntegration(_ context.Context, id string) (Integration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, ok := s.integrations[id]
	if !ok {
		return Integration{}, fmt.Errorf("integration %q not found", id)
	}
	return i, nil
}

func (s *fakeStore) GetPolicy(_ context.Context, id string) (*Policy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.policies[id]
	if !ok {
		return nil, fmt.Errorf("policy %q not found", id)
	}
	return p, nil
}

func (s *fakeStore) GetSchedule(_ context.Context, id string) (Schedule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sc, ok := s.schedules[id]
	if !ok {
		return Schedule{}, fmt.Errorf("schedule %q not found", id)
	}
	return sc, nil
}

func (s *fakeStore) GetOverrides(_ context.Context, scheduleID string) ([]Override, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.overrides[scheduleID], nil
}

func (s *fakeStore) GetUser(_ context.Context, id string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return User{}, fmt.Errorf("user %q not found", id)
	}
	return u, nil
}

func (s *fakeStore) GetNotificationRules(_ context.Context, userID string) ([]NotificationRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.notificationRules[userID], nil
}

func (s *fakeStore) RecordDelivery(_ context.Context, alertID, userID, channel, address string, success bool, failureReason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deliveries = append(s.deliveries, deliveryRecord{
		alertID:       alertID,
		userID:        userID,
		channel:       channel,
		address:       address,
		success:       success,
		failureReason: failureReason,
	})
	return nil
}

func (s *fakeStore) MarkDeliveryEscalated(_ context.Context, alertID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.escalatedAlerts = append(s.escalatedAlerts, alertID)
	return nil
}

// --- Fake Notifier ---

type notifyCall struct {
	channel    string
	address    string
	alert      Alert
	user       User
	ackURL     string
	resolveURL string
}

type fakeNotifier struct {
	mu    sync.Mutex
	calls []notifyCall
	// called is closed after the first Notify call to signal tests.
	called chan struct{}
	once   sync.Once
	// callCount tracks total calls atomically.
	callCount atomic.Int32
}

func newFakeNotifier() *fakeNotifier {
	return &fakeNotifier{
		called: make(chan struct{}),
	}
}

func (n *fakeNotifier) Notify(_ context.Context, channel, address string, a Alert, u User, ackURL, resolveURL string) (bool, error) {
	n.mu.Lock()
	n.calls = append(n.calls, notifyCall{
		channel:    channel,
		address:    address,
		alert:      a,
		user:       u,
		ackURL:     ackURL,
		resolveURL: resolveURL,
	})
	n.mu.Unlock()
	n.callCount.Add(1)
	n.once.Do(func() { close(n.called) })
	return true, nil
}

func (n *fakeNotifier) getCalls() []notifyCall {
	n.mu.Lock()
	defer n.mu.Unlock()
	cp := make([]notifyCall, len(n.calls))
	copy(cp, n.calls)
	return cp
}

// --- Test helpers ---

// seedDefaults populates the store with a minimal alert -> integration -> policy -> user chain.
// The policy has steps with short timeouts for fast tests.
func seedDefaults(store *fakeStore, policySteps []Step, policyRepeat int) {
	store.alerts["alert-1"] = Alert{
		ID:            "alert-1",
		IntegrationID: "int-1",
		Title:         "CPU high",
		Message:       "CPU > 90%",
		Severity:      "critical",
		Status:        "firing",
	}
	store.integrations["int-1"] = Integration{
		ID:                 "int-1",
		EscalationPolicyID: "pol-1",
	}
	store.policies["pol-1"] = &Policy{
		ID:     "pol-1",
		Name:   "default",
		TeamID: "team-1",
		Repeat: policyRepeat,
		Steps:  policySteps,
	}
	store.users["user-1"] = User{
		ID:    "user-1",
		Name:  "Alice",
		Email: "alice@example.com",
		Phone: "+1234567890",
	}
}

func defaultSteps() []Step {
	return []Step{
		{
			ID:             "step-1",
			PolicyID:       "pol-1",
			StepOrder:      0,
			TimeoutSeconds: 1, // 1s for fast tests
			NotifyUserID:   "user-1",
			NotifyChannel:  "email",
		},
	}
}

func twoSteps() []Step {
	return []Step{
		{
			ID:             "step-1",
			PolicyID:       "pol-1",
			StepOrder:      0,
			TimeoutSeconds: 1,
			NotifyUserID:   "user-1",
			NotifyChannel:  "email",
		},
		{
			ID:             "step-2",
			PolicyID:       "pol-1",
			StepOrder:      1,
			TimeoutSeconds: 1,
			NotifyUserID:   "user-1",
			NotifyChannel:  "sms",
		},
	}
}

// waitForNotifications waits until the notifier has at least n calls or the timeout fires.
func waitForNotifications(t *testing.T, notifier *fakeNotifier, n int, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		if int(notifier.callCount.Load()) >= n {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %d notifications, got %d", n, notifier.callCount.Load())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// --- Tests ---

func TestEngine_EnqueueStartsEscalation(t *testing.T) {
	store := newFakeStore()
	notifier := newFakeNotifier()
	seedDefaults(store, defaultSteps(), 1)

	eng := NewEngine(store, notifier, "http://localhost")
	defer eng.Shutdown()

	eng.Enqueue("alert-1")

	select {
	case <-notifier.called:
		// ok
	case <-time.After(2 * time.Second):
		t.Fatal("notifier was not called within 2s")
	}

	calls := notifier.getCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, "email", calls[0].channel)
	assert.Equal(t, "alice@example.com", calls[0].address)
	assert.Equal(t, "alert-1", calls[0].alert.ID)
	assert.Equal(t, "http://localhost/api/v1/alerts/alert-1/ack", calls[0].ackURL)
	assert.Equal(t, "http://localhost/api/v1/alerts/alert-1/resolve", calls[0].resolveURL)
}

func TestEngine_AcknowledgeCancelsTimer(t *testing.T) {
	store := newFakeStore()
	notifier := newFakeNotifier()
	steps := twoSteps()
	// Increase timeout so escalation won't fire before ack.
	steps[0].TimeoutSeconds = 10
	seedDefaults(store, steps, 1)

	eng := NewEngine(store, notifier, "http://localhost")
	defer eng.Shutdown()

	eng.Enqueue("alert-1")

	// Wait for first notification.
	select {
	case <-notifier.called:
	case <-time.After(2 * time.Second):
		t.Fatal("first notification not received")
	}

	eng.Acknowledge("alert-1")
	assert.Equal(t, 0, eng.ActiveCount())

	// Wait to confirm no second notification fires.
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, 1, int(notifier.callCount.Load()), "should have exactly 1 notification after ack")
}

func TestEngine_ResolveCancelsTimer(t *testing.T) {
	store := newFakeStore()
	notifier := newFakeNotifier()
	steps := twoSteps()
	steps[0].TimeoutSeconds = 10
	seedDefaults(store, steps, 1)

	eng := NewEngine(store, notifier, "http://localhost")
	defer eng.Shutdown()

	eng.Enqueue("alert-1")

	select {
	case <-notifier.called:
	case <-time.After(2 * time.Second):
		t.Fatal("first notification not received")
	}

	eng.Resolve("alert-1")
	assert.Equal(t, 0, eng.ActiveCount())

	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, 1, int(notifier.callCount.Load()), "should have exactly 1 notification after resolve")
}

func TestEngine_EscalatesOnTimeout(t *testing.T) {
	store := newFakeStore()
	notifier := newFakeNotifier()
	steps := twoSteps()
	// Very short timeout so escalation fires quickly.
	steps[0].TimeoutSeconds = 1
	steps[1].TimeoutSeconds = 10
	seedDefaults(store, steps, 1)

	eng := NewEngine(store, notifier, "http://localhost")
	defer eng.Shutdown()

	eng.Enqueue("alert-1")

	// Wait for both step-1 and step-2 notifications.
	waitForNotifications(t, notifier, 2, 5*time.Second)

	calls := notifier.getCalls()
	require.GreaterOrEqual(t, len(calls), 2)
	assert.Equal(t, "email", calls[0].channel)
	assert.Equal(t, "sms", calls[1].channel)
}

func TestEngine_ExhaustedStepsStopsEscalation(t *testing.T) {
	store := newFakeStore()
	notifier := newFakeNotifier()
	// Single step, repeat=1 (no repeat).
	steps := []Step{
		{
			ID:             "step-1",
			PolicyID:       "pol-1",
			StepOrder:      0,
			TimeoutSeconds: 1,
			NotifyUserID:   "user-1",
			NotifyChannel:  "email",
		},
	}
	seedDefaults(store, steps, 1)

	eng := NewEngine(store, notifier, "http://localhost")
	defer eng.Shutdown()

	eng.Enqueue("alert-1")

	// Wait for notification + timeout + cleanup.
	waitForNotifications(t, notifier, 1, 2*time.Second)
	// Wait for the timer to fire and exhaust.
	time.Sleep(2 * time.Second)

	assert.Equal(t, 1, int(notifier.callCount.Load()), "should have exactly 1 notification when steps exhaust")
	assert.Equal(t, 0, eng.ActiveCount(), "engine should have no active alerts after exhaustion")
}

func TestEngine_RepeatPolicy(t *testing.T) {
	store := newFakeStore()
	notifier := newFakeNotifier()
	steps := []Step{
		{
			ID:             "step-1",
			PolicyID:       "pol-1",
			StepOrder:      0,
			TimeoutSeconds: 1,
			NotifyUserID:   "user-1",
			NotifyChannel:  "email",
		},
	}
	// repeat=2 means step fires twice (repeat 0 and repeat 1).
	seedDefaults(store, steps, 2)

	eng := NewEngine(store, notifier, "http://localhost")
	defer eng.Shutdown()

	eng.Enqueue("alert-1")

	// Should get 2 notifications: step-1 repeat 0 and step-1 repeat 1.
	waitForNotifications(t, notifier, 2, 5*time.Second)

	calls := notifier.getCalls()
	require.GreaterOrEqual(t, len(calls), 2)
	assert.Equal(t, "email", calls[0].channel)
	assert.Equal(t, "email", calls[1].channel)
}

func TestEngine_DuplicateEnqueueIgnored(t *testing.T) {
	store := newFakeStore()
	notifier := newFakeNotifier()
	steps := defaultSteps()
	steps[0].TimeoutSeconds = 10
	seedDefaults(store, steps, 1)

	eng := NewEngine(store, notifier, "http://localhost")
	defer eng.Shutdown()

	eng.Enqueue("alert-1")
	eng.Enqueue("alert-1") // duplicate

	select {
	case <-notifier.called:
	case <-time.After(2 * time.Second):
		t.Fatal("first notification not received")
	}

	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, 1, int(notifier.callCount.Load()), "duplicate enqueue should not cause extra notification")
}

func TestEngine_ShutdownDrainsTimers(t *testing.T) {
	store := newFakeStore()
	notifier := newFakeNotifier()
	steps := defaultSteps()
	steps[0].TimeoutSeconds = 60 // long timeout so timer is still pending
	seedDefaults(store, steps, 1)

	eng := NewEngine(store, notifier, "http://localhost")

	eng.Enqueue("alert-1")

	select {
	case <-notifier.called:
	case <-time.After(2 * time.Second):
		t.Fatal("first notification not received")
	}

	eng.Shutdown()
	assert.Equal(t, 0, eng.ActiveCount(), "ActiveCount should be 0 after shutdown")
}

func TestEngine_ConcurrentEnqueueAcknowledge(t *testing.T) {
	store := newFakeStore()
	notifier := newFakeNotifier()
	seedDefaults(store, defaultSteps(), 1)

	// Add more alerts.
	for i := 2; i <= 10; i++ {
		id := fmt.Sprintf("alert-%d", i)
		store.alerts[id] = Alert{
			ID:            id,
			IntegrationID: "int-1",
			Title:         "test",
			Severity:      "warning",
			Status:        "firing",
		}
	}

	eng := NewEngine(store, notifier, "http://localhost")
	defer eng.Shutdown()

	var wg sync.WaitGroup
	for i := 1; i <= 10; i++ {
		id := fmt.Sprintf("alert-%d", i)
		wg.Add(2)
		go func(alertID string) {
			defer wg.Done()
			eng.Enqueue(alertID)
		}(id)
		go func(alertID string) {
			defer wg.Done()
			time.Sleep(50 * time.Millisecond)
			eng.Acknowledge(alertID)
		}(id)
	}
	wg.Wait()

	// Should not panic or deadlock. ActiveCount should eventually reach 0.
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, 0, eng.ActiveCount())
}

func TestEngine_ResolveChannel(t *testing.T) {
	// resolveChannel is unexported but testable from within the package.
	utc := time.Date(2025, 6, 15, 14, 30, 0, 0, time.UTC) // 14:30 UTC

	tests := []struct {
		name     string
		rules    []NotificationRule
		fallback string
		now      time.Time
		want     string
	}{
		{
			name:     "no rules returns fallback",
			rules:    nil,
			fallback: "email",
			now:      utc,
			want:     "email",
		},
		{
			name: "rule without time window always matches",
			rules: []NotificationRule{
				{Channel: "slack", Priority: 10},
			},
			fallback: "email",
			now:      utc,
			want:     "slack",
		},
		{
			name: "highest priority rule wins",
			rules: []NotificationRule{
				{Channel: "email", Priority: 1},
				{Channel: "sms", Priority: 5},
				{Channel: "slack", Priority: 10},
			},
			fallback: "email",
			now:      utc,
			want:     "slack",
		},
		{
			name: "normal time window match",
			rules: []NotificationRule{
				{Channel: "slack", Priority: 10, StartTime: "09:00", EndTime: "17:00", Timezone: "UTC"},
			},
			fallback: "email",
			now:      utc, // 14:30 is within 09:00-17:00
			want:     "slack",
		},
		{
			name: "normal time window no match",
			rules: []NotificationRule{
				{Channel: "slack", Priority: 10, StartTime: "09:00", EndTime: "12:00", Timezone: "UTC"},
			},
			fallback: "email",
			now:      utc, // 14:30 is outside 09:00-12:00
			want:     "email",
		},
		{
			name: "overnight window match before midnight",
			rules: []NotificationRule{
				{Channel: "sms", Priority: 10, StartTime: "22:00", EndTime: "08:00", Timezone: "UTC"},
			},
			fallback: "email",
			now:      time.Date(2025, 6, 15, 23, 0, 0, 0, time.UTC), // 23:00
			want:     "sms",
		},
		{
			name: "overnight window match after midnight",
			rules: []NotificationRule{
				{Channel: "sms", Priority: 10, StartTime: "22:00", EndTime: "08:00", Timezone: "UTC"},
			},
			fallback: "email",
			now:      time.Date(2025, 6, 15, 3, 0, 0, 0, time.UTC), // 03:00
			want:     "sms",
		},
		{
			name: "overnight window no match",
			rules: []NotificationRule{
				{Channel: "sms", Priority: 10, StartTime: "22:00", EndTime: "08:00", Timezone: "UTC"},
			},
			fallback: "email",
			now:      utc, // 14:30
			want:     "email",
		},
		{
			name: "higher priority time-bound rule takes precedence over lower always-on",
			rules: []NotificationRule{
				{Channel: "email", Priority: 1},
				{Channel: "slack", Priority: 10, StartTime: "09:00", EndTime: "17:00", Timezone: "UTC"},
			},
			fallback: "webhook",
			now:      utc,
			want:     "slack",
		},
		{
			name: "falls through time-bound miss to lower always-on rule",
			rules: []NotificationRule{
				{Channel: "email", Priority: 1},
				{Channel: "slack", Priority: 10, StartTime: "09:00", EndTime: "12:00", Timezone: "UTC"},
			},
			fallback: "webhook",
			now:      utc, // 14:30 misses slack window
			want:     "email",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveChannel(tt.rules, tt.fallback, tt.now)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEngine_ParseHHMM(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"00:00", 0},
		{"09:30", 9*60 + 30},
		{"23:59", 23*60 + 59},
		{"12:00", 12 * 60},
		// Invalid inputs
		{"", -1},
		{"9:30", -1},
		{"25:00", -1},
		{"12:60", -1},
		{"ab:cd", -1},
		{"12-00", -1},
		{"123:00", -1},
		{"12:0", -1},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseHHMM(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}
