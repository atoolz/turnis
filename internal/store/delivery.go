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
	RetryCount    int        `json:"retry_count"`
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

// RecentDeliveryAttempt extends DeliveryAttempt with the user name for display.
type RecentDeliveryAttempt struct {
	DeliveryAttempt
	UserName string `json:"user_name"`
}

func (db *DB) ListRecentDeliveries(ctx context.Context, limit int) ([]RecentDeliveryAttempt, error) {
	if limit <= 0 {
		limit = 5
	}

	rows, err := db.conn.QueryContext(ctx,
		`SELECT d.id, d.alert_id, d.user_id, d.channel, d.address, d.dispatched_at,
		        d.delivered_at, d.acked_at, d.failed_at, d.failure_reason, d.escalated_at,
		        d.retry_count,
		        COALESCE(u.name, '')
		 FROM delivery_attempts d
		 LEFT JOIN users u ON u.id = d.user_id
		 ORDER BY d.dispatched_at DESC
		 LIMIT ?`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("querying recent deliveries: %w", err)
	}
	defer rows.Close()

	var results []RecentDeliveryAttempt
	for rows.Next() {
		var d RecentDeliveryAttempt
		var failureReason *string
		if err := rows.Scan(
			&d.ID, &d.AlertID, &d.UserID, &d.Channel, &d.Address, &d.DispatchedAt,
			&d.DeliveredAt, &d.AckedAt, &d.FailedAt, &failureReason, &d.EscalatedAt,
			&d.RetryCount,
			&d.UserName,
		); err != nil {
			return nil, fmt.Errorf("scanning delivery attempt: %w", err)
		}
		if failureReason != nil {
			d.FailureReason = *failureReason
		}
		results = append(results, d)
	}
	return results, rows.Err()
}

func (db *DB) UpdateDeliveryRetry(ctx context.Context, id string, retryCount int, failureReason string) error {
	now := time.Now().UTC()
	_, err := db.conn.ExecContext(ctx,
		`UPDATE delivery_attempts SET retry_count = ?, failure_reason = ?, failed_at = ? WHERE id = ?`,
		retryCount, failureReason, now, id,
	)
	if err != nil {
		return fmt.Errorf("updating delivery retry for %s: %w", id, err)
	}
	return nil
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
