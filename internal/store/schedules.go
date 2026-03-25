package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/atoolz/turnis/internal/schedule"
)

func (db *DB) CreateSchedule(ctx context.Context, s *schedule.Schedule) (*schedule.Schedule, error) {
	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	s.ID = uuid.New().String()
	now := time.Now().UTC()
	s.CreatedAt = now
	s.UpdatedAt = now

	if s.Timezone == "" {
		s.Timezone = "UTC"
	}
	if s.RotationType == "" {
		s.RotationType = schedule.RotationWeekly
	}
	if s.RotationLength == 0 {
		s.RotationLength = 1
	}
	if s.HandoffTime == "" {
		s.HandoffTime = "09:00"
	}
	if s.HandoffDay == "" {
		s.HandoffDay = "monday"
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO schedules (id, name, team_id, timezone, rotation_type, rotation_length, handoff_time, handoff_day, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.Name, s.TeamID, s.Timezone, s.RotationType, s.RotationLength, s.HandoffTime, s.HandoffDay, s.CreatedAt, s.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting schedule: %w", err)
	}

	for i := range s.Layers {
		layer := &s.Layers[i]
		layer.ID = uuid.New().String()
		layer.ScheduleID = s.ID

		_, err = tx.ExecContext(ctx,
			`INSERT INTO schedule_layers (id, schedule_id, priority, created_at) VALUES (?, ?, ?, ?)`,
			layer.ID, layer.ScheduleID, layer.Priority, now,
		)
		if err != nil {
			return nil, fmt.Errorf("inserting layer: %w", err)
		}

		for j := range layer.Participants {
			p := &layer.Participants[j]
			p.ID = uuid.New().String()
			p.LayerID = layer.ID

			_, err = tx.ExecContext(ctx,
				`INSERT INTO schedule_participants (id, layer_id, user_id, position) VALUES (?, ?, ?, ?)`,
				p.ID, p.LayerID, p.UserID, p.Position,
			)
			if err != nil {
				return nil, fmt.Errorf("inserting participant: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing transaction: %w", err)
	}

	return s, nil
}

func (db *DB) ListSchedules(ctx context.Context) ([]schedule.Schedule, error) {
	rows, err := db.conn.QueryContext(ctx,
		`SELECT id, name, team_id, timezone, rotation_type, rotation_length, handoff_time, handoff_day, created_at, updated_at
		 FROM schedules ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("querying schedules: %w", err)
	}
	defer rows.Close()

	var schedules []schedule.Schedule
	for rows.Next() {
		var s schedule.Schedule
		if err := rows.Scan(&s.ID, &s.Name, &s.TeamID, &s.Timezone, &s.RotationType, &s.RotationLength, &s.HandoffTime, &s.HandoffDay, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning schedule: %w", err)
		}
		schedules = append(schedules, s)
	}
	return schedules, rows.Err()
}

func (db *DB) GetSchedule(ctx context.Context, id string) (*schedule.Schedule, error) {
	var s schedule.Schedule
	err := db.conn.QueryRowContext(ctx,
		`SELECT id, name, team_id, timezone, rotation_type, rotation_length, handoff_time, handoff_day, created_at, updated_at
		 FROM schedules WHERE id = ?`,
		id,
	).Scan(&s.ID, &s.Name, &s.TeamID, &s.Timezone, &s.RotationType, &s.RotationLength, &s.HandoffTime, &s.HandoffDay, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("getting schedule %s: %w", id, err)
	}

	layers, err := db.getScheduleLayers(ctx, s.ID)
	if err != nil {
		return nil, err
	}
	s.Layers = layers

	return &s, nil
}

func (db *DB) getScheduleLayers(ctx context.Context, scheduleID string) ([]schedule.Layer, error) {
	rows, err := db.conn.QueryContext(ctx,
		`SELECT id, schedule_id, priority FROM schedule_layers WHERE schedule_id = ? ORDER BY priority`,
		scheduleID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying layers: %w", err)
	}

	var layers []schedule.Layer
	for rows.Next() {
		var l schedule.Layer
		if err := rows.Scan(&l.ID, &l.ScheduleID, &l.Priority); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scanning layer: %w", err)
		}
		layers = append(layers, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating layers: %w", err)
	}

	for i := range layers {
		participants, err := db.getLayerParticipants(ctx, layers[i].ID)
		if err != nil {
			return nil, err
		}
		layers[i].Participants = participants
	}

	return layers, nil
}

func (db *DB) getLayerParticipants(ctx context.Context, layerID string) ([]schedule.Participant, error) {
	rows, err := db.conn.QueryContext(ctx,
		`SELECT id, layer_id, user_id, position FROM schedule_participants WHERE layer_id = ? ORDER BY position`,
		layerID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying participants: %w", err)
	}
	defer rows.Close()

	var participants []schedule.Participant
	for rows.Next() {
		var p schedule.Participant
		if err := rows.Scan(&p.ID, &p.LayerID, &p.UserID, &p.Position); err != nil {
			return nil, fmt.Errorf("scanning participant: %w", err)
		}
		participants = append(participants, p)
	}
	return participants, rows.Err()
}

func (db *DB) GetSchedulesByTeam(ctx context.Context, teamID string) ([]schedule.Schedule, error) {
	rows, err := db.conn.QueryContext(ctx,
		`SELECT id, name, team_id, timezone, rotation_type, rotation_length, handoff_time, handoff_day, created_at, updated_at
		 FROM schedules WHERE team_id = ? ORDER BY name`,
		teamID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying schedules by team: %w", err)
	}

	var schedules []schedule.Schedule
	for rows.Next() {
		var s schedule.Schedule
		if err := rows.Scan(&s.ID, &s.Name, &s.TeamID, &s.Timezone, &s.RotationType, &s.RotationLength, &s.HandoffTime, &s.HandoffDay, &s.CreatedAt, &s.UpdatedAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scanning schedule: %w", err)
		}
		schedules = append(schedules, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating schedules: %w", err)
	}

	for i := range schedules {
		layers, err := db.getScheduleLayers(ctx, schedules[i].ID)
		if err != nil {
			return nil, err
		}
		schedules[i].Layers = layers
	}

	return schedules, nil
}

func (db *DB) GetOverrides(ctx context.Context, scheduleID string) ([]schedule.Override, error) {
	rows, err := db.conn.QueryContext(ctx,
		`SELECT id, schedule_id, user_id, start_time, end_time, reason, created_at
		 FROM overrides WHERE schedule_id = ? AND end_time > datetime('now') ORDER BY start_time`,
		scheduleID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying overrides: %w", err)
	}
	defer rows.Close()

	var overrides []schedule.Override
	for rows.Next() {
		var o schedule.Override
		var reason *string
		if err := rows.Scan(&o.ID, &o.ScheduleID, &o.UserID, &o.StartTime, &o.EndTime, &reason, &o.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning override: %w", err)
		}
		if reason != nil {
			o.Reason = *reason
		}
		overrides = append(overrides, o)
	}
	return overrides, rows.Err()
}

func (db *DB) CreateOverride(ctx context.Context, scheduleID, userID string, startTime, endTime time.Time, reason string) (*schedule.Override, error) {
	id := uuid.New().String()
	now := time.Now().UTC()

	_, err := db.conn.ExecContext(ctx,
		`INSERT INTO overrides (id, schedule_id, user_id, start_time, end_time, reason, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, scheduleID, userID, startTime, endTime, reason, now,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting override: %w", err)
	}

	return &schedule.Override{
		ID:         id,
		ScheduleID: scheduleID,
		UserID:     userID,
		StartTime:  startTime,
		EndTime:    endTime,
		Reason:     reason,
		CreatedAt:  now,
	}, nil
}
