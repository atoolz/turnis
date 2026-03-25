package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/atoolz/turnis/internal/escalation"
)

func (db *DB) CreatePolicy(ctx context.Context, p *escalation.Policy) (*escalation.Policy, error) {
	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	p.ID = uuid.New().String()
	now := time.Now().UTC()
	p.CreatedAt = now
	p.UpdatedAt = now

	if p.Repeat == 0 {
		p.Repeat = 1
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO escalation_policies (id, name, team_id, repeat, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.TeamID, p.Repeat, p.CreatedAt, p.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting escalation policy: %w", err)
	}

	for i := range p.Steps {
		step := &p.Steps[i]
		step.ID = uuid.New().String()
		step.PolicyID = p.ID

		if step.NotifyChannel == "" {
			step.NotifyChannel = "slack"
		}
		if step.TimeoutSeconds == 0 {
			step.TimeoutSeconds = 300
		}

		var scheduleID, userID *string
		if step.NotifyScheduleID != "" {
			scheduleID = &step.NotifyScheduleID
		}
		if step.NotifyUserID != "" {
			userID = &step.NotifyUserID
		}

		_, err = tx.ExecContext(ctx,
			`INSERT INTO escalation_steps (id, policy_id, step_order, timeout_seconds, notify_schedule_id, notify_user_id, notify_channel, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			step.ID, step.PolicyID, step.StepOrder, step.TimeoutSeconds, scheduleID, userID, step.NotifyChannel, now,
		)
		if err != nil {
			return nil, fmt.Errorf("inserting escalation step: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing transaction: %w", err)
	}

	return p, nil
}

func (db *DB) ListPolicies(ctx context.Context) ([]escalation.Policy, error) {
	rows, err := db.conn.QueryContext(ctx,
		`SELECT id, name, team_id, repeat, created_at, updated_at FROM escalation_policies ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("querying policies: %w", err)
	}
	defer rows.Close()

	var policies []escalation.Policy
	for rows.Next() {
		var p escalation.Policy
		if err := rows.Scan(&p.ID, &p.Name, &p.TeamID, &p.Repeat, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning policy: %w", err)
		}
		policies = append(policies, p)
	}
	return policies, rows.Err()
}

func (db *DB) GetPolicy(ctx context.Context, id string) (*escalation.Policy, error) {
	var p escalation.Policy
	err := db.conn.QueryRowContext(ctx,
		`SELECT id, name, team_id, repeat, created_at, updated_at FROM escalation_policies WHERE id = ?`,
		id,
	).Scan(&p.ID, &p.Name, &p.TeamID, &p.Repeat, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("getting policy %s: %w", id, err)
	}

	steps, err := db.getPolicySteps(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	p.Steps = steps

	return &p, nil
}

func (db *DB) getPolicySteps(ctx context.Context, policyID string) ([]escalation.Step, error) {
	rows, err := db.conn.QueryContext(ctx,
		`SELECT id, policy_id, step_order, timeout_seconds, notify_schedule_id, notify_user_id, notify_channel
		 FROM escalation_steps WHERE policy_id = ? ORDER BY step_order`,
		policyID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying steps: %w", err)
	}
	defer rows.Close()

	var steps []escalation.Step
	for rows.Next() {
		var s escalation.Step
		var scheduleID, userID *string
		if err := rows.Scan(&s.ID, &s.PolicyID, &s.StepOrder, &s.TimeoutSeconds, &scheduleID, &userID, &s.NotifyChannel); err != nil {
			return nil, fmt.Errorf("scanning step: %w", err)
		}
		if scheduleID != nil {
			s.NotifyScheduleID = *scheduleID
		}
		if userID != nil {
			s.NotifyUserID = *userID
		}
		steps = append(steps, s)
	}
	return steps, rows.Err()
}

func (db *DB) DeletePolicy(ctx context.Context, id string) error {
	res, err := db.conn.ExecContext(ctx, `DELETE FROM escalation_policies WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting policy %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("policy %s not found", id)
	}
	return nil
}
