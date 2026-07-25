# Engineering Reference

Architecture and implementation notes for `jira-tui`.

## File layout

| File | Purpose |
|------|---------|
| `main.go` | Entry point; parses flags (`-ce`, `-ct`, `-st`, `-ccfe`, `-pk`, `-h`), first-run check, opens DB, launches bubbletea |
| `model.go` | Entire TUI — screen state machine, `Update`, `View`, key handlers |
| `jira.go` | Jira REST API client (auth, boards, user lookup, `CreateIssue`, `CreateEpic`, `GetIssue`, `GetEpicChildren`, `searchIssues`, `IssueDescription`, `toADFJSON`) |
| `db.go` | SQLite history DB — `openDB`, `insertTicket`, `findDuplicates`, `allTickets`, `isFirstRun` |
| `config.go` | Config struct, `loadConfig`, `saveConfig` — `~/.jira-tui/config` |
| `cache.go` | Board list cache — `~/.jira-tui/boards_cache.json` (load/save/formatAge) |
| `csv.go` | `Ticket` struct and `parseCSV()` |
| `styles.go` | lipgloss colour vars, style vars, `progressBar()`, `truncate()` |
| `Makefile` | Build, test, lint, install, run targets |
| `.github/workflows/ci.yml` | CI: build + test + lint on push/PR |
| `.github/workflows/release.yml` | Release: cross-compile 6 targets + GitHub release on `v*` tag |

## Modes

The program has five modes, selected at launch via flags:

| Flag | Mode | Behaviour |
|------|------|-----------|
| _(none)_ | `modeNormal` | Auth → board → settings → ticket list → create → done |
| `--create-epic` / `-ce` | `modeEpic` | Same flow but inserts Epic Setup screen; tickets become children of the created epic |
| `--show-tickets` / `-st` | `modeShow` | Skips Jira auth entirely; loads local history from SQLite and displays it |
| `--create-ticket` / `-ct` | `modeManual` | Auth → board → manual form (title/desc/assignee/labels/type) → create; if Epic, loops to offer subtask creation |
| `--create-csv-from-epic <KEY>` / `-ccfe <KEY>` | `modeEpicToCSV` | Auth → query epic by key → review children → choose save path → write CSV |

## Screen state machine

```
modeNormal:
  screenAuth → screenVerify → screenBoards → screenSettings
    → [screenDupCheck] → screenTickets → screenCreating → screenDone

modeEpic:
  screenAuth → screenVerify → screenBoards → screenSettings
    → screenEpicSetup → [screenEpicDupWarn]
    → screenTickets → [screenDupCheck] → screenCreating → screenDone

modeShow:
  screenShowTickets → quit

modeManual:
  screenAuth → screenVerify → screenBoards
    → screenManualEntry (form: title/desc/assignee/labels/issuetype)
    → [screenManualContinue if Epic created] → screenManualEntry (subtask loop)
    → screenDone

modeEpicToCSV:
  screenAuth → screenVerify
    → screenEpicCSVQuery (spinner, then error state on failure)
    → screenEpicCSVReview (scrollable child list, Enter to proceed)
    → screenEpicCSVPath (file-path textinput, Enter to write CSV)
    → success state on screenEpicCSVPath → quit
```

- `[screenDupCheck]` and `[screenEpicDupWarn]` are only shown when duplicates are found in the local DB.
- `screenFirstRun` is shown on the very first launch (before any other screen) when `~/.jira-tui/` does not exist and no override env var is set.
- Each screen has its own `view*()` and `handle*Key()` method on `model`.
- Async operations are `tea.Cmd` closures; results come back as typed messages.

## Message types

| Message | Trigger | Handler action |
|---------|---------|----------------|
| `authVerifiedMsg` | After `VerifyAuth()` | Save config; modeEpicToCSV → epic query; else check cache → show boards or spinner |
| `boardsLoadedMsg` | After `GetBoards()` (cold start) | Save cache; show board picker |
| `boardsSyncedMsg` | After `GetBoards()` (background) | Update list; save cache; clear syncing flag |
| `epicCreatedMsg` | After `CreateEpic()` | Save epic to DB; start first child ticket |
| `ticketCreatedMsg` | After `CreateIssue()` | Save to DB; chain next ticket or go to Done |
| `manualTicketCreatedMsg` | After `cmdCreateManualTicket()` | Save to DB; if Epic → screenManualContinue; else → screenDone |
| `historyLoadedMsg` | After `allTickets()` | Populate history list |
| `epicQueryMsg` | After `GetIssue` + `GetEpicChildren` | On error: stay on screenEpicCSVQuery with error displayed; on success: → screenEpicCSVReview |
| `spinner.TickMsg` | Bubbletea ticker | Advance spinner animation |

## SQLite history DB

**Location:** `~/.jira-tui/history.db`

**Table: `tickets`**

| Column | Type | Notes |
|--------|------|-------|
| `id` | INTEGER PK | Auto-increment |
| `title` | TEXT | Issue summary |
| `description` | TEXT | |
| `jira_key` | TEXT | e.g. `PROJ-42` |
| `url` | TEXT | Browse URL |
| `created_at` | TEXT | RFC3339 UTC |
| `ticket_type` | TEXT | `Task`, `Story`, `Bug`, `Epic`, etc. |
| `parent_key` | TEXT | Set for children of an epic |
| `parent_url` | TEXT | |
| `project_key` | TEXT | |
| `assignee` | TEXT | |
| `labels` | TEXT | JSON array |

**Key queries:**
- `findDuplicates(title, description)` — used before creation to detect re-runs
- `findEpicsByTitle(title)` — used in epic mode to warn on title collision
- `allTickets()` — used by `--show-tickets`

## Duplicate detection flow

```
handleTicketsKey("enter")
  → checkDupsAndProceed()
      for each selected ticket:
        findDuplicates(db, title, description)
        if matches → add to dupItems list
      if dupItems not empty → screenDupCheck
      else                  → startCreation()

screenDupCheck:
  user sets createAnyway=true|false per item
  "enter" → deselect skipped items → startCreation()
```

## Epic creation flow

```
screenEpicSetup:
  user enters title / description / requester
  "enter" on last field:
    findEpicsByTitle(title)
    if match → screenEpicDupWarn (Y/N confirm)
    else     → screenTickets

startCreation() with modeEpic:
  m.epicPending = true → cmdCreateEpic()
  epicCreatedMsg → insertTicket(Epic) → save epicKey
  → start creating child tickets with parentKey = epicKey

CreateIssue(…, parentKey) → {"parent":{"key":epicKey}} in payload
```

## Board caching flow

```
authVerified
  ├─ cache hit  → m.boards = cached, m.boardsSyncing = true
  │               Batch(cmdSyncBoards, boardSearch.Focus)
  └─ cache miss → m.loading = true, cmdLoadBoards()

boardsLoadedMsg  → saveBoardsCache, m.loading = false
boardsSyncedMsg  → saveBoardsCache, m.boardsSyncing = false, clamp cursor
```

## Config and data directory

| Path | Purpose |
|------|---------|
| `~/.jira-tui/config` | Credentials and settings (KEY="value" lines) |
| `~/.jira-tui/history.db` | SQLite ticket history |
| `~/.jira-tui/boards_cache.json` | Cached board list |

Legacy locations (`~/.jira_config`, `~/.jira_boards_cache.json`) are read as a fallback if the new location doesn't yet exist.

## Jira API calls

| Call | Endpoint |
|------|----------|
| Auth verify | `GET /rest/api/2/myself` |
| Boards (paginated) | `GET /rest/agile/1.0/board?maxResults=50&startAt=N` — loops until `isLast=true` |
| Resolve assignee | `GET /rest/api/3/user/search?query=<email>` — exact match on emailAddress or displayName |
| Create issue | `POST /rest/api/2/issue` (v2) or `POST /rest/api/3/issue` (v3 when `JIRA_USE_ADF=true`) |
| Create epic | Same endpoint switching as above; tries `customfield_10011` (Epic Name), retries without on error |
| Fetch single issue | `GET /rest/api/2/issue/{key}?fields=summary,description,assignee,reporter,labels,issuetype,status` |
| Search issues (JQL) | `GET /rest/api/2/search?jql=<jql>&fields=<fields>&maxResults=50&startAt=N` — paginated; used by `GetEpicChildren` |

All calls use HTTP Basic auth (`email:apiToken`).

### Epic child-issue discovery

`GetEpicChildren` runs two JQL queries in sequence:

1. `parent = "EPIC-KEY"` — works for team-managed (Next-gen) Jira Cloud projects and modern classic projects.
2. `"Epic Link" = "EPIC-KEY"` — fallback for older classic Jira Cloud configurations where children reference the epic via a custom field rather than a parent link. Only attempted if the first query returns 0 results.

### Description field handling (`IssueDescription`)

Jira REST v2 returns `description` as a plain string in classic projects and as an ADF (Atlassian Document Format) JSON object in Next-gen projects. `IssueDescription(raw json.RawMessage)` handles all three states:

1. `null` JSON → empty string
2. JSON string literal → returned as-is
3. JSON object → `extractADFPlainText` walks the ADF node tree recursively, extracting `text` leaf values and inserting newlines at `paragraph` / `hardBreak` boundaries

## Key design decisions

**Why not use the `api/v3` boards endpoint?**
The agile boards endpoint `agile/1.0/board` returns scrum/kanban boards with project context (`location.projectKey`). The v3 projects endpoint returns projects, not boards.

**Why manual JSON building in `CreateIssue`/`CreateEpic`?**
The description field in REST v2 accepts a plain string. Using `json.Marshal` per field avoids struct-tag noise and handles escaping correctly without a full payload struct. When `useADF` is true, `toADFJSON` wraps the text in the Atlassian Document Format structure and the v3 endpoint is used instead.

**Why opt-in ADF (`JIRA_USE_ADF`) rather than auto-detecting the Jira version?**
The Jira Cloud API returns HTTP 200 for both v2 and v3 requests regardless of the instance's preferred format. A v2 plain-string description sent to an ADF-only instance silently creates a ticket with no description — there is no error to detect. An explicit opt-in flag makes the behaviour transparent and reversible.

**Why synchronous DB queries inside `Update`?**
SQLite local reads/writes are sub-millisecond. Wrapping them in `tea.Cmd` goroutines would add complexity with no UX benefit. The bubbletea model's `Update` function is called on the UI goroutine; a microsecond DB call has no perceptible impact.

**Why `modernc.org/sqlite` and not `mattn/go-sqlite3`?**
`modernc.org/sqlite` is a pure-Go port — no CGO required, so cross-compilation works out of the box. `mattn/go-sqlite3` requires a C compiler.

## Known issues / future work

### Bugs

| Severity | File | Description |
|----------|------|-------------|
| **High** | `model.go:startCreation` | If all selected tickets are cleared by the dup-check screen, `startCreation()` sets `screen = screenCreating` then returns `nil` cmd — TUI is stuck with no progress and no exit path other than Ctrl+C. **Fixed in test suite.** |
| **High** | `jira.go:CreateEpic` | Retry-without-`customfield_10011` fires on all errors, not just field-validation 400. **Fixed in code** (only retries when error body contains "customfield_10011"). |
| **High** | `jira.go:CreateIssue` | `json.Marshal` errors on all payload fields silently discarded. **Fixed in code** (all marshal calls now propagate errors). |
| **Medium** | `db.go:insertTicket` | `json.Marshal(nil)` for `[]string` returns `"null"`, not `"[]"`. On read-back `scanTickets` sets `Labels` to `nil`. Normalised to `[]string{}` after fix. **Fixed in code.** |
| **Medium** | `db.go:scanTickets` | Malformed `created_at` values silently produce zero-time (`0001-01-01`); corrupted `labels` JSON silently produces nil slice. No warnings emitted. **Fixed in code** (explicit parse checks; corrupted labels default to `[]string{}`). |
| **Medium** | `model.go:checkDupsAndProceed` | DB errors in `findDuplicates` are silently treated as "no duplicates" — duplicate detection is bypassed on DB failure. **Fixed in code** (error surfaces in `m.err` and returns early). |
| **Medium** | `model.go:handleEpicSetupKey` | `Esc` does not blur the currently focused `epicInputs` entry. The component retains its focused state in the background; `Blink` ticks continue; returning to `screenEpicSetup` may show the wrong input as focused. **Fixed in code** (Blur called before screen transition). |
| **Medium** | `config.go:saveConfig` | File is truncated (`O_TRUNC`) before writing. A crash or disk-full between truncate and the final write leaves an empty or partial config file — credentials lost. Fix: write to a temp file then rename. **Fixed in code.** |
| **Low** | `jira.go:GetBoards` | Boards with empty `location.projectKey` are included in the list. Selecting one sets `m.projectKey = ""`, which Jira rejects with a 400 on the next issue creation. **Fixed in code** (boards with empty projectKey skipped). |
| **Low** | `csv.go:parseCSV` | Excel UTF-8 CSVs include a BOM (`\xef\xbb\xbf`) that is prepended to the first field of every non-header row. Ticket titles get a three-byte prefix, breaking duplicate detection on re-runs. **Fixed in code.** |
| **Low** | `jira.go:io.ReadAll` | Errors discarded at lines 65 and 85. A truncated response body produces an opaque JSON parse error. **Fixed in code** (both ReadAll calls now return errors). |
| **Low** | `config.go` / `cache.go` | `os.UserHomeDir()` errors discarded in `oldConfigPath` and `oldBoardsCachePath`. In containers without `$HOME` the path becomes `/.jira_config`. **Fixed in code** (returns "" on error; legacy fallback silently skipped). |

### UX limitations

- **No back-navigation from `screenSettings` or `screenTickets`.** Pressing Esc on either screen does nothing; the only way to reselect a board or change issue type is to restart. **Fixed in code** (Esc → screenBoards from Settings; Esc → screenSettings from Tickets).
- **`screenDone` exits on any keypress.** Pressing an arrow key while reading results quits immediately; the done view has no scrolling even when ticket count overflows the panel. **Fixed in code** (only q/Esc/Enter quit; ↑↓/jk/PgUp/PgDn scroll the results list).
- **No way to open a URL.** Done view and history view display Jira URLs as plain text with no keybinding to open them in a browser. **Fixed in code** (`o` key opens the URL of the cursor row in the system default browser via `open`/`xdg-open`/`start`).
- **No retry for failed tickets.** A transient API error requires a full restart; there is no "retry failed" option. **Fixed in code** (`r` on screenDone resets failed results, deselects successful tickets, and re-enters screenCreating; in epic mode the already-created epic is reused).
- **Assignee resolution failure is silent.** A network error during `ResolveAccountID` is treated as "user not found" — the ticket is created without an assignee with a success checkmark. **Fixed in code** (`!assignee` warning shown in the creation row when ResolveAccountID returns `""` or errors for a non-empty assignee field).
- **Progress bar width hardcoded at 44 chars** (`viewCreating`). Does not adapt to terminal width. **Fixed in code** (bar width derived from `m.width` via panel width formula).
- **`screenCreating` is uninterruptible.** Once creation begins there is no pause or abort; Ctrl+C kills the process but leaves in-flight API calls (20 s timeout) running in background goroutines. **Fixed in code** (Esc/q sets `creationAborted`; the in-flight ticket finishes before the loop stops; subsequent tickets are not started; transitions to screenDone with whatever results exist).
- **`m.err` is never cleared on `screenBoards`.** An error from a failed board load persists in the view after the user begins interacting. **Fixed in code** (cleared at the start of every `handleBoardsKey` call).

### Missing features

- **No delete from history.** `viewShowTickets` is read-only; there is no way to remove a stale or test record from `history.db`. **Fixed in code** (`d` key prompts for confirmation; `y` deletes the record; any other key cancels).
- **No environment-variable credentials.** `JIRA_BASE_URL`, `JIRA_API_TOKEN`, etc. are only read from the config file; CI/CD pipelines cannot inject credentials without writing a file. (`JIRA_TUI_DIR` for the data directory is supported.) **Fixed in code** (`JIRA_BASE_URL`, `JIRA_EMAIL`, `JIRA_API_TOKEN` env vars now override config file values in `loadConfig`).
- **No `--project-key` CLI flag.** Board selection is mandatory even when the project key is already known. **Fixed in code** (`-pk`/`--project-key <KEY>` skips board picker; auth still runs to obtain user credentials).
- **No multi-line description input.** The epic description and ticket descriptions are single-line `textinput` widgets capped at 256 characters; long descriptions must be authored in the CSV. **Fixed in code** (epic description replaced with a `textarea.Model`; Enter inserts newlines, Tab advances to the next field).
- **No CSV export of results.** After creation there is no way to write the `JiraKey → Title` mapping to a file. **Fixed in code** (`e` on screenDone writes `jira_tickets_results.csv` to the working directory with columns Status, Key, Title, URL, Error).
- **No schema migration.** `migrateDB` is a single `CREATE TABLE IF NOT EXISTS`. Adding a column to an existing DB requires a manual `ALTER TABLE`; the code silently does nothing if the table already exists. **Fixed in code** (`schema_version` table tracks version; `migrateDB` runs numbered steps in order; bump `currentSchemaVersion` and add a step for future columns).
- **No cache TTL.** A board list cached months ago is still served as the warm cache on startup (with a background sync); there is no maximum age after which a foreground re-fetch is forced. **Fixed in code** (24-hour TTL; caches older than 24 h are treated as a miss, forcing a foreground fetch).
- **`GetBoards` has no page cap.** If `IsLast` is never true the board-fetch loop runs indefinitely. A limit of ~40 pages would prevent a hang. **Fixed in code** (loop capped at 40 pages = 2 000 boards).
- **`CreateIssue` uses REST v2 plain-string description.** Instances that require Atlassian Document Format (ADF) will create tickets with no description. **Fixed in code** (set `JIRA_USE_ADF=true` or `JIRA_USE_ADF="true"` in config; switches both `CreateIssue` and `CreateEpic` to REST v3 with ADF-wrapped descriptions; `toADFJSON` handles paragraphs and hard breaks).
- **No per-ticket assignee fallback visibility.** When an assignee cannot be resolved, the user is not told; the ticket is created as unassigned silently. **Fixed in code** (`!assignee` tag shown in red on the creation row and carried through to the done screen).

## Build

```bash
make build       # go build -o jira-tui .
make test        # go test ./...
make lint        # go vet + shellcheck
make install     # copy binary to ~/.local/bin
go mod tidy      # if dependencies have drifted
```

### Release

Tag a version to trigger the GitHub Actions release workflow:

```bash
git tag v1.2.3
git push origin v1.2.3
```

The workflow cross-compiles for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64, windows/arm64, attaches binaries and a SHA256 `checksums.txt` to a GitHub release, and auto-generates release notes from commit messages.

## Dependencies

| Package | Use |
|---------|-----|
| `charmbracelet/bubbletea` | TUI framework (elm arch) |
| `charmbracelet/bubbles` | textinput, textarea, spinner components |
| `charmbracelet/lipgloss` | Styling and layout |
| `modernc.org/sqlite` | Pure-Go SQLite driver (no CGO) |
| `stretchr/testify` | Test assertions (`assert`, `require`) |
