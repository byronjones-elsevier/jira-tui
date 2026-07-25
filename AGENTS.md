# Agent Context — jira-tui

Context for AI coding assistants working in this repo.

## What this is

A Go terminal UI app that creates Jira tickets interactively. Started as a rewrite of `create-jira-tickets.sh` (kept for reference — do not modify). Built with bubbletea (elm architecture), lipgloss (styles), and bubbles (components). Pure-Go SQLite via `modernc.org/sqlite` (no CGO).

## File map (read these before editing)

```
main.go       – entry point; flag parsing (-ce -ct -st -ccfe -pk -h), first-run check, DB open, bubbletea launch
model.go      – ALL TUI logic: screen state machine, Update, View, key handlers, tea.Cmd closures
jira.go       – Jira REST API client (auth, boards, user lookup, CreateIssue, CreateEpic,
                GetIssue, GetEpicChildren, searchIssues, IssueDescription, toADFJSON)
db.go         – SQLite history DB (openDB, insertTicket, findDuplicates, allTickets, deleteTicket)
config.go     – ~/.jira-tui/config read/write (KEY="value" bash-compatible format)
cache.go      – ~/.jira-tui/boards_cache.json load/save + formatAge helper; 24-hour TTL
csv.go        – Ticket struct + parseCSV (BOM-stripping, empty-title filtering)
styles.go     – lipgloss colour vars, style vars, progressBar(), truncate()
Makefile      – build, test, lint, install, run targets

.github/workflows/ci.yml      – build + test + lint on push/PR
.github/workflows/release.yml – cross-compile 6 targets + GitHub release on v* tag

VHS_Testing.tape   – VHS tape for interactive TUI recording and testing
run_vhs_tests.sh   – test runner: assertion tests + VHS recording; requires vhs + gum
MANUAL_TESTING.md  – hierarchical manual testing guide (hierarchy IDs for each screen/flow)
```

## Build command

```bash
go build -o jira-tui .
# Or:
make build
```

Run tests:
```bash
go test ./...
make test
```

## Modes

| Flag | Const | Flow |
|------|-------|------|
| _(none)_ | `modeNormal` | Auth → board → settings → ticket list → create → done |
| `-ce` / `--create-epic` | `modeEpic` | Same + Epic Setup screen; tickets become children of the new epic |
| `-ct` / `--create-ticket` | `modeManual` | Auth → board → manual form → create; if Epic type, loops to add subtasks |
| `-st` / `--show-tickets` | `modeShow` | Local SQLite history only; no network |
| `-ccfe <KEY>` / `--create-csv-from-epic <KEY>` | `modeEpicToCSV` | Auth → query ticket (any type with children) → review children → save CSV |
| `-pk <KEY>` / `--project-key <KEY>` | _(flag, any mode)_ | Skip board picker; sets `m.projectKey` directly |

## Screen state machine

```
modeNormal / modeEpic / modeManual / modeEpicToCSV:
  [screenFirstRun] (only on very first launch when ~/.jira-tui/ does not exist)
  screenAuth → screenVerify → screenBoards (skipped with -pk)
    modeNormal:   → screenSettings → [screenDupCheck] → screenTickets → screenCreating → screenDone
    modeEpic:     → screenSettings → screenEpicSetup → [screenEpicDupWarn] → screenTickets
                    → [screenDupCheck] → screenCreating → screenDone
    modeManual:   → screenManualEntry → [screenManualContinue] → screenManualEntry (loop) → screenDone
    modeEpicToCSV:→ screenEpicCSVQuery → screenEpicCSVReview → screenEpicCSVPath → quit

modeShow: screenShowTickets → quit
```

## Screen enum (model.go)

```go
screenFirstRun                  // first-launch directory setup
screenAuth                      // credential entry form
screenVerify                    // verifying creds (spinner)
screenBoards                    // searchable board picker
screenSettings                  // assignee fallback + issue type selector
screenEpicSetup                 // epic title / description / requester
screenEpicDupWarn               // epic title collision warning (Y/N)
screenTickets                   // ticket list + preview panel
screenDupCheck                  // per-ticket duplicate decision
screenCreating                  // batch creation progress
screenDone                      // results summary
screenShowTickets               // history browser (--show-tickets)
screenManualEntry               // single-ticket form (--create-ticket)
screenManualContinue            // "add a subtask?" prompt (after Epic creation)
screenEpicCSVQuery              // ticket fetch spinner + error state
screenEpicCSVReview             // scrollable child issue list
screenEpicCSVPath               // file-path text input
screenExportCSV                 // Done-screen 'e' export: path input, overwrite confirm, save feedback
```

## Jira API calls

| Call | Method | Endpoint |
|------|--------|----------|
| Auth verify | GET | `/rest/api/2/myself` |
| Boards (paginated) | GET | `/rest/agile/1.0/board?maxResults=50&startAt=N` |
| Resolve assignee | GET | `/rest/api/3/user/search?query=<email>` |
| Create issue (v2) | POST | `/rest/api/2/issue` |
| Create issue (ADF/v3) | POST | `/rest/api/3/issue` (when `JIRA_USE_ADF=true`) |
| Fetch single issue | GET | `/rest/api/2/issue/{key}?fields=...` |
| JQL search | POST | `/rest/api/3/search/jql` (JSON body: jql, fields, maxResults, nextPageToken) |
| Delete issue | DELETE | `/rest/api/2/issue/{key}` — requires "Delete Issues" project permission |

`searchIssues` uses cursor-based pagination via `nextPageToken`. The old `GET /rest/api/2/search` endpoint was removed by Atlassian (HTTP 410).

## Conventions

- Model is a **value type** (`model`, not `*model`) — standard bubbletea pattern.
- Async work is `tea.Cmd` closures; results come back as typed message structs.
- All `tea.Cmd` functions are named `cmd*` (e.g. `cmdVerifyAuth`, `cmdCreateIssue`).
- Colours defined as `lipgloss.Color` vars in `styles.go`.
- `clamp(v, min, max int)` is local in `model.go` — do not use `min`/`max` builtins (shadowing).
- Config format: `KEY="value"` per line. `saveConfig` rewrites known keys, preserves unknowns.
- Data dir: `~/.jira-tui/` (override with `JIRA_TUI_DIR`). Legacy `~/.jira_config` fallback still supported.

## Testing

Unit tests in `*_test.go` alongside source. Use `go test ./...`.

**VHS integration tests** (`run_vhs_tests.sh`):
- Requires `vhs` and `gum` on PATH (`brew install vhs gum`).
- Requires `.env.test` with real Jira credentials (see `.env.test.example`).
- Runs assertion-based CLI tests first (no network), then launches `VHS_Testing.tape` for TUI recording.
- All `th()` header calls in the tape appear **only at the shell prompt** — never inside a running TUI session (Bubbletea's alt-screen would receive the Enter keystroke as a quit command).

## Key gotchas

- `bubbles/spinner` uses `spinner.Dot` (singular), not `spinner.Dots`.
- Description field is `json.RawMessage` — Jira v2 returns a plain string; v3 returns ADF JSON. `IssueDescription()` handles both.
- `GetEpicChildren` works for any issue type with children; tries `parent = KEY` first, then falls back to `"Epic Link" = KEY` for classic projects.
- `createCSVFromEpic` saves to the **current working directory**, not `~/.jira-tui/`.
- Config is saved atomically: write to temp file, then `os.Rename` — prevents credential loss on crash.
- `lipgloss.SetHasDarkBackground(true)` is called in `main()` before `tea.NewProgram()`. Do not remove it. `textarea.New()` creates styles with `lipgloss.AdaptiveColor`; without this call, lipgloss fires a one-time OSC 11 terminal background-color query the first time the epic description textarea renders. By that point bubbletea owns stdin and the terminal response arrives as keystrokes in the focused input.
- `exportCSVResult*` fields on the model drive `screenExportCSV` (opened by pressing `e` on `screenDone`). `exportResultsCSV(path string)` takes the path as a parameter. Default filename is `<epicKey>.csv` when an epic key is set, otherwise `jira_tickets_results.csv` (`defaultExportPath()`). The overwrite check uses `os.Stat` — if the file exists, `exportCSVResultConfirm=true` gates an inline y/n prompt before writing.
- `--show-tickets` has two delete modes: `d` deletes the local DB record only (synchronous, no network); `D` (shift+d) calls `DeleteIssue` (async `tea.Cmd`), which removes the issue from Jira then removes the local record on success. `histConfirmDelete` / `histConfirmDeleteJira` gate the respective confirmation prompts; `histJiraDeleteErr` holds any Jira API error and is displayed in the footer until the next keypress. `cmdDeleteJiraIssue` fires the async delete and returns `jiraDeleteMsg`.
