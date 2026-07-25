package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// TicketRecord is a row in the local history database.
type TicketRecord struct {
	ID          int64
	Title       string
	Description string
	JiraKey     string
	URL         string
	CreatedAt   time.Time
	TicketType  string // "Task", "Story", "Bug", "Epic", etc.
	ParentKey   string // non-empty when this is a child/subtask
	ParentURL   string
	ProjectKey  string
	Assignee    string
	Labels      []string
	Status      string   // current workflow status (e.g. "To Do", "In Progress", "Done")
}

// appDirOverride, when non-empty, replaces the default ~/.jira-tui path.
// Set this in tests to isolate data files (e.g. appDirOverride = t.TempDir()).
// In production, set the JIRA_TUI_DIR environment variable instead.
var appDirOverride string

func appDir() string {
	if appDirOverride != "" {
		return appDirOverride
	}
	if dir := os.Getenv("JIRA_TUI_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".jira-tui")
}

func ensureAppDir() error {
	return os.MkdirAll(appDir(), 0700)
}

// isFirstRun reports whether the app data directory does not yet exist.
// Returns false when either JIRA_TUI_DIR or the internal test override is set,
// so the prompt is never shown during tests or when the user has explicitly
// configured a non-default location.
func isFirstRun() bool {
	if appDirOverride != "" || os.Getenv("JIRA_TUI_DIR") != "" {
		return false
	}
	_, err := os.Stat(appDir())
	return os.IsNotExist(err)
}

func dbPath() string {
	return filepath.Join(appDir(), "history.db")
}

func openDB() (*sql.DB, error) {
	if err := ensureAppDir(); err != nil {
		return nil, fmt.Errorf("create app dir: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath())
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	// Single connection avoids SQLITE_BUSY races; 5 s timeout for any remaining contention.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		db.Close()
		return nil, fmt.Errorf("set pragma busy_timeout: %w", err)
	}
	if err := migrateDB(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("init db: %w", err)
	}
	return db, nil
}

// currentSchemaVersion is the schema version this build expects.
// Bump this and add a migration step in migrateDB whenever the schema changes.
const currentSchemaVersion = 2

func migrateDB(db *sql.DB) error {
	// Schema-version table — created unconditionally; safe on first open.
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (
		version INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("create schema_version table: %w", err)
	}

	// Read current version (0 if table is empty).
	var version int
	row := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`)
	if err := row.Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	// Run migrations in order; each step is idempotent.
	if version < 1 {
		if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS tickets (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			title       TEXT    NOT NULL,
			description TEXT    NOT NULL DEFAULT '',
			jira_key    TEXT    NOT NULL,
			url         TEXT    NOT NULL,
			created_at  TEXT    NOT NULL,
			ticket_type TEXT    NOT NULL DEFAULT 'Task',
			parent_key  TEXT    NOT NULL DEFAULT '',
			parent_url  TEXT    NOT NULL DEFAULT '',
			project_key TEXT    NOT NULL DEFAULT '',
			assignee    TEXT    NOT NULL DEFAULT '',
			labels      TEXT    NOT NULL DEFAULT '[]'
		)`); err != nil {
			return fmt.Errorf("migration 1 — create tickets table: %w", err)
		}
	}

	if version < 2 {
		if _, err := db.Exec(`ALTER TABLE tickets ADD COLUMN status TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("migration 2 — add status column: %w", err)
		}
	}

	// Write the current version (no-op if already up to date).
	if version < currentSchemaVersion {
		if _, err := db.Exec(`DELETE FROM schema_version`); err != nil {
			return fmt.Errorf("update schema version: %w", err)
		}
		if _, err := db.Exec(`INSERT INTO schema_version (version) VALUES (?)`, currentSchemaVersion); err != nil {
			return fmt.Errorf("update schema version: %w", err)
		}
	}

	return nil
}

func insertTicket(db *sql.DB, r TicketRecord) error {
	// Normalise nil to empty so JSON roundtrip is stable:
	// nil → "null" → nil  vs  []string{} → "[]" → []string{}
	if r.Labels == nil {
		r.Labels = []string{}
	}
	labels, err := json.Marshal(r.Labels)
	if err != nil {
		labels = []byte("[]")
	}
	_, err = db.Exec(
		`INSERT INTO tickets
		 (title, description, jira_key, url, created_at, ticket_type, parent_key, parent_url, project_key, assignee, labels, status)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.Title, r.Description, r.JiraKey, r.URL,
		r.CreatedAt.UTC().Format(time.RFC3339),
		r.TicketType, r.ParentKey, r.ParentURL,
		r.ProjectKey, r.Assignee, string(labels), r.Status,
	)
	return err
}

const selectCols = `id, title, description, jira_key, url, created_at, ticket_type, parent_key, parent_url, project_key, assignee, labels, status`

func scanTickets(rows *sql.Rows) ([]TicketRecord, error) {
	defer rows.Close()
	var records []TicketRecord
	for rows.Next() {
		var r TicketRecord
		var labelsJSON, createdAt string
		if err := rows.Scan(
			&r.ID, &r.Title, &r.Description, &r.JiraKey, &r.URL, &createdAt,
			&r.TicketType, &r.ParentKey, &r.ParentURL, &r.ProjectKey, &r.Assignee, &labelsJSON, &r.Status,
		); err != nil {
			return nil, err
		}
		if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
			r.CreatedAt = t
		}
		if err := json.Unmarshal([]byte(labelsJSON), &r.Labels); err != nil || r.Labels == nil {
			r.Labels = []string{}
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

// findDuplicates returns history records whose title and description both match exactly.
func findDuplicates(db *sql.DB, title, description string) ([]TicketRecord, error) {
	rows, err := db.Query(
		`SELECT `+selectCols+` FROM tickets WHERE title = ? AND description = ? ORDER BY created_at DESC`,
		title, description,
	)
	if err != nil {
		return nil, err
	}
	return scanTickets(rows)
}

// findEpicsByTitle returns history records that are Epics with the given title.
func findEpicsByTitle(db *sql.DB, title string) ([]TicketRecord, error) {
	rows, err := db.Query(
		`SELECT `+selectCols+` FROM tickets WHERE title = ? AND ticket_type = 'Epic' ORDER BY created_at DESC`,
		title,
	)
	if err != nil {
		return nil, err
	}
	return scanTickets(rows)
}

// deleteTicket removes a single history record by ID.
func deleteTicket(db *sql.DB, id int64) error {
	_, err := db.Exec(`DELETE FROM tickets WHERE id = ?`, id)
	return err
}

// updateTicketStatus sets the status column for a single history record.
func updateTicketStatus(db *sql.DB, id int64, status string) error {
	_, err := db.Exec(`UPDATE tickets SET status = ? WHERE id = ?`, status, id)
	return err
}

// allTickets returns all history records, newest first.
func allTickets(db *sql.DB) ([]TicketRecord, error) {
	rows, err := db.Query(
		`SELECT ` + selectCols + ` FROM tickets ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	return scanTickets(rows)
}
