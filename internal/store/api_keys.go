package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type APIKey struct {
	ID         string     `json:"id"`
	KeyHash    string     `json:"-"`
	Name       string     `json:"name"`
	TeamID     string     `json:"team_id,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

func (db *DB) CreateAPIKey(ctx context.Context, keyHash, name, teamID string) (*APIKey, error) {
	id := uuid.New().String()
	now := time.Now().UTC()

	var teamPtr *string
	if teamID != "" {
		teamPtr = &teamID
	}

	_, err := db.conn.ExecContext(ctx,
		`INSERT INTO api_keys (id, key_hash, name, team_id, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, keyHash, name, teamPtr, now,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting api key: %w", err)
	}

	return &APIKey{
		ID:        id,
		KeyHash:   keyHash,
		Name:      name,
		TeamID:    teamID,
		CreatedAt: now,
	}, nil
}

func (db *DB) GetAPIKeyByHash(ctx context.Context, hash string) (*APIKey, error) {
	var k APIKey
	var teamID *string
	var lastUsed *time.Time

	err := db.conn.QueryRowContext(ctx,
		`SELECT id, key_hash, name, team_id, created_at, last_used_at FROM api_keys WHERE key_hash = ?`,
		hash,
	).Scan(&k.ID, &k.KeyHash, &k.Name, &teamID, &k.CreatedAt, &lastUsed)
	if err != nil {
		return nil, fmt.Errorf("getting api key by hash: %w", err)
	}
	if teamID != nil {
		k.TeamID = *teamID
	}
	k.LastUsedAt = lastUsed
	return &k, nil
}

func (db *DB) UpdateAPIKeyLastUsed(ctx context.Context, id string) error {
	now := time.Now().UTC()
	_, err := db.conn.ExecContext(ctx,
		`UPDATE api_keys SET last_used_at = ? WHERE id = ?`,
		now, id,
	)
	if err != nil {
		return fmt.Errorf("updating api key last_used_at: %w", err)
	}
	return nil
}

func (db *DB) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	rows, err := db.conn.QueryContext(ctx,
		`SELECT id, name, team_id, created_at, last_used_at FROM api_keys ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("querying api keys: %w", err)
	}
	defer rows.Close()

	var keys []APIKey
	for rows.Next() {
		var k APIKey
		var teamID *string
		var lastUsed *time.Time
		if err := rows.Scan(&k.ID, &k.Name, &teamID, &k.CreatedAt, &lastUsed); err != nil {
			return nil, fmt.Errorf("scanning api key: %w", err)
		}
		if teamID != nil {
			k.TeamID = *teamID
		}
		k.LastUsedAt = lastUsed
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (db *DB) DeleteAPIKey(ctx context.Context, id string) error {
	res, err := db.conn.ExecContext(ctx, `DELETE FROM api_keys WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting api key %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("api key %s not found", id)
	}
	return nil
}
