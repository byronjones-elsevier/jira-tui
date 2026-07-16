package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const boardsCacheFilename = ".jira_boards_cache.json"

type boardsCache struct {
	BaseURL   string    `json:"base_url"`
	UpdatedAt time.Time `json:"updated_at"`
	Boards    []Board   `json:"boards"`
}

func boardsCachePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, boardsCacheFilename)
}

// loadBoardsCache returns cached boards for the given base URL.
// Returns (nil, zero, false) on any miss or mismatch.
func loadBoardsCache(baseURL string) ([]Board, time.Time, bool) {
	data, err := os.ReadFile(boardsCachePath())
	if err != nil {
		return nil, time.Time{}, false
	}
	var cache boardsCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, time.Time{}, false
	}
	if cache.BaseURL != baseURL {
		return nil, time.Time{}, false
	}
	return cache.Boards, cache.UpdatedAt, true
}

// saveBoardsCache persists boards to disk with the current timestamp.
func saveBoardsCache(baseURL string, boards []Board) error {
	data, err := json.MarshalIndent(boardsCache{
		BaseURL:   baseURL,
		UpdatedAt: time.Now(),
		Boards:    boards,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(boardsCachePath(), data, 0600)
}

// formatAge returns a human-readable time since t, e.g. "3m ago", "2h ago".
func formatAge(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	ago := time.Since(t)
	switch {
	case ago < time.Minute:
		return "just now"
	case ago < time.Hour:
		return fmt.Sprintf("%dm ago", int(ago.Minutes()))
	case ago < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(ago.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(ago.Hours()/24))
	}
}
