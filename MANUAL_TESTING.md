# Manual Testing Guide — jira-tui

Each item has a unique hierarchical ID (e.g. `2.3.1`). Report failures using the ID.

**Prerequisites**

- A running Jira Cloud instance with API access
- A valid Atlassian API token
- At least one Jira project with a board
- An epic that has child issues (for section 6)
- `~/.jira-tui/` directory does **not** exist before running first-run tests (or use `JIRA_TUI_DIR=/tmp/jira-tui-test`)

---

## 1  CLI / Startup

### 1.1  Help

| ID | Steps | Expected outcome |
|----|-------|-----------------|
| 1.1.1 | `./jira-tui -h` | Prints help text, exits 0 |
| 1.1.2 | `./jira-tui -H` | Prints help text, exits 0 |
| 1.1.3 | `./jira-tui --help` | Prints help text, exits 0 |
| 1.1.4 | `./jira-tui --HELP` | Prints help text, exits 0 |
| 1.1.5 | `./jira-tui -?` | Prints help text, exits 0 |
| 1.1.6 | Help text contains all flags: `-ce`, `-ct`, `-st`, `-ccfe`, `-pk`, `-h` | All flags documented |
| 1.1.7 | Help text contains CSV export section | Section "CSV EXPORT" visible with column description |

### 1.2  Input validation

| ID | Steps | Expected outcome |
|----|-------|-----------------|
| 1.2.1 | `./jira-tui --create-csv-from-epic` (no key) | Stderr: "error: --create-csv-from-epic requires a ticket ID…"; exit 1 |
| 1.2.2 | `./jira-tui -ccfe` (no key) | Same error as 1.2.1 |
| 1.2.3 | `./jira-tui nonexistent_file.csv` | Stderr: "error reading CSV: …"; exit 1 |
| 1.2.4 | `./jira-tui` with a CSV that has only a header row | Stderr: "no tickets found in CSV"; exit 1 (normal mode only) |
| 1.2.5 | `./jira-tui --create-epic header_only.csv` | App launches (epic-only creation is valid with 0 tickets) |
| 1.2.6 | `./jira-tui --show-tickets nonexistent_file.csv` | App launches (CSV is ignored in show mode) |
| 1.2.7 | `./jira-tui -ccfe proj-123` (lowercase) | Key is uppercased to `PROJ-123` internally |
| 1.2.8 | `./jira-tui --project-key "  myproj  "` (padded spaces) | Key is trimmed and uppercased to `MYPROJ` |

### 1.3  CSV parsing

| ID | Steps | Expected outcome |
|----|-------|-----------------|
| 1.3.1 | CSV with UTF-8 BOM prefix on first field | BOM stripped; ticket title correct |
| 1.3.2 | CSV row with empty Title column | Row silently skipped; not shown in ticket list |
| 1.3.3 | CSV row with more than 4 columns | Extra columns ignored; first 4 used |
| 1.3.4 | CSV row with fewer than 4 columns | Missing columns default to empty string |
| 1.3.5 | Labels column: `"FinOps;cost optimization;  "` | Produces `["FinOps","cost-optimization"]` (spaces→dashes, empty trimmed) |

---

## 2  First Run

*Before each test: ensure `~/.jira-tui/` does not exist, or use `JIRA_TUI_DIR=/tmp/jira-fresh-test ./jira-tui`.*

| ID | Steps | Expected outcome |
|----|-------|-----------------|
| 2.1 | Launch app with no existing data directory | `screenFirstRun` shown with correct directory path |
| 2.2 | On first-run screen, press `n` | App quits |
| 2.3 | On first-run screen, press `Esc` | App quits |
| 2.4 | On first-run screen, press `q` | App quits |
| 2.5 | On first-run screen, press `y` | Directory created; transitions to `screenAuth` (no saved creds) |
| 2.6 | On first-run screen, press `Enter` | Same as 2.5 |
| 2.7 | After confirming, verify `~/.jira-tui/` exists with `config` and `history.db` | Both files present; `config` mode 0600 |
| 2.8 | Run `./jira-tui --show-tickets` on first run, press `y` | Transitions to history screen (no auth needed) |
| 2.9 | Run with valid saved credentials on first run, press `y` | Transitions directly to `screenVerify` (skips auth form) |
| 2.10 | Override path: `JIRA_TUI_DIR=/tmp/mytest ./jira-tui` | First-run screen NOT shown; uses `/tmp/mytest/` as data dir |

---

## 3  Authentication (`screenAuth`)

### 3.1  Field navigation

| ID | Steps | Expected outcome |
|----|-------|-----------------|
| 3.1.1 | Press `Tab` on Site URL field | Focus moves to Email field |
| 3.1.2 | Press `Tab` on API Token field | Focus wraps to Site URL field |
| 3.1.3 | Press `Shift+Tab` on Site URL field | Focus wraps to API Token field |
| 3.1.4 | Press `Down` | Same as Tab |
| 3.1.5 | Press `Up` | Same as Shift+Tab |
| 3.1.6 | Press `Enter` on Site URL | Advances to Email field |
| 3.1.7 | Press `Enter` on Email | Advances to API Token field |
| 3.1.8 | API Token field displays typed characters as `•` (hidden) | Characters masked |

### 3.2  Submission

| ID | Steps | Expected outcome |
|----|-------|-----------------|
| 3.2.1 | Fill all 3 fields, press `Enter` on API Token | Transitions to `screenVerify` spinner |
| 3.2.2 | Leave Site URL empty, press `Enter` on API Token | Error: "all three fields are required"; stays on auth |
| 3.2.3 | Leave Email empty, press `Enter` | Same error |
| 3.2.4 | Leave API Token empty, press `Enter` | Same error |
| 3.2.5 | Enter URL with trailing slash (e.g. `https://org.atlassian.net/`) | Slash stripped before submission |
| 3.2.6 | Submit with invalid credentials | Returns to `screenAuth` with error "authentication failed — check URL, email, and token" |
| 3.2.7 | Submit with valid credentials | Credentials saved to config; advances past auth |

### 3.3  Post-auth routing

| ID | Steps | Expected outcome |
|----|-------|-----------------|
| 3.3.1 | Valid auth, no `--project-key` flag, normal mode | Goes to `screenBoards` |
| 3.3.2 | Valid auth, `--project-key PROJ`, normal mode | Goes directly to `screenSettings` (skips board picker) |
| 3.3.3 | Valid auth, `--project-key PROJ`, `--create-ticket` | Goes directly to `screenManualEntry` |
| 3.3.4 | Valid auth, `--create-csv-from-epic EPIC-1` | Goes directly to `screenEpicCSVQuery` spinner |
| 3.3.5 | Launch with pre-saved valid credentials | Skips `screenAuth` entirely; goes to `screenVerify` automatically |
| 3.3.6 | Launch with pre-saved invalid credentials | `screenVerify` spins, then falls back to `screenAuth` with error |

---

## 4  Board Selection (`screenBoards`)

### 4.1  List navigation

| ID | Steps | Expected outcome |
|----|-------|-----------------|
| 4.1.1 | Press `↓` / `j` | Cursor moves down one board |
| 4.1.2 | Press `↑` / `k` | Cursor moves up one board |
| 4.1.3 | Press `PgDn` / `Ctrl+F` | Cursor jumps one page down; list scrolls |
| 4.1.4 | Press `PgUp` / `Ctrl+B` | Cursor jumps one page up |
| 4.1.5 | Scroll past the last board | Cursor stops at last board |
| 4.1.6 | Scroll above the first board | Cursor stops at first board |
| 4.1.7 | Press `Enter` on a board | Transitions to `screenSettings` with that project key |
| 4.1.8 | Scroll hints appear when list overflows | "↑ more above" / "↓ more below" displayed correctly |

### 4.2  Search

| ID | Steps | Expected outcome |
|----|-------|-----------------|
| 4.2.1 | Type board name substring | List filters to matching boards |
| 4.2.2 | Type project key substring | List filters to matching boards |
| 4.2.3 | Type (case-insensitive) | Matches regardless of case |
| 4.2.4 | Type a string with no matches | "No boards match your search" shown |
| 4.2.5 | Type then backspace to empty | Full board list restored; cursor/offset reset to 0 |

### 4.3  Manual key entry

| ID | Steps | Expected outcome |
|----|-------|-----------------|
| 4.3.1 | Press `m` when search is empty | Manual key input sub-mode activated |
| 4.3.2 | Press `Ctrl+M` when search is empty | Same as 4.3.1 |
| 4.3.3 | Type `m` when search is non-empty | `m` typed into search box (not manual mode) |
| 4.3.4 | In manual mode: type `myproj`, press `Enter` | Project key set to `MYPROJ`; → `screenSettings` |
| 4.3.5 | In manual mode: press `Enter` on empty input | No-op; stays in manual mode |
| 4.3.6 | In manual mode: press `Esc` | Returns to list mode; search box refocused |

### 4.4  Board cache

| ID | Steps | Expected outcome |
|----|-------|-----------------|
| 4.4.1 | First launch (no cache): boards load with spinner | Boards shown after load; cache written |
| 4.4.2 | Second launch (cache fresh, < 24 h old): boards shown immediately | List visible at once; background sync badge shown |
| 4.4.3 | During background sync: navigate the board list | Navigation works without waiting for sync |
| 4.4.4 | After background sync: list updates if boards changed | List refreshes; cursor clamped if needed |
| 4.4.5 | Press `r` with empty search box | Forces background sync (spinner badge appears) |
| 4.4.6 | Press `r` with search text in box | `r` typed into search field (not a refresh) |
| 4.4.7 | Launch with stale cache (> 24 h old) | Foreground reload (spinner) rather than instant display |

### 4.5  Error / edge cases

| ID | Steps | Expected outcome |
|----|-------|-----------------|
| 4.5.1 | Jira returns zero boards | "No boards found on this site" shown |
| 4.5.2 | Board load API error (e.g. network down) | Error message shown inline in board panel |

---

## 5  Settings (`screenSettings`)

| ID | Steps | Expected outcome |
|----|-------|-----------------|
| 5.1 | Press `↓` / `j` | Cursor moves to Issue Type row |
| 5.2 | Press `↑` / `k` | Cursor moves to Assignee Fallback row |
| 5.3 | On Assignee Fallback row, press `Space` | Toggles between "unassigned" and "requester" |
| 5.4 | On Assignee Fallback row, press `→` / `l` | Toggles to "requester" |
| 5.5 | On Assignee Fallback row, press `←` / `h` | Forces "unassigned" |
| 5.6 | "requester" label shows the authenticated display name | Correct user name shown |
| 5.7 | On Issue Type row, press `→` / `l` | Cycles forward: Task→Story→Bug→Subtask→Epic→Task |
| 5.8 | On Issue Type row, press `←` / `h` | Cycles backward |
| 5.9 | On Issue Type row, press `Space` | Cycles forward (same as `→`) |
| 5.10 | On Issue Type row, press `↑` / `↓` | Cycles backward / forward |
| 5.11 | In normal mode, press `Enter` | Advances to `screenTickets` |
| 5.12 | In `--create-epic` mode, press `Enter` | Advances to `screenEpicSetup` |
| 5.13 | Press `Esc` | Returns to `screenBoards` |
| 5.14 | Settings saved to config | After advancing past Tickets screen, `~/.jira-tui/config` updated with assignee fallback and issue type |

---

## 6  Ticket List (`screenTickets`)

| ID | Steps | Expected outcome |
|----|-------|-----------------|
| 6.1 | All tickets pre-selected by default | Every checkbox shows `☑` |
| 6.2 | Press `↓` / `j` | Cursor moves down; right panel preview updates |
| 6.3 | Press `↑` / `k` | Cursor moves up |
| 6.4 | Press `Space` on a ticket | Toggles selection (`☑` ↔ `☐`) |
| 6.5 | Press `a` when all selected | Deselects all |
| 6.6 | Press `a` when any unselected | Selects all |
| 6.7 | Preview panel shows title, description (word-wrapped) | Text wraps correctly to panel width |
| 6.8 | Preview panel: ticket with no assignee | Assignee section not rendered |
| 6.9 | Preview panel: ticket with no labels | Labels section not rendered |
| 6.10 | Preview panel: ticket with assignee and labels | Both sections shown correctly |
| 6.11 | Press `Enter` with 0 selected (normal mode) | No-op; stays on ticket list |
| 6.12 | Press `Enter` with 0 selected (epic mode) | Advances (epic-only creation is valid) |
| 6.13 | Press `Enter` with tickets selected | Dup check runs; advances to `screenDupCheck` or `screenCreating` |
| 6.14 | Press `Esc` | Returns to `screenSettings` |
| 6.15 | Header in epic mode: "Subtasks for: <epic title>" | Title shown (truncated to 28 chars if long) |

---

## 7  Epic Setup (`screenEpicSetup`) — `--create-epic` only

### 7.1  Field navigation

| ID | Steps | Expected outcome |
|----|-------|-----------------|
| 7.1.1 | Press `Tab` on Title field | Focus moves to Description (textarea) |
| 7.1.2 | Press `Tab` on Requester field | Focus wraps back to Title |
| 7.1.3 | Press `Shift+Tab` on Title | Focus wraps to Requester |
| 7.1.4 | Press `↓` on Title field | Advances to Description |
| 7.1.5 | Press `↑` on Requester field | Moves back to Description |
| 7.1.6 | Press `↑` / `↓` when Description focused | Moves cursor within textarea (not field navigation) |
| 7.1.7 | Press `Enter` when Description focused | Inserts newline in textarea |
| 7.1.8 | Press `Enter` on Title | Advances to Description |
| 7.1.9 | Press `Enter` on Requester, title filled | Submits epic form |
| 7.1.10 | Press `Enter` on Requester, title empty | Error: "epic title is required" |
| 7.1.11 | Press `Esc` on any field | Returns to `screenSettings` |

### 7.2  Submission behavior

| ID | Steps | Expected outcome |
|----|-------|-----------------|
| 7.2.1 | Submit with unique epic title (not in history) | Goes to `screenTickets` |
| 7.2.2 | Submit with title matching a previously created epic | `screenEpicDupWarn` shown with existing epic details |
| 7.2.3 | Submit with title, no description, no requester | Accepted; description will be empty in Jira |
| 7.2.4 | Submit with requester "alice@example.com" | Jira ticket description will start with "Requester: alice@example.com" |
| 7.2.5 | Submit with description and requester | "Requester: …\n\n<description>" in Jira |

---

## 8  Epic Duplicate Warning (`screenEpicDupWarn`)

| ID | Steps | Expected outcome |
|----|-------|-----------------|
| 8.1 | Warning screen shows existing epic details | Key, title (truncated), creation date displayed |
| 8.2 | Press `y` | Proceeds to `screenTickets` (creates new epic anyway) |
| 8.3 | Press `Y` | Same as 8.2 |
| 8.4 | Press `n` | Returns to `screenEpicSetup` |
| 8.5 | Press `N` | Same as 8.4 |
| 8.6 | Press `Esc` | Returns to `screenEpicSetup` |

---

## 9  Duplicate Check (`screenDupCheck`)

| ID | Steps | Expected outcome |
|----|-------|-----------------|
| 9.1 | Create a ticket, then immediately re-run with same CSV | Duplicate check fires and shows the matching ticket |
| 9.2 | On dup check screen: default state per item | All items show `[SKIP]` (skip is default) |
| 9.3 | Press `Space` or `c` on an item | Toggles to `[CREATE]` and back |
| 9.4 | Press `s` on an item set to CREATE | Forces back to `[SKIP]` |
| 9.5 | Press `↓` / `↑` / `j` / `k` | Navigates between duplicate items |
| 9.6 | Press `Enter` with all items set to SKIP | All marked tickets deselected; creation proceeds (or Done if nothing left) |
| 9.7 | Press `Enter` with some items set to CREATE | Those items created; skipped items deselected |
| 9.8 | Press `Esc` | Returns to `screenTickets` |
| 9.9 | Duplicate shown includes Jira key and creation date | Format: "existing: KEY-123  created 2024-01-15" |

---

## 10  Ticket Creation (`screenCreating`)

| ID | Steps | Expected outcome |
|----|-------|-----------------|
| 10.1 | Creation progress: rows update in real time | Each ticket shows spinner→`✓ KEY` or `✗ failed` |
| 10.2 | Progress bar fills as tickets complete | Bar width matches terminal width correctly |
| 10.3 | Assignee field in CSV cannot be resolved | `✓ KEY !assignee` shown in red; ticket still created |
| 10.4 | API error for one ticket | `✗ failed` shown; remaining tickets continue |
| 10.5 | Press `Esc` during creation | Footer changes to "Aborting…"; in-flight ticket completes; no further tickets started |
| 10.6 | Press `q` during creation | Same as `Esc` abort |
| 10.7 | After abort: Done screen shows partial results | Created tickets listed; unstarted tickets absent |
| 10.8 | Epic mode: epic row appears first in list | Epic shows spinner then `✓ EPIC-KEY` |
| 10.9 | Epic mode: epic failure | Error shown; returns to `screenEpicSetup` |
| 10.10 | Epic mode: abort before epic created | Epic still completes; no children started |
| 10.11 | After creation: tickets visible in `--show-tickets` | History DB populated with created tickets |

---

## 11  Results Screen (`screenDone`)

| ID | Steps | Expected outcome |
|----|-------|-----------------|
| 11.1 | Summary line shows correct created/failed counts | "Created: N   Failed: M" |
| 11.2 | Failed count appears in red when > 0 | Red text for non-zero failures |
| 11.3 | Press `↓` / `j` | Cursor moves down result list |
| 11.4 | Press `↑` / `k` | Cursor moves up |
| 11.5 | Press `PgDn` / `Ctrl+F` | Cursor jumps down one page |
| 11.6 | Press `PgUp` / `Ctrl+B` | Cursor jumps up |
| 11.7 | Press `o` on a successful ticket row | Browser opens ticket URL |
| 11.8 | Press `o` on a failed ticket row | No crash (graceful no-op or no visible browser) |
| 11.9 | Epic mode: first row is the epic | Epic URL shown; `o` opens epic |
| 11.10 | Press `e` | `jira_tickets_results.csv` created in CWD; columns: Status, Key, Title, URL, Error |
| 11.11 | Press `e` twice | File overwritten (not appended) |
| 11.12 | Press `r` with failed tickets | Failed tickets retried; successful ones deselected |
| 11.13 | Press `r` with no failures | No-op |
| 11.14 | Press `r` in `--create-ticket` mode | No-op (retry not supported) |
| 11.15 | Press `q` | App quits |
| 11.16 | Press `Esc` | App quits |
| 11.17 | Press `Enter` | App quits |
| 11.18 | List longer than screen height | Scroll hints shown; content scrollable |

---

## 12  Interactive Ticket Form (`--create-ticket`)

### 12.1  Field navigation

| ID | Steps | Expected outcome |
|----|-------|-----------------|
| 12.1.1 | Press `Tab` | Focus advances: Title→Desc→Assignee→Labels→IssueType→Title |
| 12.1.2 | Press `Shift+Tab` | Focus retreats in reverse |
| 12.1.3 | Press `Enter` on Title field | Advances to Description |
| 12.1.4 | Press `Enter` on Description | Inserts newline (does not advance) |
| 12.1.5 | Press `Enter` on Assignee | Advances to Labels |
| 12.1.6 | Press `Enter` on Labels | Advances to Issue Type |
| 12.1.7 | Press `Enter` on Issue Type, title filled | Submits ticket |
| 12.1.8 | Press `Enter` on Issue Type, title empty | Error: "title is required" |
| 12.1.9 | Press `↑` / `↓` when Issue Type focused | Cycles issue type |
| 12.1.10 | Press `←` / `→` when Issue Type focused | Cycles issue type |
| 12.1.11 | Press `Ctrl+S` from any field | Submits (if title filled) |
| 12.1.12 | Form shows "child of KEY" header when creating subtask | Subtitle shows parent key |

### 12.2  Submission and state

| ID | Steps | Expected outcome |
|----|-------|-----------------|
| 12.2.1 | Submit Task type | Spinner; then Done screen |
| 12.2.2 | Submit Epic type | Spinner; then "Add a subtask?" screen |
| 12.2.3 | On "Add subtask" screen, press `y` | Form resets; header shows "child of <epic key>" |
| 12.2.4 | On "Add subtask" screen, press `Enter` | Same as `y` |
| 12.2.5 | On "Add subtask" screen, press `n` | Done screen showing all created tickets |
| 12.2.6 | On "Add subtask" screen, press `Esc` | Same as `n` |
| 12.2.7 | Create multiple subtasks then press `Esc` on form | Done screen shows epic + all created subtasks |
| 12.2.8 | Submit with API error | Error shown inline; form stays; user can retry |
| 12.2.9 | Press `Esc` with tickets already created | Done screen (not back to boards) |
| 12.2.10 | Press `Esc` with no tickets created | Returns to `screenBoards` |
| 12.2.11 | During API call (`manualCreating=true`): all keys ignored | Spinner shown; no input accepted |

### 12.3  Assignee resolution

| ID | Steps | Expected outcome |
|----|-------|-----------------|
| 12.3.1 | Enter valid Jira email as assignee | Ticket assigned in Jira |
| 12.3.2 | Enter valid Jira display name as assignee | Ticket assigned in Jira |
| 12.3.3 | Enter an unresolvable assignee string | Ticket created unassigned; `!assignee` warning shown |
| 12.3.4 | Leave assignee blank | Ticket created unassigned (or requester if fallback=requester) |

---

## 13  Ticket History (`--show-tickets`)

| ID | Steps | Expected outcome |
|----|-------|-----------------|
| 13.1 | No tickets in history | "No tickets have been created yet." message; `q`/`Esc` to quit |
| 13.2 | Press `↓` / `j` | Cursor moves down |
| 13.3 | Press `↑` / `k` | Cursor moves up |
| 13.4 | Press `PgDn` / `Ctrl+F` | Page jump down |
| 13.5 | Press `PgUp` / `Ctrl+B` | Page jump up |
| 13.6 | Scroll hints appear when list overflows | "↑ more above" / "↓ more below" visible |
| 13.7 | Records show: Jira key, type, parent (if any), title, date | All columns rendered |
| 13.8 | Epic children show `↳ PARENT-KEY` prefix | Parent key indicator displayed |
| 13.9 | Press `o` on a record with a URL | Browser opens the Jira ticket URL |
| 13.10 | Press `d` on a record | Footer shows delete confirmation with key and title |
| 13.11 | Confirmation prompt: press `y` | Record deleted from DB and removed from list |
| 13.12 | Confirmation prompt: press any other key | Deletion cancelled; returns to normal navigation |
| 13.13 | Delete last item in list | List becomes empty; displays empty state |
| 13.14 | Delete middle item | Cursor clamped correctly; list compacts |
| 13.15 | Press `q` / `Esc` / `Enter` | App quits |

---

## 14  Epic → CSV Export (`--create-csv-from-epic`)

### 14.1  Query and validation

| ID | Steps | Expected outcome |
|----|-------|-----------------|
| 14.1.1 | `./jira-tui -ccfe EPIC-123` (valid epic key) | Spinner shown: "Querying epic EPIC-123…" |
| 14.1.2 | `./jira-tui -ccfe TASK-123` (not an epic) | Error: "TASK-123 is not an Epic (issue type: Task)" |
| 14.1.3 | `./jira-tui -ccfe NOTEXIST-999` (invalid key) | Error: "fetching NOTEXIST-999: HTTP 404: …" |
| 14.1.4 | Valid epic with 0 child issues | Review screen shows "0 child issues found" + explanation of JQL methods |
| 14.1.5 | Valid epic on a team-managed project | Children fetched via `parent =` JQL |
| 14.1.6 | Valid epic on a classic project (if applicable) | Children fetched via `"Epic Link" =` fallback |
| 14.1.7 | Query error: on error screen, press `q` | App quits |
| 14.1.8 | Query error: on error screen, press `Esc` | App quits |
| 14.1.9 | Query error: on error screen, press `Enter` | App quits |

### 14.2  Review screen

| ID | Steps | Expected outcome |
|----|-------|-----------------|
| 14.2.1 | Epic header shows: key, EPIC tag, summary | Header rendered correctly |
| 14.2.2 | Child count line correct | "N child issue(s) found" |
| 14.2.3 | Each child row shows: type tag, summary, assignee | All three visible |
| 14.2.4 | Assignee shows email (preferred) or display name | Email used when available |
| 14.2.5 | Press `↓` / `j` | Cursor moves down |
| 14.2.6 | Press `↑` / `k` | Cursor moves up |
| 14.2.7 | Press `PgDn` / `Ctrl+F` | Page jump down |
| 14.2.8 | Press `PgUp` / `Ctrl+B` | Page jump up |
| 14.2.9 | Scroll hints shown when list overflows | "↑ more above" / "↓ more below" visible |
| 14.2.10 | Footer shows default filename (e.g. `"EPIC-123.csv"`) | Filename in footer matches key |
| 14.2.11 | Press `Enter` | Transitions to save-path screen; path input pre-filled with `EPIC-123.csv` |
| 14.2.12 | Press `q` | App quits |
| 14.2.13 | Press `Esc` | App quits |

### 14.3  Save screen

| ID | Steps | Expected outcome |
|----|-------|-----------------|
| 14.3.1 | Save path input pre-filled with `<KEY>.csv` | Default filename visible |
| 14.3.2 | Press `Enter` with default path | CSV written to `<KEY>.csv` in CWD |
| 14.3.3 | Clear path, type custom path, press `Enter` | CSV written to custom path |
| 14.3.4 | Clear path to empty, press `Enter` | Default `<KEY>.csv` used |
| 14.3.5 | Type path to a non-existent directory (e.g. `/nodir/out.csv`) | Error shown inline; can retry with valid path |
| 14.3.6 | After successful save: success message shown | "✓ <path>" in green; child count shown |
| 14.3.7 | After save: press `Enter` | App quits |
| 14.3.8 | After save: press `q` | App quits |
| 14.3.9 | After save: press `Esc` | App quits |
| 14.3.10 | Press `Esc` before saving | Returns to review screen |

### 14.4  CSV output verification

| ID | Steps | Expected outcome |
|----|-------|-----------------|
| 14.4.1 | Open saved CSV; check header row | `Title,Description,Assignee,Labels,Requester` |
| 14.4.2 | Assignee column: assigned ticket | Shows email address (or display name if no email) |
| 14.4.3 | Assignee column: unassigned ticket | Empty string |
| 14.4.4 | Requester (reporter) column present | Reporter email or display name |
| 14.4.5 | Labels column: ticket with labels | Labels semicolon-separated |
| 14.4.6 | Labels column: ticket with no labels | Empty string |
| 14.4.7 | Description: plain text | Text extracted correctly |
| 14.4.8 | Description: ADF document (next-gen project) | Plain text extracted; no JSON artifacts in CSV |
| 14.4.9 | Description: null / missing | Empty string |
| 14.4.10 | Exported CSV re-imported as `./jira-tui <exported.csv>` | Parses successfully; tickets appear in list |
| 14.4.11 | Epic with > 50 children | All children exported (pagination works) |

---

## 15  Configuration and Environment Variables

### 15.1  Config file

| ID | Steps | Expected outcome |
|----|-------|-----------------|
| 15.1.1 | After first successful auth, inspect `~/.jira-tui/config` | Contains `JIRA_BASE_URL`, `JIRA_EMAIL`, `JIRA_API_TOKEN`; file mode 0600 |
| 15.1.2 | After changing Settings, check config | `JIRA_ASSIGNEE_FALLBACK_MODE` and `JIRA_ISSUE_TYPE` updated |
| 15.1.3 | Manually edit config, re-launch | Updated values loaded |
| 15.1.4 | Config file has `#` comment lines | Lines preserved on save; ignored on parse |
| 15.1.5 | Config file has unknown `CUSTOM_KEY="val"` | Unknown key preserved on next save |
| 15.1.6 | Legacy `~/.jira_config` present, no new dir | Legacy file read as fallback |

### 15.2  Environment variable overrides

| ID | Steps | Expected outcome |
|----|-------|-----------------|
| 15.2.1 | `JIRA_BASE_URL=https://other.atlassian.net ./jira-tui` | Env var URL takes precedence over config |
| 15.2.2 | `JIRA_EMAIL=other@example.com ./jira-tui` | Env var email takes precedence |
| 15.2.3 | `JIRA_API_TOKEN=newtoken ./jira-tui` | Env var token takes precedence |
| 15.2.4 | `JIRA_BOARD_CACHE_TTL_HOURS=1 ./jira-tui` | Cache expires after 1 hour (not 24) |
| 15.2.5 | `JIRA_USE_ADF=true ./jira-tui` | Descriptions sent as ADF via REST v3 |
| 15.2.6 | `JIRA_USE_ADF=1 ./jira-tui` | Same as 15.2.5 |
| 15.2.7 | `JIRA_TUI_DIR=/tmp/testdata ./jira-tui` | Data dir is `/tmp/testdata/`; first-run screen suppressed |

### 15.3  ADF mode (`JIRA_USE_ADF=true`)

| ID | Steps | Expected outcome |
|----|-------|-----------------|
| 15.3.1 | Create a ticket with multi-paragraph description | Description renders as paragraphs in Jira |
| 15.3.2 | Create a ticket with single-newline description | Single newlines become hard breaks in Jira |
| 15.3.3 | Create an epic with ADF enabled | Epic description formatted correctly in Jira |
| 15.3.4 | Empty description with ADF enabled | Ticket created with empty ADF paragraph; no error |

---

## 16  Global / Cross-Cutting Behaviors

### 16.1  Ctrl+C

| ID | Steps | Expected outcome |
|----|-------|-----------------|
| 16.1.1 | `Ctrl+C` on any screen | App quits immediately |
| 16.1.2 | `Ctrl+C` during `screenCreating` | App exits without waiting for in-flight API call |

### 16.2  Window resize

| ID | Steps | Expected outcome |
|----|-------|-----------------|
| 16.2.1 | Resize terminal window while on `screenBoards` | Board list reflows; page size adjusts |
| 16.2.2 | Resize while on `screenTickets` | Both panels resize; no content cut off |
| 16.2.3 | Resize while on `screenCreating` | Progress bar resizes to new width |
| 16.2.4 | Resize while on `screenDone` | Result list page size adjusts; scroll still works |
| 16.2.5 | Resize to very small terminal (< 40 cols) | App does not crash; content may overlap |

### 16.3  Assignee fallback modes

| ID | Steps | Expected outcome |
|----|-------|-----------------|
| 16.3.1 | Fallback = "unassigned"; CSV row has no assignee | Ticket created with no assignee in Jira |
| 16.3.2 | Fallback = "requester"; CSV row has no assignee | Ticket assigned to the authenticated user in Jira |
| 16.3.3 | Fallback = "requester"; CSV row has valid assignee | Assigned to the CSV assignee (not the requester) |
| 16.3.4 | Fallback = "requester"; CSV assignee unresolvable | Falls through to requester; `!assignee` warning shown |

### 16.4  URL opening (press `o`)

| ID | Steps | Expected outcome |
|----|-------|-----------------|
| 16.4.1 | Press `o` on Done screen (macOS) | `open <url>` executed; browser opens ticket |
| 16.4.2 | Press `o` on History screen (macOS) | Same |
| 16.4.3 | Press `o` on a row with empty URL | No crash; browser may or may not open |

### 16.5  Database integrity

| ID | Steps | Expected outcome |
|----|-------|-----------------|
| 16.5.1 | Create tickets, close app, re-open with `--show-tickets` | All previously created tickets present |
| 16.5.2 | Duplicate detection across sessions | Tickets created in prior session detected as duplicates in new session |
| 16.5.3 | Delete a ticket in history, restart | Deleted ticket not present |

---

## 17  Negative / Edge Cases

| ID | Steps | Expected outcome |
|----|-------|-----------------|
| 17.1 | Network disconnected after auth, during board load | Board load error shown inline |
| 17.2 | Network disconnected during ticket creation | Affected ticket shows `✗ failed`; creation continues for others |
| 17.3 | Jira API rate limiting (HTTP 429) | Ticket shows `✗ failed` with HTTP 429 error |
| 17.4 | Token expired mid-session | API calls fail; errors shown per-ticket on creation screen |
| 17.5 | CSV with only one ticket, that one is a duplicate and skipped | Zero tickets created; Done screen shows "Created: 0" |
| 17.6 | Run `--create-epic` with no tickets selected at ticket list | Epic created; Done screen shows epic only |
| 17.7 | Very long ticket title (> terminal width) | Title truncated with `…` in all list views |
| 17.8 | Very long description in CSV | Preview wraps correctly; no crash |
| 17.9 | Labels with spaces: `"cost optimization"` in CSV | Converted to `cost-optimization` in Jira |
| 17.10 | Epic CSV export: `Esc` on save screen, then re-enter via `Enter` on review | State reset correctly; save error cleared |
| 17.11 | `--create-csv-from-epic` with epic that has > 100 children | All children paginated and exported |
| 17.12 | Simultaneous launch of two instances pointing at same DB | SQLite busy-timeout (5 s) prevents corruption; one instance waits |
