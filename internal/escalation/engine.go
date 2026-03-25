package escalation

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Store defines the database operations the engine needs.
type Store interface {
	GetAlert(ctx context.Context, id string) (Alert, error)
	GetIntegration(ctx context.Context, id string) (Integration, error)
	GetPolicy(ctx context.Context, id string) (*Policy, error)
	GetSchedule(ctx context.Context, id string) (Schedule, error)
	GetOverrides(ctx context.Context, scheduleID string) ([]Override, error)
	GetUser(ctx context.Context, id string) (User, error)
	RecordDelivery(ctx context.Context, alertID, userID, channel, address string, success bool, failureReason string) error
	MarkDeliveryEscalated(ctx context.Context, alertID string) error
}

type Alert struct {
	ID            string
	IntegrationID string
	Title         string
	Message       string
	Severity      string
	Status        string
}

type Integration struct {
	ID                 string
	EscalationPolicyID string
}

// RotationType mirrors schedule.RotationType to avoid import cycles.
type RotationType = string

type Schedule struct {
	ID             string
	Timezone       string
	RotationType   RotationType
	RotationLength int
	HandoffTime    string
	HandoffDay     string
	Layers         []Layer
	CreatedAt      time.Time
}

type Layer struct {
	Priority     int
	Participants []Participant
}

type Participant struct {
	UserID string
}

type Override struct {
	UserID    string
	StartTime time.Time
	EndTime   time.Time
}

type User struct {
	ID        string
	Name      string
	Email     string
	Phone     string
	SlackID   string
	NtfyTopic string
}

type Notifier interface {
	Notify(ctx context.Context, channel, address string, a Alert, u User, ackURL, resolveURL string) (bool, error)
}

// trackedAlert holds the state of an in-flight escalation.
// All fields are protected by Engine.mu.
type trackedAlert struct {
	alertID       string
	policyID      string
	currentStep   int
	currentRepeat int
	timer         *time.Timer
	cancelled     bool
}

// Engine drives the escalation process for active alerts.
type Engine struct {
	store    Store
	notifier Notifier
	baseURL  string

	mu      sync.Mutex
	tracked map[string]*trackedAlert
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
}

func NewEngine(store Store, notifier Notifier, baseURL string) *Engine {
	ctx, cancel := context.WithCancel(context.Background())
	return &Engine{
		store:    store,
		notifier: notifier,
		baseURL:  baseURL,
		tracked:  make(map[string]*trackedAlert),
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Enqueue starts the escalation chain for an alert.
func (e *Engine) Enqueue(alertID string) {
	e.mu.Lock()
	if _, exists := e.tracked[alertID]; exists {
		e.mu.Unlock()
		return
	}

	ta := &trackedAlert{
		alertID:     alertID,
		currentStep: -1,
	}
	e.tracked[alertID] = ta
	e.mu.Unlock()

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.startEscalation(alertID)
	}()
}

func (e *Engine) startEscalation(alertID string) {
	ctx := e.ctx

	a, err := e.store.GetAlert(ctx, alertID)
	if err != nil {
		slog.Error("escalation: failed to get alert", "alert_id", alertID, "error", err)
		e.cleanup(alertID)
		return
	}

	integration, err := e.store.GetIntegration(ctx, a.IntegrationID)
	if err != nil {
		slog.Error("escalation: failed to get integration", "alert_id", alertID, "error", err)
		e.cleanup(alertID)
		return
	}

	if integration.EscalationPolicyID == "" {
		slog.Warn("escalation: integration has no escalation policy", "alert_id", alertID)
		e.cleanup(alertID)
		return
	}

	policy, err := e.store.GetPolicy(ctx, integration.EscalationPolicyID)
	if err != nil {
		slog.Error("escalation: failed to get policy", "alert_id", alertID, "error", err)
		e.cleanup(alertID)
		return
	}

	if len(policy.Steps) == 0 {
		slog.Warn("escalation: policy has no steps", "alert_id", alertID)
		e.cleanup(alertID)
		return
	}

	e.mu.Lock()
	ta, exists := e.tracked[alertID]
	if !exists || ta.cancelled {
		e.mu.Unlock()
		return
	}
	ta.policyID = policy.ID
	e.mu.Unlock()

	e.advanceToNextStep(alertID, policy)
}

func (e *Engine) advanceToNextStep(alertID string, policy *Policy) {
	e.mu.Lock()
	ta, exists := e.tracked[alertID]
	if !exists || ta.cancelled {
		e.mu.Unlock()
		return
	}

	step := NextStep(policy, ta.currentStep, ta.currentRepeat)
	if step == nil {
		e.mu.Unlock()
		slog.Info("escalation: exhausted all steps", "alert_id", alertID, "policy_id", ta.policyID)
		e.cleanup(alertID)
		return
	}

	ta.currentStep++
	if ta.currentStep >= len(policy.Steps) {
		ta.currentStep = 0
		ta.currentRepeat++
	}

	stepCopy := *step
	e.mu.Unlock()

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.executeStep(alertID, policy, &stepCopy)
	}()
}

func (e *Engine) executeStep(alertID string, policy *Policy, step *Step) {
	ctx := e.ctx

	e.mu.Lock()
	ta, exists := e.tracked[alertID]
	if !exists || ta.cancelled {
		e.mu.Unlock()
		return
	}
	e.mu.Unlock()

	a, err := e.store.GetAlert(ctx, alertID)
	if err != nil {
		slog.Error("escalation: failed to refresh alert", "alert_id", alertID, "error", err)
		e.cleanup(alertID)
		return
	}

	if a.Status != "firing" {
		slog.Info("escalation: alert no longer firing, stopping", "alert_id", alertID, "status", a.Status)
		e.cleanup(alertID)
		return
	}

	userID, err := e.resolveTargetUser(ctx, step)
	if err != nil {
		slog.Error("escalation: failed to resolve target user", "alert_id", alertID, "error", err)
		e.scheduleTimeout(alertID, policy, step)
		return
	}

	if userID == "" {
		slog.Warn("escalation: no on-call user found", "alert_id", alertID, "step", step.StepOrder)
		e.scheduleTimeout(alertID, policy, step)
		return
	}

	user, err := e.store.GetUser(ctx, userID)
	if err != nil {
		slog.Error("escalation: failed to get user", "alert_id", alertID, "user_id", userID, "error", err)
		e.scheduleTimeout(alertID, policy, step)
		return
	}

	address := resolveAddress(user, step.NotifyChannel)
	ackURL := fmt.Sprintf("%s/api/v1/alerts/%s/ack", e.baseURL, alertID)
	resolveURL := fmt.Sprintf("%s/api/v1/alerts/%s/resolve", e.baseURL, alertID)

	slog.Info("escalation: notifying user",
		"alert_id", alertID,
		"user", user.Name,
		"channel", step.NotifyChannel,
		"step", step.StepOrder,
	)

	success, notifyErr := e.notifier.Notify(ctx, step.NotifyChannel, address, a, user, ackURL, resolveURL)

	failReason := ""
	if notifyErr != nil {
		failReason = notifyErr.Error()
		slog.Error("escalation: notification failed",
			"alert_id", alertID,
			"user_id", user.ID,
			"channel", step.NotifyChannel,
			"error", notifyErr,
		)
	}
	_ = success

	if recordErr := e.store.RecordDelivery(ctx, alertID, user.ID, step.NotifyChannel, address, success, failReason); recordErr != nil {
		slog.Error("escalation: failed to record delivery", "error", recordErr)
	}

	e.scheduleTimeout(alertID, policy, step)
}

func (e *Engine) scheduleTimeout(alertID string, policy *Policy, step *Step) {
	timeout := StepTimeout(step)

	e.mu.Lock()
	ta, exists := e.tracked[alertID]
	if !exists || ta.cancelled {
		e.mu.Unlock()
		return
	}

	ta.timer = time.AfterFunc(timeout, func() {
		// Check if alert is still tracked before any DB writes
		e.mu.Lock()
		_, exists := e.tracked[alertID]
		if !exists {
			e.mu.Unlock()
			return
		}
		e.mu.Unlock()

		if err := e.store.MarkDeliveryEscalated(e.ctx, alertID); err != nil {
			slog.Error("escalation: failed to mark escalated", "alert_id", alertID, "error", err)
		}

		slog.Info("escalation: step timed out, escalating",
			"alert_id", alertID,
			"step", step.StepOrder,
			"timeout", timeout,
		)

		e.advanceToNextStep(alertID, policy)
	})
	e.mu.Unlock()
}

func (e *Engine) resolveTargetUser(ctx context.Context, step *Step) (string, error) {
	if step.NotifyUserID != "" {
		return step.NotifyUserID, nil
	}

	if step.NotifyScheduleID != "" {
		sched, err := e.store.GetSchedule(ctx, step.NotifyScheduleID)
		if err != nil {
			return "", fmt.Errorf("getting schedule %s: %w", step.NotifyScheduleID, err)
		}

		overrides, err := e.store.GetOverrides(ctx, sched.ID)
		if err != nil {
			return "", fmt.Errorf("getting overrides for schedule %s: %w", sched.ID, err)
		}

		s := toScheduleModel(sched)
		ovs := toOverrideModels(overrides)

		return WhosOnCallFromEngine(s, ovs, time.Now()), nil
	}

	return "", nil
}

// Acknowledge cancels the pending escalation for an alert.
func (e *Engine) Acknowledge(alertID string) {
	e.cleanup(alertID)
	slog.Info("escalation: acknowledged, timer cancelled", "alert_id", alertID)
}

// Resolve cancels the pending escalation for an alert.
func (e *Engine) Resolve(alertID string) {
	e.cleanup(alertID)
	slog.Info("escalation: resolved, timer cancelled", "alert_id", alertID)
}

func (e *Engine) cleanup(alertID string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if ta, exists := e.tracked[alertID]; exists {
		ta.cancelled = true
		if ta.timer != nil {
			ta.timer.Stop()
		}
		delete(e.tracked, alertID)
	}
}

// ActiveCount returns the number of alerts currently being escalated.
func (e *Engine) ActiveCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.tracked)
}

// Shutdown gracefully stops the engine.
func (e *Engine) Shutdown() {
	e.cancel()

	e.mu.Lock()
	count := len(e.tracked)
	for alertID, ta := range e.tracked {
		ta.cancelled = true
		if ta.timer != nil {
			ta.timer.Stop()
		}
		delete(e.tracked, alertID)
	}
	e.mu.Unlock()

	e.wg.Wait()

	slog.Info("escalation engine stopped", "cancelled_timers", count)
}

func resolveAddress(user User, channel string) string {
	switch channel {
	case "slack":
		if user.SlackID != "" {
			return user.SlackID
		}
		return user.Email
	case "sms", "voice":
		return user.Phone
	case "push":
		return user.NtfyTopic
	case "email":
		return user.Email
	default:
		return user.Email
	}
}
