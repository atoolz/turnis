package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Integration struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	TeamID             string    `json:"team_id"`
	Type               string    `json:"type"`
	EscalationPolicyID string    `json:"escalation_policy_id,omitempty"`
	Token              string    `json:"token"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating random token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func (db *DB) CreateIntegration(ctx context.Context, name, teamID, integrationType, escalationPolicyID string) (*Integration, error) {
	id := uuid.New().String()
	now := time.Now().UTC()

	token, err := generateToken()
	if err != nil {
		return nil, err
	}

	if integrationType == "" {
		integrationType = "webhook"
	}

	var epPtr *string
	if escalationPolicyID != "" {
		epPtr = &escalationPolicyID
	}

	_, err = db.conn.ExecContext(ctx,
		`INSERT INTO integrations (id, name, team_id, type, escalation_policy_id, token, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, name, teamID, integrationType, epPtr, token, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting integration: %w", err)
	}

	return &Integration{
		ID:                 id,
		Name:               name,
		TeamID:             teamID,
		Type:               integrationType,
		EscalationPolicyID: escalationPolicyID,
		Token:              token,
		CreatedAt:          now,
		UpdatedAt:          now,
	}, nil
}

func (db *DB) ListIntegrations(ctx context.Context) ([]Integration, error) {
	rows, err := db.conn.QueryContext(ctx,
		`SELECT id, name, team_id, type, escalation_policy_id, token, created_at, updated_at FROM integrations ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("querying integrations: %w", err)
	}
	defer rows.Close()

	var integrations []Integration
	for rows.Next() {
		var i Integration
		var epID *string
		if err := rows.Scan(&i.ID, &i.Name, &i.TeamID, &i.Type, &epID, &i.Token, &i.CreatedAt, &i.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning integration: %w", err)
		}
		if epID != nil {
			i.EscalationPolicyID = *epID
		}
		integrations = append(integrations, i)
	}
	return integrations, rows.Err()
}

func (db *DB) GetIntegration(ctx context.Context, id string) (*Integration, error) {
	var i Integration
	var epID *string
	err := db.conn.QueryRowContext(ctx,
		`SELECT id, name, team_id, type, escalation_policy_id, token, created_at, updated_at FROM integrations WHERE id = ?`,
		id,
	).Scan(&i.ID, &i.Name, &i.TeamID, &i.Type, &epID, &i.Token, &i.CreatedAt, &i.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if epID != nil {
		i.EscalationPolicyID = *epID
	}
	return &i, nil
}

func (db *DB) GetIntegrationByToken(ctx context.Context, token string) (*Integration, error) {
	var i Integration
	var epID *string
	err := db.conn.QueryRowContext(ctx,
		`SELECT id, name, team_id, type, escalation_policy_id, token, created_at, updated_at FROM integrations WHERE token = ?`,
		token,
	).Scan(&i.ID, &i.Name, &i.TeamID, &i.Type, &epID, &i.Token, &i.CreatedAt, &i.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("getting integration by token: %w", err)
	}
	if epID != nil {
		i.EscalationPolicyID = *epID
	}
	return &i, nil
}

func (db *DB) DeleteIntegration(ctx context.Context, id string) error {
	res, err := db.conn.ExecContext(ctx, `DELETE FROM integrations WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting integration %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("integration %s not found", id)
	}
	return nil
}
