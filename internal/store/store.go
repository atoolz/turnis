package store

import (
	"database/sql"
	"embed"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/atoolz/turnis/internal/config"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type DB struct {
	conn   *sql.DB
	driver string
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

	return &DB{conn: conn, driver: driver}, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) Conn() *sql.DB {
	return db.conn
}

// parseMigrationVersion extracts the numeric version prefix from a migration
// filename like "001_initial.sql" and returns 1.
func parseMigrationVersion(filename string) (int, error) {
	base := filepath.Base(filename)
	parts := strings.SplitN(base, "_", 2)
	if len(parts) < 2 {
		return 0, fmt.Errorf("invalid migration filename: %s", filename)
	}
	v, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, fmt.Errorf("parsing version from %s: %w", filename, err)
	}
	return v, nil
}

type migration struct {
	Version  int
	Filename string
	SQL      string
}

// loadMigrations reads all embedded SQL migration files and returns them
// sorted by version number.
func loadMigrations() ([]migration, error) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("reading migrations dir: %w", err)
	}

	var migrations []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		v, err := parseMigrationVersion(e.Name())
		if err != nil {
			return nil, err
		}
		content, err := migrationFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("reading migration %s: %w", e.Name(), err)
		}
		migrations = append(migrations, migration{
			Version:  v,
			Filename: e.Name(),
			SQL:      string(content),
		})
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	return migrations, nil
}

// existingTablesPresent checks whether the schema already has application
// tables (created before the migration tracking system was introduced).
func (db *DB) existingTablesPresent() (bool, error) {
	var q string
	switch db.driver {
	case "postgres":
		q = `SELECT COUNT(*) FROM information_schema.tables WHERE table_name='teams'`
	default:
		q = `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='teams'`
	}
	var count int
	if err := db.conn.QueryRow(q).Scan(&count); err != nil {
		return false, fmt.Errorf("checking existing tables: %w", err)
	}
	return count > 0, nil
}

func (db *DB) Migrate() error {
	// Step 1: create the schema_migrations tracking table.
	_, err := db.conn.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    INTEGER PRIMARY KEY,
			applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		return fmt.Errorf("creating schema_migrations table: %w", err)
	}

	// Step 2: determine the highest applied version.
	var maxVersion int
	err = db.conn.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&maxVersion)
	if err != nil {
		return fmt.Errorf("querying max migration version: %w", err)
	}

	// Step 3: handle existing databases that predate the migration system.
	// If tables already exist but no migrations have been recorded, mark
	// migration 001 as applied (it created those tables originally).
	if maxVersion == 0 {
		hasExisting, err := db.existingTablesPresent()
		if err != nil {
			return err
		}
		if hasExisting {
			slog.Info("existing schema detected, recording migration 001 as already applied")
			_, err = db.conn.Exec(`INSERT INTO schema_migrations (version) VALUES (1)`)
			if err != nil {
				return fmt.Errorf("recording pre-existing migration 001: %w", err)
			}
			maxVersion = 1
		}
	}

	// Step 4: load all migration files.
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}

	// Step 5: apply unapplied migrations in order.
	for _, m := range migrations {
		if m.Version <= maxVersion {
			continue
		}

		slog.Info("applying migration", "version", m.Version, "file", m.Filename)

		tx, err := db.conn.Begin()
		if err != nil {
			return fmt.Errorf("starting transaction for migration %d: %w", m.Version, err)
		}
		if _, err := tx.Exec(m.SQL); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d (%s): %w", m.Version, m.Filename, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, m.Version); err != nil {
			tx.Rollback()
			return fmt.Errorf("recording migration %d: %w", m.Version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration %d: %w", m.Version, err)
		}
	}

	return nil
}
