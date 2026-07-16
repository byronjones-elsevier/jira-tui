# Engineering Reference

Architecture and implementation notes for `jira-tui`.

## File layout

| File | Purpose |
|------|---------|
| `main.go` | Entry point; parses args, loads config, launches bubbletea |
| `model.go` | Entire TUI — screen state machine, Update, View, key handlers |
| `jira.go` | Jira REST API client (auth, boards, user lookup, issue creation) |
| `config.go` | Config struct, `loadConfig()`, `saveConfig()` — `~/.jira_config` |
| `cache.go` | Board list cache — `~/.jira_boards_cache.json` (load/save/formatAge) |
| `csv.go` | `Ticket` struct and `parseCSV()` |
| `styles.go` | lipgloss colour vars, style vars, `progressBar()`, `truncate()` |

## Screen state machine

```
screenAuth → screenVerify → screenBoards → screenSettings → screenTickets → screenCreating → screenDone
```

- Each screen has its own `view*()` and `handle*Key()` method.
- Async operations are `tea.Cmd` closures; results come back as typed messages.

## Message types

| Message | Trigger | Handler action |
|---------|---------|----------------|
| `authVerifiedMsg` | After `VerifyAuth()` | Save config; check cache → show boards or spinner |
| `boardsLoadedMsg` | After `GetBoards()` (cold start) | Save cache; show board picker |
| `boardsSyncedMsg` | After `GetBoards()` (background) | Update list; save cache; clear syncing flag |
| `ticketCreatedMsg` | After `CreateIssue()` | Store result; chain next ticket or go to Done |
| `spinner.TickMsg` | Bubbletea ticker | Advance spinner animation |

## Board caching flow

```
authVerified
  ├─ cache hit  → m.boards = cached, m.boardsSyncing = true
  │               Batch(cmdSyncBoards, boardSearch.Focus)
  └─ cache miss → m.loading = true, cmdLoadBoards()

boardsLoadedMsg  → saveBoardsCache, m.loading = false
boardsSyncedMsg  → saveBoardsCache, m.boardsSyncing = false, clamp cursor
```

Cache file: `~/.jira_boards_cache.json`  
Schema: `{ base_url, updated_at, boards: [{ID, Name, ProjectKey}] }`  
Base URL is stored so board lists from different Jira instances don't mix.

## Jira API calls

| Call | Endpoint |
|------|----------|
| Auth verify | `GET /rest/api/2/myself` |
| Boards (paginated) | `GET /rest/agile/1.0/board?maxResults=50&startAt=N` — loops until `isLast=true` |
| Resolve assignee | `GET /rest/api/3/user/search?query=<email>` — exact match on emailAddress or displayName |
| Create issue | `POST /rest/api/2/issue` |

All calls use HTTP Basic auth (`email:apiToken`).

## Key design decisions

**Why not use the `api/v3` boards endpoint?**  
The agile boards endpoint `agile/1.0/board` returns scrum/kanban boards with project context (the `location.projectKey` field). The v3 projects endpoint returns projects, not boards — different concept.

**Why manual JSON building in `CreateIssue`?**  
The description field in REST v2 accepts a plain string. Using `json.Marshal` per field avoids struct-tag noise and handles escaping correctly without a full payload struct.

**Why `boardOffset` sliding window instead of discrete pages?**  
Allows smooth cursor-follows-content scrolling with `j`/`k` — more natural for a list than a paged `PgDn`-only UX. PgUp/PgDn still jump a full page.

**Config format compatibility**  
`~/.jira_config` uses `KEY="value"` lines (bash-style), matching the original `create-jira-tickets.sh`. `loadConfig` and `saveConfig` preserve unknown keys.

## Known issues / future work

- `CreateIssue` uses REST v2 description (plain string). If the Jira instance requires v3 Atlassian Document Format (ADF), this will fail silently — ticket is created with no description. Fix: detect v3 requirement or always send ADF.
- Assignee resolution does a substring search then exact-matches on emailAddress or displayName. Two users with similar emails could cause a miss. Fix: require exact email match only.
- No retry logic on API errors (transient network issues during batch create will mark a ticket as failed).
- Board cache has no TTL — it's refreshed on every launch. Could add a max-age to skip the background sync when cache is fresh (e.g., < 5 min old).

## Build

```bash
go build -o jira-tui .
go mod tidy          # if dependencies have drifted
```

Go 1.21 required (uses `max` builtin — but local `maxInt` shadows it to avoid conflict).

## Dependencies

| Package | Version | Use |
|---------|---------|-----|
| `charmbracelet/bubbletea` | v0.26.6 | TUI framework (elm arch) |
| `charmbracelet/bubbles` | v0.18.0 | textinput, spinner components |
| `charmbracelet/lipgloss` | v0.10.0 | Styling and layout |
