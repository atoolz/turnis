package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type DeliveryAttempt struct {
	ID            string     `json:"id"`
	AlertID       string     `json:"alert_id"`
	UserID        string     `json:"user_id"`
	Channel       string     `json:"channel"`
	Address       string     `json:"address"`
	DispatchedAt  time.Time  `json:"dispatched_at"`
	DeliveredAt   *time.Time `json:"delivered_at,omitempty"`
	AckedAt       *time.Time `json:"acked_at,omitempty"`
	FailedAt      *time.Time `json:"failed_at,omitempty"`
	FailureReason string     `json:"failure_reason,omitempty"`
	EscalatedAt   *time.Time `json:"escalated_at,omitempty"`
}

func (db *DB) RecordDelivery(ctx context.Context, alertID, userID, channel, address string, success bool, failureReason string) (*DeliveryAttempt, error) {
	id := uuid.New().String()
	now := time.Now().UTC()

	d := &DeliveryAttempt{
		ID:           id,
		AlertID:      alertID,
		UserID:       userID,
		Channel:      channel,
		Address:      address,
		DispatchedAt: now,
	}

	if success {
		d.DeliveredAt = &now
	} else {
		d.FailedAt = &now
		d.FailureReason = failureReason
	}

	_, err := db.conn.ExecContext(ctx,
		`INSERT INTO delivery_attempts (id, alert_id, user_id, channel, address, dispatched_at, delivered_at, failed_at, failure_reason)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.AlertID, d.UserID, d.Channel, d.Address, d.DispatchedAt, d.DeliveredAt, d.FailedAt, d.FailureReason,
	)
	if err != nil {
		return nil, fmt.Errorf("recording delivery attempt: %w", err)
	}

	return d, nil
}

func (db *DB) MarkDeliveryEscalated(ctx context.Context, alertID string) error {
	now := time.Now().UTC()
	_, err := db.conn.ExecContext(ctx,
		`UPDATE delivery_attempts SET escalated_at = ? WHERE alert_id = ? AND escalated_at IS NULL`,
		now, alertID,
	)
	if err != nil {
		return fmt.Errorf("marking delivery escalated for alert %s: %w", alertID, err)
	}
	return nil
}
