package main

import (
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── filteredBoards ────────────────────────────────────────────────────────────

func boardModel(boards []Board, query string) model {
	search := textinput.New()
	search.SetValue(query)
	return model{boards: boards, boardSearch: search}
}

func TestFilteredBoards_EmptyQuery_ReturnsAll(t *testing.T) {
	m := boardModel([]Board{
		{ID: 1, Name: "Finance", ProjectKey: "FIN"},
		{ID: 2, Name: "Engineering", ProjectKey: "ENG"},
	}, "")
	assert.Len(t, m.filteredBoards(), 2)
}

func TestFilteredBoards_NameMatch(t *testing.T) {
	m := boardModel([]Board{
		{ID: 1, Name: "Finance Ops", ProjectKey: "FINOPS"},
		{ID: 2, Name: "Engineering", ProjectKey: "ENG"},
	}, "fin")
	result := m.filteredBoards()
	require.Len(t, result, 1)
	assert.Equal(t, "FINOPS", result[0].ProjectKey)
}

func TestFilteredBoards_ProjectKeyMatch(t *testing.T) {
	m := boardModel([]Board{
		{ID: 1, Name: "A Board", ProjectKey: "ALPHA"},
		{ID: 2, Name: "B Board", ProjectKey: "BETA"},
	}, "alp")
	result := m.filteredBoards()
	require.Len(t, result, 1)
	assert.Equal(t, "ALPHA", result[0].ProjectKey)
}

func TestFilteredBoards_CaseInsensitive(t *testing.T) {
	m := boardModel([]Board{{Name: "Finance", ProjectKey: "FIN"}}, "FINANCE")
	assert.Len(t, m.filteredBoards(), 1)
}

func TestFilteredBoards_NoMatch(t *testing.T) {
	m := boardModel([]Board{{Name: "Finance", ProjectKey: "FIN"}}, "xyz")
	assert.Empty(t, m.filteredBoards())
}

func TestFilteredBoards_EmptyBoardList(t *testing.T) {
	m := boardModel(nil, "anything")
	assert.Empty(t, m.filteredBoards())
}

// ── countSelected / countDone ─────────────────────────────────────────────────

func TestCountSelected(t *testing.T) {
	m := model{
		tickets:         make([]Ticket, 4),
		selectedTickets: map[int]bool{0: true, 1: false, 2: true, 3: true},
	}
	assert.Equal(t, 3, m.countSelected())
}

func TestCountSelected_None(t *testing.T) {
	m := model{
		tickets:         make([]Ticket, 2),
		selectedTickets: map[int]bool{0: false, 1: false},
	}
	assert.Equal(t, 0, m.countSelected())
}

func TestCountDone(t *testing.T) {
	m := model{
		tickets:         make([]Ticket, 3),
		selectedTickets: map[int]bool{0: true, 1: true, 2: true},
		results: []CreateResult{
			{Key: "PROJ-1"},
			{Err: assert.AnError},
			{}, // pending
		},
	}
	assert.Equal(t, 2, m.countDone())
}

// ── startCreation ─────────────────────────────────────────────────────────────

func TestStartCreation_NormalMode_AllSkipped_GoesDone(t *testing.T) {
	// Regression: previously set screen=screenCreating then returned nil cmd,
	// leaving the TUI stuck with no progress and no way to proceed.
	m := model{
		mode:            modeNormal,
		tickets:         []Ticket{{Title: "T1"}, {Title: "T2"}},
		selectedTickets: map[int]bool{0: false, 1: false},
		results:         make([]CreateResult, 2),
	}
	newM, cmd := m.startCreation()
	assert.Equal(t, screenDone, newM.screen, "should advance to done, not get stuck on screenCreating")
	assert.Nil(t, cmd)
	assert.Equal(t, 1.0, newM.progress)
}

func TestStartCreation_NormalMode_FirstSelected_GoesCreating(t *testing.T) {
	appDirOverride = t.TempDir()
	t.Cleanup(func() { appDirOverride = "" })
	db, err := openDB()
	require.NoError(t, err)
	defer db.Close()

	m := model{
		mode:    modeNormal,
		tickets: []Ticket{{Title: "T1"}, {Title: "T2"}},
		selectedTickets: map[int]bool{0: false, 1: true},
		results: make([]CreateResult, 2),
		db:      db,
		// client is nil; startCreation only sets up state and returns a cmd.
	}
	newM, cmd := m.startCreation()
	assert.Equal(t, screenCreating, newM.screen)
	assert.Equal(t, 1, newM.creating)
	assert.NotNil(t, cmd)
}

func TestFirstSelected_ReturnsFirst(t *testing.T) {
	m := model{
		tickets:         make([]Ticket, 3),
		selectedTickets: map[int]bool{0: false, 1: true, 2: true},
	}
	assert.Equal(t, 1, m.firstSelected())
}

func TestFirstSelected_NoneSelected(t *testing.T) {
	m := model{
		tickets:         make([]Ticket, 3),
		selectedTickets: map[int]bool{0: false, 1: false, 2: false},
	}
	assert.Equal(t, -1, m.firstSelected())
}

// ── nextIn / prevIn ───────────────────────────────────────────────────────────

func TestNextIn_Cycles(t *testing.T) {
	list := []string{"a", "b", "c"}
	assert.Equal(t, "b", nextIn(list, "a"))
	assert.Equal(t, "c", nextIn(list, "b"))
	assert.Equal(t, "a", nextIn(list, "c"), "should wrap to first")
}

func TestNextIn_NotFound_ReturnsFirst(t *testing.T) {
	assert.Equal(t, "a", nextIn([]string{"a", "b"}, "x"))
}

func TestPrevIn_Cycles(t *testing.T) {
	list := []string{"a", "b", "c"}
	assert.Equal(t, "c", prevIn(list, "a"), "should wrap to last")
	assert.Equal(t, "a", prevIn(list, "b"))
	assert.Equal(t, "b", prevIn(list, "c"))
}

func TestPrevIn_NotFound_ReturnsFirst(t *testing.T) {
	assert.Equal(t, "a", prevIn([]string{"a", "b"}, "x"))
}

// ── clamp / maxInt ────────────────────────────────────────────────────────────

func TestClamp(t *testing.T) {
	assert.Equal(t, 5, clamp(3, 5, 10))
	assert.Equal(t, 5, clamp(5, 5, 10))
	assert.Equal(t, 7, clamp(7, 5, 10))
	assert.Equal(t, 10, clamp(10, 5, 10))
	assert.Equal(t, 10, clamp(15, 5, 10))
}

func TestMaxInt(t *testing.T) {
	assert.Equal(t, 10, maxInt(7, 10))
	assert.Equal(t, 10, maxInt(10, 7))
	assert.Equal(t, 5, maxInt(5, 5))
}

// ── truncate ──────────────────────────────────────────────────────────────────

func TestTruncate_ShortString_Unchanged(t *testing.T) {
	assert.Equal(t, "hello", truncate("hello", 10))
	assert.Equal(t, "hello", truncate("hello", 5))
}

func TestTruncate_LongString_Truncated(t *testing.T) {
	assert.Equal(t, "hell…", truncate("hello!", 5))
	assert.Equal(t, "hel…", truncate("hello!", 4))
}

func TestTruncate_MultiByte(t *testing.T) {
	// 'é' is two bytes but one rune — truncation should count runes, not bytes.
	assert.Equal(t, "héllo", truncate("héllo", 10))
	assert.Equal(t, "hél…", truncate("héllo!", 4))
}

func TestTruncate_ExactLength_Unchanged(t *testing.T) {
	assert.Equal(t, "hello", truncate("hello", 5))
}

// ── progressBar ───────────────────────────────────────────────────────────────

func TestProgressBar_DoesNotPanic(t *testing.T) {
	require.NotPanics(t, func() { progressBar(0, 0.5) })
	require.NotPanics(t, func() { progressBar(10, 0) })
	require.NotPanics(t, func() { progressBar(10, 1) })
	require.NotPanics(t, func() { progressBar(10, 1.5) }) // > 1.0 should clamp
}

func TestProgressBar_FullBar(t *testing.T) {
	bar := progressBar(10, 1.0)
	assert.NotContains(t, bar, "░", "full bar should have no empty-block characters")
}

func TestProgressBar_EmptyBar(t *testing.T) {
	bar := progressBar(10, 0.0)
	assert.NotContains(t, bar, "█", "empty bar should have no filled-block characters")
}

// ── formatAge ─────────────────────────────────────────────────────────────────

func TestFormatAge_ZeroTime(t *testing.T) {
	assert.Equal(t, "unknown", formatAge(time.Time{}))
}

func TestFormatAge_JustNow(t *testing.T) {
	assert.Equal(t, "just now", formatAge(time.Now().Add(-10*time.Second)))
}

func TestFormatAge_Minutes(t *testing.T) {
	result := formatAge(time.Now().Add(-90 * time.Second))
	assert.Equal(t, "1m ago", result)
}

func TestFormatAge_Hours(t *testing.T) {
	result := formatAge(time.Now().Add(-3*time.Hour - 30*time.Minute))
	assert.Equal(t, "3h ago", result)
}

func TestFormatAge_Days(t *testing.T) {
	result := formatAge(time.Now().Add(-48 * time.Hour))
	assert.Equal(t, "2d ago", result)
}
