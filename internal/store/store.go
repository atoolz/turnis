package store

import (
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/atoolz/turnis/internal/config"
)

type DB struct {
	conn *sql.DB
}

func New(cfg config.DatabaseConfig) (*DB, error) {
	driver := cfg.Driver
	dsn := cfg.DSN

	if driver == "sqlite" {
		sep := "?"
		if strings.Contains(cfg.DSN, "?") {
			sep = "&"
		}
		dsn = cfg.DSN + sep + "_journal_mode=WAL&_busy_timeout=5000"
	}

	conn, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", driver, err)
	}

	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("pinging %s: %w", driver, err)
	}

	if driver == "sqlite" {
		conn.SetMaxOpenConns(1)
		// modernc.org/sqlite does not parse _foreign_keys from DSN params.
		// Enable foreign keys explicitly via PRAGMA.
		if _, err := conn.Exec("PRAGMA foreign_keys = ON"); err != nil {
			return nil, fmt.Errorf("enabling foreign keys: %w", err)
		}
	}

	return &DB{conn: conn}, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) Conn() *sql.DB {
	return db.conn
}

func (db *DB) Migrate() error {
	migrations := []string{
		migrationTeams,
		migrationUsers,
		migrationSchedules,
		migrationOverrides,
		migrationEscalationPolicies,
		migrationIntegrations,
		migrationAlerts,
		migrationDeliveryAttempts,
	}

	for i, m := range migrations {
		if _, err := db.conn.Exec(m); err != nil {
			return fmt.Errorf("migration %d: %w", i, err)
		}
	}

	return nil
}

var migrationTeams = `
CREATE TABLE IF NOT EXISTS teams (
	id         TEXT PRIMARY KEY,
	name       TEXT NOT NULL UNIQUE,
	slack_channel TEXT,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);`

var migrationUsers = `
CREATE TABLE IF NOT EXISTS users (
	id          TEXT PRIMARY KEY,
	name        TEXT NOT NULL,
	email       TEXT NOT NULL UNIQUE,
	phone       TEXT,
	slack_id    TEXT,
	ntfy_topic  TEXT,
	team_id     TEXT REFERENCES teams(id),
	created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);`

var migrationSchedules = `
CREATE TABLE IF NOT EXISTS schedules (
	id              TEXT PRIMARY KEY,
	name            TEXT NOT NULL,
	team_id         TEXT NOT NULL REFERENCES teams(id),
	timezone        TEXT NOT NULL DEFAULT 'UTC',
	rotation_type   TEXT NOT NULL DEFAULT 'weekly',
	rotation_length INTEGER NOT NULL DEFAULT 1,
	handoff_time    TEXT NOT NULL DEFAULT '09:00',
	handoff_day     TEXT NOT NULL DEFAULT 'monday',
	created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS schedule_layers (
	id          TEXT PRIMARY KEY,
	schedule_id TEXT NOT NULL REFERENCES schedules(id) ON DELETE CASCADE,
	priority    INTEGER NOT NULL DEFAULT 0,
	created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS schedule_participants (
	id       TEXT PRIMARY KEY,
	layer_id TEXT NOT NULL REFERENCES schedule_layers(id) ON DELETE CASCADE,
	user_id  TEXT NOT NULL REFERENCES users(id),
	position INTEGER NOT NULL DEFAULT 0
);`

var migrationOverrides = `
CREATE TABLE IF NOT EXISTS overrides (
	id          TEXT PRIMARY KEY,
	schedule_id TEXT NOT NULL REFERENCES schedules(id) ON DELETE CASCADE,
	user_id     TEXT NOT NULL REFERENCES users(id),
	start_time  DATETIME NOT NULL,
	end_time    DATETIME NOT NULL,
	reason      TEXT,
	created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);`

var migrationEscalationPolicies = `
CREATE TABLE IF NOT EXISTS escalation_policies (
	id         TEXT PRIMARY KEY,
	name       TEXT NOT NULL,
	team_id    TEXT NOT NULL REFERENCES teams(id),
	repeat     INTEGER NOT NULL DEFAULT 1,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS escalation_steps (
	id                   TEXT PRIMARY KEY,
	policy_id            TEXT NOT NULL REFERENCES escalation_policies(id) ON DELETE CASCADE,
	step_order           INTEGER NOT NULL,
	timeout_seconds      INTEGER NOT NULL DEFAULT 300,
	notify_schedule_id   TEXT REFERENCES schedules(id),
	notify_user_id       TEXT REFERENCES users(id),
	notify_channel       TEXT NOT NULL DEFAULT 'slack',
	created_at           DATETIME DEFAULT CURRENT_TIMESTAMP
);`

var migrationIntegrations = `
CREATE TABLE IF NOT EXISTS integrations (
	id                  TEXT PRIMARY KEY,
	name                TEXT NOT NULL,
	team_id             TEXT NOT NULL REFERENCES teams(id),
	type                TEXT NOT NULL DEFAULT 'webhook',
	escalation_policy_id TEXT REFERENCES escalation_policies(id),
	token               TEXT NOT NULL UNIQUE,
	created_at          DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at          DATETIME DEFAULT CURRENT_TIMESTAMP
);`

var migrationAlerts = `
CREATE TABLE IF NOT EXISTS alerts (
	id              TEXT PRIMARY KEY,
	integration_id  TEXT NOT NULL REFERENCES integrations(id),
	fingerprint     TEXT,
	status          TEXT NOT NULL DEFAULT 'firing',
	title           TEXT NOT NULL,
	message         TEXT,
	severity        TEXT NOT NULL DEFAULT 'warning',
	source          TEXT,
	acknowledged_by TEXT REFERENCES users(id),
	acknowledged_at DATETIME,
	resolved_at     DATETIME,
	created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_alerts_fingerprint ON alerts(fingerprint);
CREATE INDEX IF NOT EXISTS idx_alerts_status ON alerts(status);
CREATE INDEX IF NOT EXISTS idx_alerts_integration ON alerts(integration_id);`

var migrationDeliveryAttempts = `
CREATE TABLE IF NOT EXISTS delivery_attempts (
	id             TEXT PRIMARY KEY,
	alert_id       TEXT NOT NULL REFERENCES alerts(id),
	user_id        TEXT NOT NULL REFERENCES users(id),
	channel        TEXT NOT NULL,
	address        TEXT NOT NULL,
	dispatched_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	delivered_at   DATETIME,
	acked_at       DATETIME,
	failed_at      DATETIME,
	failure_reason TEXT,
	escalated_at   DATETIME
);

CREATE INDEX IF NOT EXISTS idx_delivery_alert ON delivery_attempts(alert_id);
CREATE INDEX IF NOT EXISTS idx_delivery_user ON delivery_attempts(user_id);`
