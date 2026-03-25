package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/atoolz/turnis/internal/alert"
)

func (db *DB) CreateAlert(ctx context.Context, integrationID string, incoming alert.IncomingAlert) (*alert.Alert, error) {
	id := uuid.New().String()
	now := time.Now().UTC()

	severity := incoming.Severity
	if severity == "" {
		severity = alert.SeverityWarning
	}

	_, err := db.conn.ExecContext(ctx,
		`INSERT INTO alerts (id, integration_id, fingerprint, status, title, message, severity, source, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, integrationID, incoming.Fingerprint, alert.StatusFiring, incoming.Title, incoming.Message, severity, incoming.Source, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting alert: %w", err)
	}

	return &alert.Alert{
		ID:            id,
		IntegrationID: integrationID,
		Fingerprint:   incoming.Fingerprint,
		Status:        alert.StatusFiring,
		Title:         incoming.Title,
		Message:       incoming.Message,
		Severity:      severity,
		Source:        incoming.Source,
		CreatedAt:     now,
		UpdatedAt:     now,
	}, nil
}

func (db *DB) ListAlerts(ctx context.Context, status, integrationID string) ([]alert.Alert, error) {
	query := `SELECT id, integration_id, fingerprint, status, title, message, severity, source,
	           acknowledged_by, acknowledged_at, resolved_at, created_at, updated_at
	          FROM alerts WHERE 1=1`
	var args []any

	if status != "" {
		query += ` AND status = ?`
		args = append(args, status)
	}
	if integrationID != "" {
		query += ` AND integration_id = ?`
		args = append(args, integrationID)
	}

	query += ` ORDER BY created_at DESC`

	rows, err := db.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying alerts: %w", err)
	}
	defer rows.Close()

	var alerts []alert.Alert
	for rows.Next() {
		var a alert.Alert
		var fp, msg, source, ackedBy *string
		var ackedAt, resolvedAt *time.Time
		if err := rows.Scan(&a.ID, &a.IntegrationID, &fp, &a.Status, &a.Title, &msg, &a.Severity, &source, &ackedBy, &ackedAt, &resolvedAt, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning alert: %w", err)
		}
		if fp != nil {
			a.Fingerprint = *fp
		}
		if msg != nil {
			a.Message = *msg
		}
		if source != nil {
			a.Source = *source
		}
		if ackedBy != nil {
			a.AcknowledgedBy = *ackedBy
		}
		a.AcknowledgedAt = ackedAt
		a.ResolvedAt = resolvedAt
		alerts = append(alerts, a)
	}
	return alerts, rows.Err()
}

func (db *DB) GetAlert(ctx context.Context, id string) (*alert.Alert, error) {
	var a alert.Alert
	var fp, msg, source, ackedBy *string
	var ackedAt, resolvedAt *time.Time
	err := db.conn.QueryRowContext(ctx,
		`SELECT id, integration_id, fingerprint, status, title, message, severity, source,
		        acknowledged_by, acknowledged_at, resolved_at, created_at, updated_at
		 FROM alerts WHERE id = ?`,
		id,
	).Scan(&a.ID, &a.IntegrationID, &fp, &a.Status, &a.Title, &msg, &a.Severity, &source, &ackedBy, &ackedAt, &resolvedAt, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("getting alert %s: %w", id, err)
	}
	if fp != nil {
		a.Fingerprint = *fp
	}
	if msg != nil {
		a.Message = *msg
	}
	if source != nil {
		a.Source = *source
	}
	if ackedBy != nil {
		a.AcknowledgedBy = *ackedBy
	}
	a.AcknowledgedAt = ackedAt
	a.ResolvedAt = resolvedAt
	return &a, nil
}

func (db *DB) AcknowledgeAlert(ctx context.Context, id, userID string) (*alert.Alert, error) {
	now := time.Now().UTC()
	res, err := db.conn.ExecContext(ctx,
		`UPDATE alerts SET status = ?, acknowledged_by = ?, acknowledged_at = ?, updated_at = ? WHERE id = ? AND status = ?`,
		alert.StatusAcknowledged, userID, now, now, id, alert.StatusFiring,
	)
	if err != nil {
		return nil, fmt.Errorf("acknowledging alert %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("checking rows affected: %w", err)
	}
	if n == 0 {
		return nil, fmt.Errorf("alert %s not found or not in firing status", id)
	}
	return db.GetAlert(ctx, id)
}

func (db *DB) ResolveAlert(ctx context.Context, id string) (*alert.Alert, error) {
	now := time.Now().UTC()
	res, err := db.conn.ExecContext(ctx,
		`UPDATE alerts SET status = ?, resolved_at = ?, updated_at = ? WHERE id = ? AND status != ?`,
		alert.StatusResolved, now, now, id, alert.StatusResolved,
	)
	if err != nil {
		return nil, fmt.Errorf("resolving alert %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("checking rows affected: %w", err)
	}
	if n == 0 {
		return nil, fmt.Errorf("alert %s not found or already resolved", id)
	}
	return db.GetAlert(ctx, id)
}

func (db *DB) GetAlertsByFingerprint(ctx context.Context, integrationID, fingerprint string) ([]alert.Alert, error) {
	rows, err := db.conn.QueryContext(ctx,
		`SELECT id, integration_id, fingerprint, status, title, message, severity, source,
		        acknowledged_by, acknowledged_at, resolved_at, created_at, updated_at
		 FROM alerts WHERE integration_id = ? AND fingerprint = ? AND status != ?`,
		integrationID, fingerprint, alert.StatusResolved,
	)
	if err != nil {
		return nil, fmt.Errorf("querying alerts by fingerprint: %w", err)
	}
	defer rows.Close()

	var alerts []alert.Alert
	for rows.Next() {
		var a alert.Alert
		var fp, msg, source, ackedBy *string
		var ackedAt, resolvedAt *time.Time
		if err := rows.Scan(&a.ID, &a.IntegrationID, &fp, &a.Status, &a.Title, &msg, &a.Severity, &source, &ackedBy, &ackedAt, &resolvedAt, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning alert: %w", err)
		}
		if fp != nil {
			a.Fingerprint = *fp
		}
		if msg != nil {
			a.Message = *msg
		}
		if source != nil {
			a.Source = *source
		}
		if ackedBy != nil {
			a.AcknowledgedBy = *ackedBy
		}
		a.AcknowledgedAt = ackedAt
		a.ResolvedAt = resolvedAt
		alerts = append(alerts, a)
	}
	return alerts, rows.Err()
}
