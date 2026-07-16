# jira-tui

A terminal UI for batch-creating Jira tickets from a CSV file. Built with [bubbletea](https://github.com/charmbracelet/bubbletea), [lipgloss](https://github.com/charmbracelet/lipgloss), and [bubbles](https://github.com/charmbracelet/bubbles).

## Features

- Authenticates with Jira Cloud via API token
- Fetches all accessible agile boards (fully paginated)
- Caches board list locally; refreshes in background on every launch
- Searchable, paginated board picker
- Per-session settings: assignee fallback and issue type
- Selectable ticket list with live preview
- Batch creates issues with per-ticket progress indicator
- Reads the same `~/.jira_config` format as the original bash script

## Requirements

- Go 1.21+
- A Jira Cloud account with an [API token](https://id.atlassian.com/manage-profile/security/api-tokens)

## Build

```bash
go build -o jira-tui .
```

## Run

```bash
./jira-tui                     # uses jira_tickets.csv in current directory
./jira-tui path/to/tickets.csv # custom CSV path
```

## CSV format

```
Title,Description,Assignee,Labels
My ticket,A description,user@company.com,label1;label2
Another ticket,No assignee,,single-label
```

- **Title** — issue summary
- **Description** — plain text body
- **Assignee** — email or display name; resolved to accountId via Jira API; blank = unassigned
- **Labels** — semicolon-separated; spaces are replaced with hyphens

## Config file

Credentials are saved automatically to `~/.jira_config` after first successful auth:

```
BASE_URL="https://company.atlassian.net"
EMAIL="you@company.com"
API_TOKEN="your-token"
ISSUE_TYPE="Task"
ASSIGNEE_FALLBACK="unassigned"
```

This file is compatible with the original `create-jira-tickets.sh` bash script.

## Board cache

The full board list is cached in `~/.jira_boards_cache.json`. On subsequent launches the list loads instantly from cache, and a fresh fetch runs in the background (shown with a spinner). Press **R** to force a manual refresh.

## Keyboard shortcuts

### Board picker
| Key | Action |
|-----|--------|
| `↑`/`↓`, `j`/`k` | Navigate |
| `PgUp`/`PgDn`, `Ctrl+B`/`Ctrl+F` | Jump a page |
| Type anything | Search boards by name or key |
| `Esc` | Clear search |
| `Enter` | Select board |
| `M` | Enter project key manually |
| `R` | Force background refresh |
| `Ctrl+C` | Quit |

### Ticket list
| Key | Action |
|-----|--------|
| `↑`/`↓`, `j`/`k` | Navigate |
| `Space` | Toggle selection |
| `a` | Select / deselect all |
| `Enter` | Create selected tickets |
| `Ctrl+C` | Quit |
