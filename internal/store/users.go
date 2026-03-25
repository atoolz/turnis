package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Phone     string    `json:"phone,omitempty"`
	SlackID   string    `json:"slack_id,omitempty"`
	NtfyTopic string    `json:"ntfy_topic,omitempty"`
	TeamID    string    `json:"team_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (db *DB) CreateUser(ctx context.Context, name, email, phone, slackID, ntfyTopic, teamID string) (*User, error) {
	id := uuid.New().String()
	now := time.Now().UTC()

	var teamPtr *string
	if teamID != "" {
		teamPtr = &teamID
	}

	_, err := db.conn.ExecContext(ctx,
		`INSERT INTO users (id, name, email, phone, slack_id, ntfy_topic, team_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, name, email, phone, slackID, ntfyTopic, teamPtr, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting user: %w", err)
	}

	return &User{
		ID:        id,
		Name:      name,
		Email:     email,
		Phone:     phone,
		SlackID:   slackID,
		NtfyTopic: ntfyTopic,
		TeamID:    teamID,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (db *DB) ListUsers(ctx context.Context, teamID string) ([]User, error) {
	var query string
	var args []any

	if teamID != "" {
		query = `SELECT id, name, email, phone, slack_id, ntfy_topic, team_id, created_at, updated_at FROM users WHERE team_id = ? ORDER BY name`
		args = append(args, teamID)
	} else {
		query = `SELECT id, name, email, phone, slack_id, ntfy_topic, team_id, created_at, updated_at FROM users ORDER BY name`
	}

	rows, err := db.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying users: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		var phone, slackID, ntfyTopic, teamID *string
		if err := rows.Scan(&u.ID, &u.Name, &u.Email, &phone, &slackID, &ntfyTopic, &teamID, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning user: %w", err)
		}
		if phone != nil {
			u.Phone = *phone
		}
		if slackID != nil {
			u.SlackID = *slackID
		}
		if ntfyTopic != nil {
			u.NtfyTopic = *ntfyTopic
		}
		if teamID != nil {
			u.TeamID = *teamID
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (db *DB) GetUser(ctx context.Context, id string) (*User, error) {
	var u User
	var phone, slackID, ntfyTopic, teamID *string
	err := db.conn.QueryRowContext(ctx,
		`SELECT id, name, email, phone, slack_id, ntfy_topic, team_id, created_at, updated_at FROM users WHERE id = ?`,
		id,
	).Scan(&u.ID, &u.Name, &u.Email, &phone, &slackID, &ntfyTopic, &teamID, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("getting user %s: %w", id, err)
	}
	if phone != nil {
		u.Phone = *phone
	}
	if slackID != nil {
		u.SlackID = *slackID
	}
	if ntfyTopic != nil {
		u.NtfyTopic = *ntfyTopic
	}
	if teamID != nil {
		u.TeamID = *teamID
	}
	return &u, nil
}

func (db *DB) UpdateUser(ctx context.Context, id, name, email, phone, slackID, ntfyTopic, teamID string) (*User, error) {
	now := time.Now().UTC()

	var teamPtr *string
	if teamID != "" {
		teamPtr = &teamID
	}

	res, err := db.conn.ExecContext(ctx,
		`UPDATE users SET name = ?, email = ?, phone = ?, slack_id = ?, ntfy_topic = ?, team_id = ?, updated_at = ? WHERE id = ?`,
		name, email, phone, slackID, ntfyTopic, teamPtr, now, id,
	)
	if err != nil {
		return nil, fmt.Errorf("updating user %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("checking rows affected: %w", err)
	}
	if n == 0 {
		return nil, fmt.Errorf("user %s not found", id)
	}

	return db.GetUser(ctx, id)
}

func (db *DB) GetUserBySlackID(ctx context.Context, slackID string) (*User, error) {
	var u User
	var phone, sid, ntfyTopic, teamID *string
	err := db.conn.QueryRowContext(ctx,
		`SELECT id, name, email, phone, slack_id, ntfy_topic, team_id, created_at, updated_at FROM users WHERE slack_id = ?`,
		slackID,
	).Scan(&u.ID, &u.Name, &u.Email, &phone, &sid, &ntfyTopic, &teamID, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("getting user by slack_id %s: %w", slackID, err)
	}
	if phone != nil {
		u.Phone = *phone
	}
	if sid != nil {
		u.SlackID = *sid
	}
	if ntfyTopic != nil {
		u.NtfyTopic = *ntfyTopic
	}
	if teamID != nil {
		u.TeamID = *teamID
	}
	return &u, nil
}

func (db *DB) DeleteUser(ctx context.Context, id string) error {
	res, err := db.conn.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting user %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("user %s not found", id)
	}
	return nil
}
