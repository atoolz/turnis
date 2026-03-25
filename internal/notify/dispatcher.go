package notify

import (
	"context"
	"fmt"
	"log/slog"
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
}

type Sender interface {
	Send(ctx context.Context, msg Message) (*DeliveryResult, error)
	Name() Channel
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

func (d *Dispatcher) AvailableChannels() []Channel {
	d.mu.RLock()
	defer d.mu.RUnlock()
	channels := make([]Channel, 0, len(d.senders))
	for ch := range d.senders {
		channels = append(channels, ch)
	}
	return channels
}
