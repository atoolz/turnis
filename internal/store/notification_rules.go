package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type NotificationRule struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Channel   string    `json:"channel"`
	Priority  int       `json:"priority"`
	StartTime string    `json:"start_time,omitempty"`
	EndTime   string    `json:"end_time,omitempty"`
	Timezone  string    `json:"timezone"`
	CreatedAt time.Time `json:"created_at"`
}

func (db *DB) CreateNotificationRule(ctx context.Context, userID, channel string, priority int, startTime, endTime, timezone string) (*NotificationRule, error) {
	id := uuid.New().String()
	now := time.Now().UTC()

	if timezone == "" {
		timezone = "UTC"
	}

	var st, et *string
	if startTime != "" {
		st = &startTime
	}
	if endTime != "" {
		et = &endTime
	}

	_, err := db.conn.ExecContext(ctx,
		`INSERT INTO notification_rules (id, user_id, channel, priority, start_time, end_time, timezone, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, userID, channel, priority, st, et, timezone, now,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting notification rule: %w", err)
	}

	return &NotificationRule{
		ID:        id,
		UserID:    userID,
		Channel:   channel,
		Priority:  priority,
		StartTime: startTime,
		EndTime:   endTime,
		Timezone:  timezone,
		CreatedAt: now,
	}, nil
}

func (db *DB) ListNotificationRules(ctx context.Context, userID string) ([]NotificationRule, error) {
	rows, err := db.conn.QueryContext(ctx,
		`SELECT id, user_id, channel, priority, start_time, end_time, timezone, created_at
		 FROM notification_rules
		 WHERE user_id = ?
		 ORDER BY priority DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying notification rules: %w", err)
	}
	defer rows.Close()

	var rules []NotificationRule
	for rows.Next() {
		var r NotificationRule
		var startTime, endTime *string
		if err := rows.Scan(&r.ID, &r.UserID, &r.Channel, &r.Priority, &startTime, &endTime, &r.Timezone, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning notification rule: %w", err)
		}
		if startTime != nil {
			r.StartTime = *startTime
		}
		if endTime != nil {
			r.EndTime = *endTime
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

func (db *DB) DeleteNotificationRule(ctx context.Context, id string) error {
	res, err := db.conn.ExecContext(ctx, `DELETE FROM notification_rules WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting notification rule %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("notification rule %s not found", id)
	}
	return nil
}
