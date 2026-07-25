package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeCSV writes content to a temp file and returns its path.
func writeCSV(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "tickets.csv")
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
	return path
}

func TestParseCSV_Normal(t *testing.T) {
	path := writeCSV(t, "Title,Description,Assignee,Labels\nFix Login Bug,Users can't log in,alice@example.com,bug;auth\n")
	tickets, err := parseCSV(path)
	require.NoError(t, err)
	require.Len(t, tickets, 1)
	assert.Equal(t, "Fix Login Bug", tickets[0].Title)
	assert.Equal(t, "Users can't log in", tickets[0].Description)
	assert.Equal(t, "alice@example.com", tickets[0].Assignee)
	assert.Equal(t, []string{"bug", "auth"}, tickets[0].Labels)
}

func TestParseCSV_MultipleRows(t *testing.T) {
	path := writeCSV(t, "Title,Description,Assignee,Labels\nTicket A,Desc A,,\nTicket B,Desc B,,label1\n")
	tickets, err := parseCSV(path)
	require.NoError(t, err)
	assert.Len(t, tickets, 2)
	assert.Equal(t, "Ticket A", tickets[0].Title)
	assert.Equal(t, "Ticket B", tickets[1].Title)
}

func TestParseCSV_EmptyFile(t *testing.T) {
	path := writeCSV(t, "")
	tickets, err := parseCSV(path)
	require.NoError(t, err)
	assert.Empty(t, tickets)
}

func TestParseCSV_HeaderOnly(t *testing.T) {
	path := writeCSV(t, "Title,Description,Assignee,Labels\n")
	tickets, err := parseCSV(path)
	require.NoError(t, err)
	assert.Empty(t, tickets)
}

func TestParseCSV_SkipsEmptyTitleRows(t *testing.T) {
	path := writeCSV(t, "Title,Description,Assignee,Labels\n,No title row,,\nReal Ticket,desc,,\n")
	tickets, err := parseCSV(path)
	require.NoError(t, err)
	require.Len(t, tickets, 1)
	assert.Equal(t, "Real Ticket", tickets[0].Title)
}

func TestParseCSV_BOMStripped(t *testing.T) {
	// UTF-8 BOM that Excel prepends to UTF-8 CSV exports.
	content := "\xef\xbb\xbfTitle,Description,Assignee,Labels\nFix Bug,A bug,,\n"
	path := writeCSV(t, content)
	tickets, err := parseCSV(path)
	require.NoError(t, err)
	require.Len(t, tickets, 1)
	assert.Equal(t, "Fix Bug", tickets[0].Title, "BOM must be stripped from first data row title")
}

func TestParseCSV_LabelSpacesToDashes(t *testing.T) {
	path := writeCSV(t, "Title,Description,Assignee,Labels\nTicket,desc,,cost optimization;tech debt\n")
	tickets, err := parseCSV(path)
	require.NoError(t, err)
	require.Len(t, tickets, 1)
	assert.Equal(t, []string{"cost-optimization", "tech-debt"}, tickets[0].Labels)
}

func TestParseCSV_NoLabels(t *testing.T) {
	path := writeCSV(t, "Title,Description,Assignee,Labels\nTicket,desc,,\n")
	tickets, err := parseCSV(path)
	require.NoError(t, err)
	require.Len(t, tickets, 1)
	assert.Nil(t, tickets[0].Labels)
}

func TestParseCSV_MissingColumns(t *testing.T) {
	// Two-column CSV: title and description only — no assignee or labels column.
	path := writeCSV(t, "Title,Description\nTicket A,Some description\n")
	tickets, err := parseCSV(path)
	require.NoError(t, err)
	require.Len(t, tickets, 1)
	assert.Equal(t, "Ticket A", tickets[0].Title)
	assert.Equal(t, "Some description", tickets[0].Description)
	assert.Empty(t, tickets[0].Assignee)
	assert.Nil(t, tickets[0].Labels)
}

func TestParseCSV_QuotedFields(t *testing.T) {
	// RFC 4180 quoting — commas and newlines inside quoted fields.
	path := writeCSV(t, "Title,Description,Assignee,Labels\n\"Budget, Q3\",\"Line one\nLine two\",,\n")
	tickets, err := parseCSV(path)
	require.NoError(t, err)
	require.Len(t, tickets, 1)
	assert.Equal(t, "Budget, Q3", tickets[0].Title)
	assert.Equal(t, "Line one\nLine two", tickets[0].Description)
}

func TestParseCSV_FileNotFound(t *testing.T) {
	_, err := parseCSV("/nonexistent/path/tickets.csv")
	assert.Error(t, err)
}
