# Agent Context — jira-tui

Context for AI coding assistants working in this repo.

## What this is

A Go terminal UI app that batch-creates Jira tickets from a CSV. Converted from `create-jira-tickets.sh` (still present for reference — do not modify it). Built with bubbletea (elm architecture), lipgloss (styles), and bubbles (components).

## File map (read these before editing)

```
main.go       – entry point only; ~25 lines
model.go      – ALL TUI logic: screens, Update, View, key handlers, commands
jira.go       – Jira REST API client
config.go     – ~/.jira_config read/write (bash-compatible KEY="value" format)
cache.go      – ~/.jira_boards_cache.json read/write + formatAge helper
csv.go        – CSV parser + Ticket struct
styles.go     – lipgloss vars + progressBar/truncate helpers
```

## Build command

```bash
go build -o jira-tui .
```

Go is not installed in the Cowork sandbox — build must be verified on the host machine.

## Completed features

- [x] Full TUI with screen state machine (Auth → Verify → Boards → Settings → Tickets → Creating → Done)
- [x] Config persistence: `~/.jira_config` (loaded on startup, auto-saved after auth)
- [x] Jira auth verification via `/rest/api/2/myself`
- [x] Board list: fully paginated (loops 50 at a time until `isLast=true`)
- [x] Board list: searchable (case-insensitive substring, resets cursor on query change)
- [x] Board list: scrolling window with PgUp/PgDn and j/k navigation
- [x] Board list: manual project key entry (M key)
- [x] Board list: local cache (`~/.jira_boards_cache.json`) with instant display + async background refresh
- [x] Board list: visual sync indicator (spinner + "syncing…" / "synced Xm ago")
- [x] Board list: manual refresh with R key
- [x] Settings screen: assignee fallback (unassigned / requester) + issue type selector
- [x] Ticket list: two-panel (list + preview), Space to toggle, 'a' for all
- [x] Batch creation: sequential with progress bar and per-ticket status
- [x] Done screen: created/failed summary with issue keys and URLs
- [x] Assignee resolution via `/rest/api/3/user/search` (exact email or display name match)
- [x] Git repo initialised with `.gitignore`

## Pending / known issues

- [ ] `CreateIssue` uses REST v2 plain-string description. Jira instances requiring v3 ADF will create tickets with no description. Detect or always send ADF.
- [ ] No retry on transient API failures during batch create.
- [ ] Board cache has no TTL — always refreshes in background. Could skip sync when cache < N minutes old.
- [ ] Assignee resolution: if two users share a name prefix, wrong one may match. Consider email-only matching.

## Conventions

- Model is a **value type** (`model`, not `*model`) — bubbletea pattern.
- Async work: `tea.Cmd` closures capture `m.client` (pointer, safe to copy).
- Colours: defined as `lipgloss.Color` vars in `styles.go` (purple, green, red, muted, subtle).
- `maxInt(a,b)` is a local helper — do NOT use the Go 1.21 `max` builtin (causes shadowing compile error in this module).
- `clamp(v, min, max int)` is also local in `model.go`.
- Config file format: `KEY="value"` per line. `saveConfig` preserves unknown keys.
- Cache file format: JSON with `base_url` field — used to invalidate cache on Jira URL change.

## Spinner note

`bubbles/spinner` v0.18 uses `spinner.Dot` (singular), not `spinner.Dots`. Using the wrong name causes a compile error.

## Screen enum

```go
screenAuth     // credential entry
screenVerify   // verifying creds (full-screen spinner)
screenBoards   // board list / manual key entry
screenSettings // assignee fallback + issue type
screenTickets  // ticket list + preview
screenCreating // creation progress
screenDone     // results summary
```

## boardPageSize logic

`m.height - 12` clamped to `[4, 25]`. The constant 12 accounts for title, subtitle, search bar, pagination line, scroll arrows, manual-key overlay, error line, footer.

## Cache flow detail

```
authVerifiedMsg received
  loadBoardsCache(baseURL)
  ├─ hit  → m.boards = cached, m.boardsSyncing = true
  │          return Batch(cmdSyncBoards, boardSearch.Focus)
  └─ miss → m.loading = true, loadingMsg = "Loading all boards…"
             return cmdLoadBoards()

boardsLoadedMsg  → saveBoardsCache; m.loading = false; boardSearch.Focus
boardsSyncedMsg  → m.boardsSyncing = false; saveBoardsCache if no error; clamp cursor
```
