package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withTempDB opens a fresh DB in a temp directory and returns it.
// The appDirOverride is restored after the test completes.
func withTempDB(t *testing.T) interface{ Close() error } {
	t.Helper()
	appDirOverride = t.TempDir()
	t.Cleanup(func() { appDirOverride = "" })
	db, err := openDB()
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

func sampleRecord(overrides ...func(*TicketRecord)) TicketRecord {
	r := TicketRecord{
		Title:       "Fix Login Bug",
		Description: "Users cannot log in via SSO",
		JiraKey:     "PROJ-1",
		URL:         "https://example.atlassian.net/browse/PROJ-1",
		CreatedAt:   time.Date(2025, 7, 1, 12, 0, 0, 0, time.UTC),
		TicketType:  "Task",
		ProjectKey:  "PROJ",
		Labels:      []string{"bug", "auth"},
	}
	for _, fn := range overrides {
		fn(&r)
	}
	return r
}

func TestOpenDB_CreatesSchema(t *testing.T) {
	appDirOverride = t.TempDir()
	t.Cleanup(func() { appDirOverride = "" })

	db, err := openDB()
	require.NoError(t, err)
	defer db.Close()

	// Verify the tickets table exists by querying it.
	_, err = db.Exec("SELECT id FROM tickets LIMIT 1")
	assert.NoError(t, err)
}

func TestInsertAndAllTickets_Roundtrip(t *testing.T) {
	appDirOverride = t.TempDir()
	t.Cleanup(func() { appDirOverride = "" })

	db, err := openDB()
	require.NoError(t, err)
	defer db.Close()

	r := sampleRecord()
	require.NoError(t, insertTicket(db, r))

	records, err := allTickets(db)
	require.NoError(t, err)
	require.Len(t, records, 1)

	got := records[0]
	assert.Equal(t, r.Title, got.Title)
	assert.Equal(t, r.Description, got.Description)
	assert.Equal(t, r.JiraKey, got.JiraKey)
	assert.Equal(t, r.URL, got.URL)
	assert.Equal(t, r.TicketType, got.TicketType)
	assert.Equal(t, r.ProjectKey, got.ProjectKey)
	assert.Equal(t, r.Labels, got.Labels)
	// CreatedAt is stored as RFC3339 (second precision).
	assert.Equal(t, r.CreatedAt.Truncate(time.Second), got.CreatedAt)
}

func TestAllTickets_OrderedNewestFirst(t *testing.T) {
	appDirOverride = t.TempDir()
	t.Cleanup(func() { appDirOverride = "" })

	db, err := openDB()
	require.NoError(t, err)
	defer db.Close()

	older := sampleRecord(func(r *TicketRecord) {
		r.JiraKey = "PROJ-1"
		r.CreatedAt = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	})
	newer := sampleRecord(func(r *TicketRecord) {
		r.JiraKey = "PROJ-2"
		r.CreatedAt = time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	})
	require.NoError(t, insertTicket(db, older))
	require.NoError(t, insertTicket(db, newer))

	records, err := allTickets(db)
	require.NoError(t, err)
	require.Len(t, records, 2)
	assert.Equal(t, "PROJ-2", records[0].JiraKey, "newest should come first")
	assert.Equal(t, "PROJ-1", records[1].JiraKey)
}

func TestFindDuplicates_ExactMatch(t *testing.T) {
	appDirOverride = t.TempDir()
	t.Cleanup(func() { appDirOverride = "" })

	db, err := openDB()
	require.NoError(t, err)
	defer db.Close()

	r := sampleRecord()
	require.NoError(t, insertTicket(db, r))

	dups, err := findDuplicates(db, r.Title, r.Description)
	require.NoError(t, err)
	require.Len(t, dups, 1)
	assert.Equal(t, r.JiraKey, dups[0].JiraKey)
}

func TestFindDuplicates_DifferentDescription_NoMatch(t *testing.T) {
	appDirOverride = t.TempDir()
	t.Cleanup(func() { appDirOverride = "" })

	db, err := openDB()
	require.NoError(t, err)
	defer db.Close()

	r := sampleRecord()
	require.NoError(t, insertTicket(db, r))

	dups, err := findDuplicates(db, r.Title, "completely different description")
	require.NoError(t, err)
	assert.Empty(t, dups)
}

func TestFindDuplicates_DifferentTitle_NoMatch(t *testing.T) {
	appDirOverride = t.TempDir()
	t.Cleanup(func() { appDirOverride = "" })

	db, err := openDB()
	require.NoError(t, err)
	defer db.Close()

	r := sampleRecord()
	require.NoError(t, insertTicket(db, r))

	dups, err := findDuplicates(db, "A Completely Different Title", r.Description)
	require.NoError(t, err)
	assert.Empty(t, dups)
}

func TestFindDuplicates_MultipleMatches(t *testing.T) {
	appDirOverride = t.TempDir()
	t.Cleanup(func() { appDirOverride = "" })

	db, err := openDB()
	require.NoError(t, err)
	defer db.Close()

	r1 := sampleRecord(func(r *TicketRecord) { r.JiraKey = "PROJ-1" })
	r2 := sampleRecord(func(r *TicketRecord) { r.JiraKey = "PROJ-2" })
	require.NoError(t, insertTicket(db, r1))
	require.NoError(t, insertTicket(db, r2))

	dups, err := findDuplicates(db, r1.Title, r1.Description)
	require.NoError(t, err)
	assert.Len(t, dups, 2)
}

func TestFindEpicsByTitle_OnlyReturnsEpics(t *testing.T) {
	appDirOverride = t.TempDir()
	t.Cleanup(func() { appDirOverride = "" })

	db, err := openDB()
	require.NoError(t, err)
	defer db.Close()

	task := sampleRecord(func(r *TicketRecord) {
		r.JiraKey = "PROJ-1"
		r.Title = "My Epic"
		r.TicketType = "Task" // same title, different type
	})
	epic := sampleRecord(func(r *TicketRecord) {
		r.JiraKey = "PROJ-2"
		r.Title = "My Epic"
		r.TicketType = "Epic"
	})
	require.NoError(t, insertTicket(db, task))
	require.NoError(t, insertTicket(db, epic))

	epics, err := findEpicsByTitle(db, "My Epic")
	require.NoError(t, err)
	require.Len(t, epics, 1)
	assert.Equal(t, "PROJ-2", epics[0].JiraKey)
	assert.Equal(t, "Epic", epics[0].TicketType)
}

func TestFindEpicsByTitle_NoMatch(t *testing.T) {
	appDirOverride = t.TempDir()
	t.Cleanup(func() { appDirOverride = "" })

	db, err := openDB()
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, insertTicket(db, sampleRecord()))

	epics, err := findEpicsByTitle(db, "Nonexistent Epic")
	require.NoError(t, err)
	assert.Empty(t, epics)
}

func TestInsertTicket_NilLabelsRoundtrip(t *testing.T) {
	appDirOverride = t.TempDir()
	t.Cleanup(func() { appDirOverride = "" })

	db, err := openDB()
	require.NoError(t, err)
	defer db.Close()

	r := sampleRecord(func(r *TicketRecord) { r.Labels = nil })
	require.NoError(t, insertTicket(db, r))

	records, err := allTickets(db)
	require.NoError(t, err)
	require.Len(t, records, 1)
	// Must come back as an empty (non-nil) slice, not nil.
	assert.NotNil(t, records[0].Labels)
	assert.Empty(t, records[0].Labels)
}

func TestInsertTicket_EmptyOptionalFields(t *testing.T) {
	appDirOverride = t.TempDir()
	t.Cleanup(func() { appDirOverride = "" })

	db, err := openDB()
	require.NoError(t, err)
	defer db.Close()

	r := TicketRecord{
		Title:      "Minimal",
		JiraKey:    "PROJ-1",
		URL:        "https://example.com",
		CreatedAt:  time.Now().UTC(),
		TicketType: "Task",
	}
	require.NoError(t, insertTicket(db, r))

	records, err := allTickets(db)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Empty(t, records[0].ParentKey)
	assert.Empty(t, records[0].ParentURL)
	assert.Empty(t, records[0].Assignee)
	assert.NotNil(t, records[0].Labels)
}

func TestInsertTicket_WithParent(t *testing.T) {
	appDirOverride = t.TempDir()
	t.Cleanup(func() { appDirOverride = "" })

	db, err := openDB()
	require.NoError(t, err)
	defer db.Close()

	r := sampleRecord(func(r *TicketRecord) {
		r.TicketType = "Story"
		r.ParentKey = "EPIC-1"
		r.ParentURL = "https://example.atlassian.net/browse/EPIC-1"
	})
	require.NoError(t, insertTicket(db, r))

	records, err := allTickets(db)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "EPIC-1", records[0].ParentKey)
	assert.Equal(t, "https://example.atlassian.net/browse/EPIC-1", records[0].ParentURL)
}
