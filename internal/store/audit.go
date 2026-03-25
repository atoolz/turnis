package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type AuditEntry struct {
	ID           string            `json:"id"`
	ActorUserID  string            `json:"actor_user_id,omitempty"`
	Action       string            `json:"action"`
	ResourceType string            `json:"resource_type"`
	ResourceID   string            `json:"resource_id,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
}

func (db *DB) RecordAudit(ctx context.Context, actorUserID, action, resourceType, resourceID string, metadata map[string]string) error {
	id := uuid.New().String()
	now := time.Now().UTC()

	var metadataJSON *string
	if len(metadata) > 0 {
		b, err := json.Marshal(metadata)
		if err != nil {
			return fmt.Errorf("marshaling audit metadata: %w", err)
		}
		s := string(b)
		metadataJSON = &s
	}

	var actorPtr *string
	if actorUserID != "" {
		actorPtr = &actorUserID
	}
	var resourcePtr *string
	if resourceID != "" {
		resourcePtr = &resourceID
	}

	_, err := db.conn.ExecContext(ctx,
		`INSERT INTO audit_log (id, actor_user_id, action, resource_type, resource_id, metadata_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, actorPtr, action, resourceType, resourcePtr, metadataJSON, now,
	)
	if err != nil {
		return fmt.Errorf("inserting audit entry: %w", err)
	}
	return nil
}

func (db *DB) ListAuditLog(ctx context.Context, resourceType, resourceID string, limit int) ([]AuditEntry, error) {
	query := `SELECT id, actor_user_id, action, resource_type, resource_id, metadata_json, created_at
	          FROM audit_log WHERE 1=1`
	var args []any

	if resourceType != "" {
		query += ` AND resource_type = ?`
		args = append(args, resourceType)
	}
	if resourceID != "" {
		query += ` AND resource_id = ?`
		args = append(args, resourceID)
	}

	query += ` ORDER BY created_at DESC`

	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	} else {
		query += ` LIMIT 100`
	}

	rows, err := db.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying audit log: %w", err)
	}
	defer rows.Close()

	var entries []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var actor, resID, metaJSON *string
		if err := rows.Scan(&e.ID, &actor, &e.Action, &e.ResourceType, &resID, &metaJSON, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning audit entry: %w", err)
		}
		if actor != nil {
			e.ActorUserID = *actor
		}
		if resID != nil {
			e.ResourceID = *resID
		}
		if metaJSON != nil {
			if err := json.Unmarshal([]byte(*metaJSON), &e.Metadata); err != nil {
				return nil, fmt.Errorf("unmarshaling audit metadata: %w", err)
			}
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
