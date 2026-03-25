package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/atoolz/turnis/internal/notify"
)

type Sender struct {
	client *http.Client
}

func New() *Sender {
	return &Sender{
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *Sender) Name() notify.Channel {
	return notify.ChannelWebhook
}

func (s *Sender) Send(ctx context.Context, msg notify.Message) (*notify.DeliveryResult, error) {
	payload := map[string]any{
		"event":       "alert.notification",
		"alert_id":    msg.AlertID,
		"title":       msg.Title,
		"body":        msg.Body,
		"severity":    msg.Severity,
		"ack_url":     msg.AckURL,
		"resolve_url": msg.ResolveURL,
		"user_id":     msg.UserID,
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshaling webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, msg.Address, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating webhook request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Turnis/1.0")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending webhook: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return &notify.DeliveryResult{
		Channel: notify.ChannelWebhook,
		Success: true,
		SentAt:  time.Now(),
	}, nil
}
