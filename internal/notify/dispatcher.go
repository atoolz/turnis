package notify

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"strings"
	"sync"
	"time"
)

type Channel string

const (
	ChannelSlack   Channel = "slack"
	ChannelSMS     Channel = "sms"
	ChannelVoice   Channel = "voice"
	ChannelPush    Channel = "push"
	ChannelWebhook Channel = "webhook"
	ChannelEmail   Channel = "email"
)

const (
	maxRetries     = 3
	baseBackoffSec = 1
	maxJitterPct   = 0.25
)

type Message struct {
	AlertID    string
	UserID     string
	Channel    Channel
	Address    string
	Title      string
	Body       string
	Severity   string
	AckURL     string
	ResolveURL string
	Urgent     bool
}

type DeliveryResult struct {
	MessageID  string
	ChannelRef string // Platform-specific channel identifier (e.g., Slack channel ID)
	Channel    Channel
	Success    bool
	Error      string
	SentAt     time.Time
	RetryCount int
}

type Sender interface {
	Send(ctx context.Context, msg Message) (*DeliveryResult, error)
	Name() Channel
}

// RetryRecorder is called after each retry attempt to persist retry_count.
type RetryRecorder interface {
	UpdateDeliveryRetry(ctx context.Context, id string, retryCount int, failureReason string) error
}

// StatusError wraps an error with an HTTP status code for transient detection.
type StatusError struct {
	StatusCode int
	Err        error
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("status %d: %s", e.StatusCode, e.Err.Error())
}

func (e *StatusError) Unwrap() error {
	return e.Err
}

// NewStatusError creates a StatusError with the given code and underlying error.
func NewStatusError(statusCode int, err error) *StatusError {
	return &StatusError{StatusCode: statusCode, Err: err}
}

type Dispatcher struct {
	mu      sync.RWMutex
	senders map[Channel]Sender
}

func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		senders: make(map[Channel]Sender),
	}
}

func (d *Dispatcher) Register(sender Sender) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.senders[sender.Name()] = sender
	slog.Info("notification channel registered", "channel", sender.Name())
}

func (d *Dispatcher) Dispatch(ctx context.Context, msg Message) (*DeliveryResult, error) {
	d.mu.RLock()
	sender, ok := d.senders[msg.Channel]
	d.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("no sender registered for channel %q", msg.Channel)
	}

	result, err := sender.Send(ctx, msg)
	if err != nil {
		slog.Error("notification dispatch failed",
			"channel", msg.Channel,
			"alert_id", msg.AlertID,
			"user_id", msg.UserID,
			"error", err,
		)
		return &DeliveryResult{
			Channel: msg.Channel,
			Success: false,
			Error:   err.Error(),
			SentAt:  time.Now(),
		}, err
	}

	slog.Info("notification dispatched",
		"channel", msg.Channel,
		"alert_id", msg.AlertID,
		"user_id", msg.UserID,
	)

	return result, nil
}

// DispatchWithRetry sends a message, retrying transient failures up to maxRetries
// times with exponential backoff and jitter. Non-transient errors fail immediately.
// If recorder is non-nil and deliveryID is provided, retry_count is updated in the
// store after each retry attempt.
func (d *Dispatcher) DispatchWithRetry(ctx context.Context, msg Message, deliveryID string, recorder RetryRecorder) (*DeliveryResult, error) {
	d.mu.RLock()
	sender, ok := d.senders[msg.Channel]
	d.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("no sender registered for channel %q", msg.Channel)
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoff := backoffDuration(attempt)

			slog.Warn("retrying notification dispatch",
				"channel", msg.Channel,
				"alert_id", msg.AlertID,
				"user_id", msg.UserID,
				"attempt", attempt,
				"backoff", backoff,
			)

			select {
			case <-ctx.Done():
				return &DeliveryResult{
					Channel:    msg.Channel,
					Success:    false,
					Error:      ctx.Err().Error(),
					SentAt:     time.Now(),
					RetryCount: attempt,
				}, ctx.Err()
			case <-time.After(backoff):
			}

			// Update retry count in store if recorder is available.
			if recorder != nil && deliveryID != "" {
				reason := ""
				if lastErr != nil {
					reason = lastErr.Error()
				}
				if err := recorder.UpdateDeliveryRetry(ctx, deliveryID, attempt, reason); err != nil {
					slog.Error("failed to update delivery retry count",
						"delivery_id", deliveryID,
						"retry_count", attempt,
						"error", err,
					)
				}
			}
		}

		result, err := sender.Send(ctx, msg)
		if err == nil {
			if result != nil {
				result.RetryCount = attempt
			}
			slog.Info("notification dispatched",
				"channel", msg.Channel,
				"alert_id", msg.AlertID,
				"user_id", msg.UserID,
				"attempts", attempt+1,
			)
			return result, nil
		}

		lastErr = err

		if !isTransient(err) {
			slog.Error("notification dispatch failed with non-transient error",
				"channel", msg.Channel,
				"alert_id", msg.AlertID,
				"user_id", msg.UserID,
				"error", err,
			)
			return &DeliveryResult{
				Channel:    msg.Channel,
				Success:    false,
				Error:      err.Error(),
				SentAt:     time.Now(),
				RetryCount: attempt,
			}, err
		}

		slog.Warn("transient notification failure",
			"channel", msg.Channel,
			"alert_id", msg.AlertID,
			"user_id", msg.UserID,
			"attempt", attempt+1,
			"error", err,
		)
	}

	// All retries exhausted.
	slog.Error("notification dispatch permanently failed after retries",
		"channel", msg.Channel,
		"alert_id", msg.AlertID,
		"user_id", msg.UserID,
		"attempts", maxRetries+1,
		"error", lastErr,
	)

	// Record final retry count.
	if recorder != nil && deliveryID != "" {
		reason := ""
		if lastErr != nil {
			reason = lastErr.Error()
		}
		if err := recorder.UpdateDeliveryRetry(ctx, deliveryID, maxRetries, reason); err != nil {
			slog.Error("failed to update delivery retry count after exhaustion",
				"delivery_id", deliveryID,
				"error", err,
			)
		}
	}

	return &DeliveryResult{
		Channel:    msg.Channel,
		Success:    false,
		Error:      lastErr.Error(),
		SentAt:     time.Now(),
		RetryCount: maxRetries,
	}, lastErr
}

func (d *Dispatcher) AvailableChannels() []Channel {
	d.mu.RLock()
	defer d.mu.RUnlock()
	channels := make([]Channel, 0, len(d.senders))
	for ch := range d.senders {
		channels = append(channels, ch)
	}
	return channels
}

// isTransient returns true if the error is likely temporary and the operation
// should be retried. It checks for network timeouts, connection errors, and
// HTTP 5xx status codes.
func isTransient(err error) bool {
	if err == nil {
		return false
	}

	// Check for net.Error with Timeout().
	var netErr net.Error
	if ok := errorAs(err, &netErr); ok && netErr.Timeout() {
		return true
	}

	// Check for StatusError with 5xx.
	var statusErr *StatusError
	if ok := errorAs(err, &statusErr); ok {
		return statusErr.StatusCode >= 500
	}

	// Check error message for connection-level failures.
	msg := err.Error()
	if strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "connection reset") {
		return true
	}

	return false
}

// errorAs is a thin wrapper around errors.As to help with interface targets.
// Go's errors.As requires a pointer to the target type. This helper keeps the
// call sites readable.
func errorAs[T any](err error, target *T) bool {
	// Use interface-based unwrapping to walk the chain.
	for err != nil {
		if t, ok := any(err).(T); ok {
			*target = t
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// backoffDuration computes the delay for the given retry attempt using
// exponential backoff (base^(2*attempt)) with random jitter of 0-25%.
func backoffDuration(attempt int) time.Duration {
	// 1s, 4s, 16s for attempts 1, 2, 3
	secs := float64(baseBackoffSec)
	for i := 0; i < attempt; i++ {
		secs *= secs
		if i == 0 {
			secs = float64(baseBackoffSec)
			// attempt 1 => 1s, attempt 2 => 4s, attempt 3 => 16s
		}
	}
	// Simpler: use power of 4 progression.
	switch attempt {
	case 1:
		secs = 1
	case 2:
		secs = 4
	case 3:
		secs = 16
	default:
		secs = 16
	}

	jitter := secs * maxJitterPct * rand.Float64()
	return time.Duration((secs + jitter) * float64(time.Second))
}
