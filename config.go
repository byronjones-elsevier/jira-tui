package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Config holds all persisted settings.
type Config struct {
	BaseURL          string
	Email            string
	APIToken         string
	DefaultBoardURL  string
	AssigneeFallback string // "unassigned" | "requester"
	IssueType        string // "Task" | "Story" | "Bug" | ...
}

func configPath() string {
	return filepath.Join(appDir(), "config")
}

// oldConfigPath returns the legacy config location for migration fallback.
func oldConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".jira_config")
}

func loadConfig() Config {
	cfg := Config{
		AssigneeFallback: "unassigned",
		IssueType:        "Task",
	}

	// Prefer new location; fall back to legacy location.
	// Skip legacy fallback when appDirOverride is set (test isolation).
	path := configPath()
	if _, err := os.Stat(path); os.IsNotExist(err) && appDirOverride == "" {
		if _, err2 := os.Stat(oldConfigPath()); err2 == nil {
			path = oldConfigPath()
		}
	}

	f, err := os.Open(path)
	if err != nil {
		return cfg
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.Trim(strings.TrimSpace(parts[1]), `"`)
		switch key {
		case "JIRA_BASE_URL":
			cfg.BaseURL = val
		case "JIRA_EMAIL":
			cfg.Email = val
		case "JIRA_API_TOKEN":
			cfg.APIToken = val
		case "JIRA_DEFAULT_BOARD_URL":
			cfg.DefaultBoardURL = val
		case "JIRA_ASSIGNEE_FALLBACK_MODE":
			cfg.AssigneeFallback = val
		case "JIRA_ISSUE_TYPE":
			cfg.IssueType = val
		}
	}
	// Environment variables override config file values.
	if v := os.Getenv("JIRA_BASE_URL"); v != "" {
		cfg.BaseURL = v
	}
	if v := os.Getenv("JIRA_EMAIL"); v != "" {
		cfg.Email = v
	}
	if v := os.Getenv("JIRA_API_TOKEN"); v != "" {
		cfg.APIToken = v
	}

	return cfg
}

func saveConfig(cfg Config) error {
	if err := ensureAppDir(); err != nil {
		return fmt.Errorf("create app dir: %w", err)
	}
	path := configPath()

	// Read existing lines to preserve unknown keys.
	existing := map[string]string{}
	order := []string{}
	if f, err := os.Open(path); err == nil {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				k := strings.TrimSpace(parts[0])
				existing[k] = line
				order = append(order, k)
			}
		}
		f.Close()
	}

	updates := map[string]string{
		"JIRA_BASE_URL":               cfg.BaseURL,
		"JIRA_EMAIL":                  cfg.Email,
		"JIRA_API_TOKEN":              cfg.APIToken,
		"JIRA_ASSIGNEE_FALLBACK_MODE": cfg.AssigneeFallback,
		"JIRA_ISSUE_TYPE":             cfg.IssueType,
	}
	if cfg.DefaultBoardURL != "" {
		updates["JIRA_DEFAULT_BOARD_URL"] = cfg.DefaultBoardURL
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".config-tmp-*")
	if err != nil {
		return fmt.Errorf("could not create temp config: %w", err)
	}
	tmpName := tmp.Name()

	written := map[string]bool{}
	for _, k := range order {
		if v, ok := updates[k]; ok {
			fmt.Fprintf(tmp, "%s=\"%s\"\n", k, v)
			written[k] = true
		} else {
			fmt.Fprintln(tmp, existing[k])
		}
	}
	keys := make([]string, 0, len(updates))
	for k := range updates {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !written[k] {
			fmt.Fprintf(tmp, "%s=\"%s\"\n", k, updates[k])
		}
	}

	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("could not flush temp config: %w", err)
	}
	if err := os.Chmod(tmpName, 0600); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("could not chmod temp config: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("could not write config: %w", err)
	}
	return nil
}
