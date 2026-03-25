package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Team struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	SlackChannel string    `json:"slack_channel,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (db *DB) CreateTeam(ctx context.Context, name, slackChannel string) (*Team, error) {
	id := uuid.New().String()
	now := time.Now().UTC()

	_, err := db.conn.ExecContext(ctx,
		`INSERT INTO teams (id, name, slack_channel, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		id, name, slackChannel, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting team: %w", err)
	}

	return &Team{
		ID:           id,
		Name:         name,
		SlackChannel: slackChannel,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

func (db *DB) ListTeams(ctx context.Context) ([]Team, error) {
	rows, err := db.conn.QueryContext(ctx,
		`SELECT id, name, slack_channel, created_at, updated_at FROM teams ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("querying teams: %w", err)
	}
	defer rows.Close()

	var teams []Team
	for rows.Next() {
		var t Team
		var sc *string
		if err := rows.Scan(&t.ID, &t.Name, &sc, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning team: %w", err)
		}
		if sc != nil {
			t.SlackChannel = *sc
		}
		teams = append(teams, t)
	}
	return teams, rows.Err()
}

func (db *DB) GetTeam(ctx context.Context, id string) (*Team, error) {
	var t Team
	var sc *string
	err := db.conn.QueryRowContext(ctx,
		`SELECT id, name, slack_channel, created_at, updated_at FROM teams WHERE id = ?`,
		id,
	).Scan(&t.ID, &t.Name, &sc, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("getting team %s: %w", id, err)
	}
	if sc != nil {
		t.SlackChannel = *sc
	}
	return &t, nil
}

func (db *DB) DeleteTeam(ctx context.Context, id string) error {
	res, err := db.conn.ExecContext(ctx, `DELETE FROM teams WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting team %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("team %s not found", id)
	}
	return nil
}
