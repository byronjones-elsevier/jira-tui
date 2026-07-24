# Engineering Reference

Architecture and implementation notes for `jira-tui`.

## File layout

| File | Purpose |
|------|---------|
| `main.go` | Entry point; parses flags (`-ce`, `-st`, `-h`), opens DB, launches bubbletea |
| `model.go` | Entire TUI — screen state machine, `Update`, `View`, key handlers |
| `jira.go` | Jira REST API client (auth, boards, user lookup, `CreateIssue`, `CreateEpic`) |
| `db.go` | SQLite history DB — `openDB`, `insertTicket`, `findDuplicates`, `allTickets` |
| `config.go` | Config struct, `loadConfig`, `saveConfig` — `~/.jira-tui/config` |
| `cache.go` | Board list cache — `~/.jira-tui/boards_cache.json` (load/save/formatAge) |
| `csv.go` | `Ticket` struct and `parseCSV()` |
| `styles.go` | lipgloss colour vars, style vars, `progressBar()`, `truncate()` |

## Modes

The program has three modes, selected at launch via flags:

| Flag | Mode | Behaviour |
|------|------|-----------|
| _(none)_ | `modeNormal` | Auth → board → settings → ticket list → create → done |
| `--create-epic` / `-ce` | `modeEpic` | Same flow but inserts Epic Setup screen; tickets become children of the created epic |
| `--show-tickets` / `-st` | `modeShow` | Skips Jira auth entirely; loads local history from SQLite and displays it |

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
```

- `[screenDupCheck]` and `[screenEpicDupWarn]` are only shown when duplicates are found in the local DB.
- Each screen has its own `view*()` and `handle*Key()` method on `model`.
- Async operations are `tea.Cmd` closures; results come back as typed messages.

## Message types

| Message | Trigger | Handler action |
|---------|---------|----------------|
| `authVerifiedMsg` | After `VerifyAuth()` | Save config; check cache → show boards or spinner |
| `boardsLoadedMsg` | After `GetBoards()` (cold start) | Save cache; show board picker |
| `boardsSyncedMsg` | After `GetBoards()` (background) | Update list; save cache; clear syncing flag |
| `epicCreatedMsg` | After `CreateEpic()` | Save epic to DB; start first child ticket |
| `ticketCreatedMsg` | After `CreateIssue()` | Save to DB; chain next ticket or go to Done |
| `historyLoadedMsg` | After `allTickets()` | Populate history list |
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
| Create issue | `POST /rest/api/2/issue` |
| Create epic | `POST /rest/api/2/issue` with `issuetype.name=Epic`; tries `customfield_10011` (Epic Name), retries without on error |

All calls use HTTP Basic auth (`email:apiToken`).

## Key design decisions

**Why not use the `api/v3` boards endpoint?**
The agile boards endpoint `agile/1.0/board` returns scrum/kanban boards with project context (`location.projectKey`). The v3 projects endpoint returns projects, not boards.

**Why manual JSON building in `CreateIssue`/`CreateEpic`?**
The description field in REST v2 accepts a plain string. Using `json.Marshal` per field avoids struct-tag noise and handles escaping correctly without a full payload struct.

**Why synchronous DB queries inside `Update`?**
SQLite local reads/writes are sub-millisecond. Wrapping them in `tea.Cmd` goroutines would add complexity with no UX benefit. The bubbletea model's `Update` function is called on the UI goroutine; a microsecond DB call has no perceptible impact.

**Why `modernc.org/sqlite` and not `mattn/go-sqlite3`?**
`modernc.org/sqlite` is a pure-Go port — no CGO required, so cross-compilation works out of the box. `mattn/go-sqlite3` requires a C compiler.

## Known issues / future work

- `CreateIssue` uses REST v2 description (plain string). If the Jira instance requires v3 Atlassian Document Format (ADF), the ticket is created with no description. Fix: detect v3 requirement or always send ADF.
- No retry logic on API errors during batch creation.
- Board cache has no TTL — refreshed on every launch.

## Build

```bash
go build -o jira-tui .
go mod tidy   # if dependencies have drifted
```

## Dependencies

| Package | Use |
|---------|-----|
| `charmbracelet/bubbletea` | TUI framework (elm arch) |
| `charmbracelet/bubbles` | textinput, spinner components |
| `charmbracelet/lipgloss` | Styling and layout |
| `modernc.org/sqlite` | Pure-Go SQLite driver (no CGO) |
