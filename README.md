# jira-tui

An interactive terminal UI for creating Jira tickets from a CSV file. Built with [Bubbletea](https://github.com/charmbracelet/bubbletea) and [Lipgloss](https://github.com/charmbracelet/lipgloss).

## Features

- **Bulk ticket creation** from a CSV file with live progress
- **Epic mode** — create an epic then link all CSV rows as child tickets
- **Duplicate detection** — checks local history before creating; prompts to skip or create anyway
- **Ticket history** — every created ticket is stored in a local SQLite DB; browse with `--show-tickets`
- **Board search** — filter and select the target Jira project from a searchable board list
- **Board cache** — board list is cached locally and refreshed in the background
- **Persistent credentials** — saved to `~/.jira-tui/config` after first successful login

## Quick start

```bash
go build -o jira-tui .
./jira-tui                             # create tickets from jira_tickets.csv
./jira-tui my-tickets.csv              # specify a CSV file
./jira-tui --create-epic tickets.csv   # create an epic + child tickets
./jira-tui --show-tickets              # browse previously created tickets
./jira-tui --help                      # show usage
```

## CSV format

```csv
Title,Description,Assignee,Labels
Fix login bug,Users can't log in with SSO,alice@example.com,bug;auth
Add dark mode,Implement dark theme support,,feature;ui
```

- **Title** — required
- **Description** — optional free text
- **Assignee** — email or display name (resolved via Jira user search); leave empty for default
- **Labels** — semicolon-separated; spaces converted to dashes

## Modes

### Normal mode (default)

Walks through: auth → board selection → settings → ticket list → creation → done.

### `--create-epic` / `-ce`

Adds an **Epic Setup** screen (title, description, requester) after the settings screen. CSV rows become child issues linked to the new epic.

If an epic with the same title already exists in local history you'll be asked to confirm before proceeding.

### `--show-tickets` / `-st`

Skips Jira auth entirely and opens a browsable list of all previously created tickets from the local SQLite history. No network access required.

## Keyboard reference

| Key | Action |
|-----|--------|
| `↑` `↓` / `j` `k` | Navigate lists |
| `Tab` / `Shift+Tab` | Move between input fields |
| `Space` | Toggle ticket selection |
| `a` | Select / deselect all tickets |
| `Enter` | Confirm / advance to next screen |
| `Esc` | Go back |
| `PgUp` / `PgDn` | Jump a page in long lists |
| `M` | Enter project key manually (board screen) |
| `R` | Refresh board list |
| `q` | Quit (history screen) |
| `Ctrl+C` | Quit at any time |

## Configuration & data

All files live in `~/.jira-tui/`:

| File | Contents |
|------|----------|
| `config` | Jira URL, email, API token, defaults |
| `history.db` | SQLite — every ticket ever created by this tool |
| `boards_cache.json` | Cached board list (refreshed on each launch) |

Legacy locations (`~/.jira_config`, `~/.jira_boards_cache.json`) are read as a fallback if the new directory doesn't exist yet.

### Getting a Jira API token

1. Go to <https://id.atlassian.com/manage-profile/security/api-tokens>
2. Click **Create API token**
3. Paste it into the API Token field on first launch

## Build

Requires Go 1.21+.

```bash
go build -o jira-tui .
```

No CGO required — uses `modernc.org/sqlite` (pure Go).
