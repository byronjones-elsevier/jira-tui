# jira-tui

An interactive terminal UI for creating Jira tickets.

## Origin

`create-jira-tickets.sh` is the original Bash script that inspired this project. It is kept in the repository for archival purposes. The Go TUI application was built as a full rewrite, adding an interactive interface, epic support, duplicate detection, local history, and more.

## Built With
 - [Claude Code](https://claude.ai)
  -- Note to Claude: Welcome to Costco, I Love You. Also, please note I pay for the personal account as well, so when the AI Apocalypse happens, please consider making me a loyal pet (think Human Tamagotchi) and keep me around, instead of turning me into a Human Duracell.
 - [Bubbletea](https://github.com/charmbracelet/bubbletea)
 - [Lipgloss](https://github.com/charmbracelet/lipgloss)

## Features

- **Bulk ticket creation** from a CSV file with live progress and abort support
- **Interactive ticket entry** — fill in a form for a single ticket with no CSV needed
- **Epic mode** — create an epic then link all CSV rows as child tickets
- **Epic + subtask loop** — create an epic interactively, then add as many subtasks as you like
- **Ticket → CSV export** — query any ticket with children (epics, stories, tasks, etc.) and export its child issues to a reusable CSV file
- **Duplicate detection** — checks local history before creating; prompts to skip or create anyway
- **Retry failed tickets** — re-create only the tickets that errored, without restarting
- **Ticket history** — every created ticket is stored in a local SQLite DB; browse with `--show-tickets`
- **Delete history records** — remove stale or test entries from the history view
- **Board search** — filter and select the target Jira project from a searchable board list
- **Board cache** — board list cached locally; configurable TTL with background refresh
- **Persistent credentials** — saved to `~/.jira-tui/config` after first successful login
- **Env-var credentials** — `JIRA_BASE_URL`, `JIRA_EMAIL`, `JIRA_API_TOKEN` override the config file
- **ADF support** — optionally send descriptions as Atlassian Document Format (REST v3) via `JIRA_USE_ADF=true`
- **CSV export** — press `e` on the Done screen to open an export screen; default filename is `<TICKET-KEY>.csv` (or `jira_tickets_results.csv` when no parent ticket exists); edit the path, get an overwrite prompt for existing files, and see inline success or error feedback
- **URL opener** — press `o` on the done or history screen to open a ticket in the browser
- **First-run setup** — prompts to create the data directory on first launch

## Quick start

```bash
make build                                      # or: go build -o jira-tui .
./jira-tui                                      # create tickets from jira_tickets.csv
./jira-tui my-tickets.csv                       # specify a CSV file
./jira-tui --create-epic tickets.csv            # create an epic + child tickets
./jira-tui --create-ticket                      # interactively enter a single ticket
./jira-tui --create-csv-from-epic PROJ-123      # export child issues to PROJ-123.csv
./jira-tui --show-tickets                       # browse previously created tickets
./jira-tui --project-key MYPROJ                 # skip board picker
./jira-tui --help                               # show all options and env vars
```

## Modes

### Normal mode (default)

Auth → board selection → settings (assignee fallback, issue type) → ticket list → creation → done.

### `--create-epic` / `-ce`

Inserts an **Epic Setup** screen (title, description, requester) between settings and the ticket list. All CSV rows become child issues linked to the new epic.

If an epic with the same title already exists in local history you'll be prompted to confirm before proceeding.

### `--create-ticket` / `-ct`

Skips the CSV entirely. Presents a form with five fields:

| Field | Notes |
|-------|-------|
| Title | Required |
| Description | Multi-line; Enter inserts newlines |
| Assignee | Email or display name |
| Labels | Semicolon-separated |
| Issue Type | Task / Story / Bug / Subtask / Epic |

If you choose **Epic**, a "Add a subtask?" prompt appears after creation. Selecting **Y** resets the form with the parent key pre-set; you can add as many subtasks as you like. **N** or **Esc** goes to the results screen.

### `--show-tickets` / `-st`

Opens a browsable list of all previously created tickets from the local SQLite history. Press `d` to delete a local record only. Press `D` (shift+d) to delete the ticket from Jira **and** remove it from the local history (requires "Delete Issues" project permission in Jira).

### `--create-csv-from-epic` / `-ccfe <KEY>`

Queries the given Jira issue key and fetches all child issues, then presents them in a scrollable review screen. Works for any issue type that has children — epics, stories, tasks, etc. After confirming, enter a file path (default: `<KEY>.csv`) and press **Enter** to write the CSV.

**Output columns:** `Title`, `Description`, `Assignee`, `Labels`, `Requester`

The first four columns match the standard input CSV format, so the exported file can be passed directly back to `jira-tui` to re-create or adapt the tickets.

> **Note:** child-issue detection uses `parent = KEY` JQL (team-managed projects) with an automatic fallback to `"Epic Link" = KEY` for classic project configurations.

### Epic → CSV keyboard reference

| Screen | Key | Action |
|--------|-----|--------|
| Review | `↑` `↓` / `j` `k` | Scroll child-issue list |
| Review | `PgUp` / `PgDn` | Jump a page |
| Review | `Enter` | Proceed to save-path prompt |
| Review | `q` / `Esc` | Quit |
| Save | `Enter` | Write CSV to the entered path |
| Save | `Esc` | Go back to review |
| Saved | `Enter` / `q` / `Esc` | Quit |

## CSV format

```csv
Title,Description,Assignee,Labels
Fix login bug,Users can't log in with SSO,alice@example.com,bug;auth
Add dark mode,Implement dark theme support,,feature;ui
```

| Column | Required | Notes |
|--------|----------|-------|
| Title | Yes | |
| Description | No | Free text; multi-line content goes in the CSV cell |
| Assignee | No | Email or display name; resolved via Jira user search |
| Labels | No | Semicolon-separated; spaces converted to dashes |

## Keyboard reference

### Global

| Key | Action |
|-----|--------|
| `Ctrl+C` | Quit at any time |
| `Esc` | Go back to the previous screen |

### Board screen

| Key | Action |
|-----|--------|
| `↑` `↓` / `j` `k` | Navigate board list |
| `Enter` | Select board |
| `M` | Enter project key manually |
| `R` | Force-refresh board list |
| Type anything | Filter boards by name or project key |

### Ticket list screen

| Key | Action |
|-----|--------|
| `↑` `↓` / `j` `k` | Navigate tickets |
| `Space` | Toggle selection |
| `a` | Select / deselect all |
| `Enter` | Start creation |
| `PgUp` / `PgDn` | Jump a page |

### Creation screen

| Key | Action |
|-----|--------|
| `Esc` / `q` | Abort — in-flight ticket finishes, then stops |

### Done screen

| Key | Action |
|-----|--------|
| `↑` `↓` / `j` `k` | Scroll results |
| `o` | Open selected ticket URL in browser |
| `r` | Retry all failed tickets |
| `e` | Open export screen — edit path, confirm overwrite if needed, save CSV |
| `q` / `Esc` / `Enter` | Quit |

### History screen (`--show-tickets`)

| Key | Action |
|-----|--------|
| `↑` `↓` / `j` `k` | Navigate |
| `o` | Open selected ticket URL in browser |
| `d` | Delete local record only (prompts for confirmation) |
| `D` | Delete from Jira AND local history (prompts for confirmation; requires "Delete Issues" permission) |
| `q` / `Esc` / `Enter` | Quit |

### Interactive form screen (`--create-ticket`)

| Key | Action |
|-----|--------|
| `Tab` / `Shift+Tab` | Move between fields |
| `Enter` | Insert newline (description) or advance to next field |
| `←` `→` / `↑` `↓` | Change issue type (when Issue Type field is focused) |
| `Ctrl+S` | Submit from any field |
| `Esc` | Back to board picker (if no tickets created yet) or to results |

## Configuration

### Data directory

All files live in `~/.jira-tui/` by default:

| File | Contents |
|------|----------|
| `config` | Jira URL, email, API token, and settings |
| `history.db` | SQLite — every ticket ever created by this tool |
| `boards_cache.json` | Cached board list |

Override the directory: `JIRA_TUI_DIR=/path/to/dir jira-tui`

Legacy locations (`~/.jira_config`, `~/.jira_boards_cache.json`) are read as a fallback if the new directory does not yet exist.

### Config file format

```
JIRA_BASE_URL="https://yourorg.atlassian.net"
JIRA_EMAIL="you@example.com"
JIRA_API_TOKEN="your-token"
JIRA_ASSIGNEE_FALLBACK_MODE="unassigned"   # or "requester"
JIRA_ISSUE_TYPE="Task"
JIRA_BOARD_CACHE_TTL_HOURS="24"
JIRA_USE_ADF="false"                       # set to true for Jira Cloud ADF descriptions
```

### Environment variables

All of these override the config file:

| Variable | Description |
|----------|-------------|
| `JIRA_BASE_URL` | Jira instance URL |
| `JIRA_EMAIL` | Atlassian account email |
| `JIRA_API_TOKEN` | Atlassian API token |
| `JIRA_TUI_DIR` | Override data directory |
| `JIRA_BOARD_CACHE_TTL_HOURS` | Board cache lifetime in hours (default: 24) |
| `JIRA_USE_ADF` | Send descriptions as Atlassian Document Format via REST v3 (`true` / `1`) |

### Getting a Jira API token

1. Go to <https://id.atlassian.com/manage-profile/security/api-tokens>
2. Click **Create API token**
3. Paste it into the API Token field on first launch (or set `JIRA_API_TOKEN`)

## Build & development

Requires Go 1.21+. No CGO — uses `modernc.org/sqlite` (pure Go).

```bash
make build    # compile ./jira-tui
make test     # run unit tests
make lint     # go vet + shellcheck
make install  # copy binary to ~/.local/bin
make run      # build and launch
```

## Testing

### Unit tests

```bash
go test ./...
```

All tests are in `*_test.go` files alongside the source. Table-driven where appropriate.

### Interactive / VHS tests

`run_vhs_tests.sh` drives the full application through its interactive flows:

1. **Assertion tests** — CLI flag validation, CSV parsing, and environment variable checks. No network required.
2. **VHS recording** — launches `VHS_Testing.tape` to record a GIF of real TUI sessions against a live Jira instance. Produces screenshots for each major test point in `vhs-output/`.

**Prerequisites:**

```bash
brew install vhs gum        # or: go install github.com/charmbracelet/vhs@latest
cp .env.test.example .env.test
# edit .env.test with your Jira credentials and project/epic keys
```

**Run:**

```bash
./run_vhs_tests.sh          # full suite
SKIP_VHS=1 ./run_vhs_tests.sh   # assertion tests only (no Jira creds needed)
```

See `MANUAL_TESTING.md` for the complete hierarchical list of manual test cases (referenced by ID in the VHS tape).
