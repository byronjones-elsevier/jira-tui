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
}

func appDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".jira-tui")
}

func ensureAppDir() error {
	return os.MkdirAll(appDir(), 0700)
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
	if err := migrateDB(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("init db: %w", err)
	}
	return db, nil
}

func migrateDB(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS tickets (
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
	)`)
	return err
}

func insertTicket(db *sql.DB, r TicketRecord) error {
	labels, err := json.Marshal(r.Labels)
	if err != nil || labels == nil {
		labels = []byte("[]")
	}
	_, err = db.Exec(
		`INSERT INTO tickets
		 (title, description, jira_key, url, created_at, ticket_type, parent_key, parent_url, project_key, assignee, labels)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.Title, r.Description, r.JiraKey, r.URL,
		r.CreatedAt.UTC().Format(time.RFC3339),
		r.TicketType, r.ParentKey, r.ParentURL,
		r.ProjectKey, r.Assignee, string(labels),
	)
	return err
}

const selectCols = `id, title, description, jira_key, url, created_at, ticket_type, parent_key, parent_url, project_key, assignee, labels`

func scanTickets(rows *sql.Rows) ([]TicketRecord, error) {
	defer rows.Close()
	var records []TicketRecord
	for rows.Next() {
		var r TicketRecord
		var labelsJSON, createdAt string
		if err := rows.Scan(
			&r.ID, &r.Title, &r.Description, &r.JiraKey, &r.URL, &createdAt,
			&r.TicketType, &r.ParentKey, &r.ParentURL, &r.ProjectKey, &r.Assignee, &labelsJSON,
		); err != nil {
			return nil, err
		}
		r.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		_ = json.Unmarshal([]byte(labelsJSON), &r.Labels)
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
