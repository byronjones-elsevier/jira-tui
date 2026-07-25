# VHS_Testing.tape
#
# Drives jira-tui through its key interactive flows.
# Run via:  ./run_vhs_tests.sh
#
# Required environment (set by run_vhs_tests.sh, or export manually):
#   JIRA_TEST_DIR       – pre-populated data directory (config + history.db)
#   JIRA_TEST_CSV       – path to the test CSV file created by the script
#   TEST_PROJECT_KEY    – a real Jira project key (e.g. "PROJ")
#   TEST_EPIC_KEY       – a real Jira Epic key (e.g. "PROJ-1")
#
# Timing note: Sleep values are conservative.  If your Jira API is fast you
# can lower them; if it is slow or on VPN, increase them.

Output vhs-output/jira-tui-tests.gif

Set Shell      bash
Set Width      200
Set Height     52
Set FontSize   13
Set Framerate  10
Set Theme      "Dracula"

# ---------------------------------------------------------------------------
# GLOBAL SETUP
# Define th() — the test-header function used throughout this tape.
# th 'ID' 'Description' renders a gum-styled box with the MANUAL_TESTING.md
# hierarchy ID and its description before each test step.
# ---------------------------------------------------------------------------
Hide
Type "export JIRA_TUI_DIR=\"$JIRA_TEST_DIR\""
Enter
Type "export TEST_CSV=\"$JIRA_TEST_CSV\""
Enter
Type "th() { gum style --border rounded --border-foreground 99 --foreground 15 --bold --padding '0 2' --margin '1 0' \"§ $1\" \"$2\"; }"
Enter
Type "clear"
Enter
Show

# ===========================================================================
# SECTION 1.1  Help flags
# ===========================================================================

Hide
Type "th '1.1.1' 'Help flag: --help exits 0 and prints usage'"
Enter
Sleep 0.5s
Show
Type "./jira-tui --help"
Enter
Sleep 2s
Screenshot vhs-output/1.1.1-help-long.png

Hide
Type "th '1.1.2' 'Help flag: -h (abbreviated) exits 0 and prints usage'"
Enter
Sleep 0.5s
Show
Type "./jira-tui -h 2>&1 | head -8"
Enter
Sleep 1s
Screenshot vhs-output/1.1.2-help-short.png

Hide
Type "th '1.1.6' 'Help text documents all flags: -ce -ct -st -ccfe -pk -h'"
Enter
Sleep 0.5s
Show
Type "./jira-tui --help 2>&1 | grep -E 'create-epic|create-ticket|show-tickets|create-csv|project-key'"
Enter
Sleep 1s
Screenshot vhs-output/1.1.6-flags-documented.png

Hide
Type "th '1.1.7' 'Help text contains CSV export column description'"
Enter
Sleep 0.5s
Show
Type "./jira-tui --help 2>&1 | grep -A2 'CSV'"
Enter
Sleep 1s
Screenshot vhs-output/1.1.7-csv-section.png

# ===========================================================================
# SECTION 1.2  Input validation (exit immediately, no TUI launched)
# ===========================================================================

Hide
Type "clear"
Enter
Type "th '1.2.1' 'Validation: -ccfe with no key -> stderr error, exit 1'"
Enter
Sleep 0.5s
Show
Type "./jira-tui -ccfe 2>&1; echo \"Exit: $?\""
Enter
Sleep 1s
Screenshot vhs-output/1.2.1-missing-epic-key.png

Hide
Type "th '1.2.2' 'Validation: --create-csv-from-epic with no key -> exit 1'"
Enter
Sleep 0.5s
Show
Type "./jira-tui --create-csv-from-epic 2>&1; echo \"Exit: $?\""
Enter
Sleep 1s
Screenshot vhs-output/1.2.2-missing-epic-key-long.png

Hide
Type "th '1.2.3' 'Validation: nonexistent CSV path -> stderr error, exit 1'"
Enter
Sleep 0.5s
Show
Type "./jira-tui /tmp/no_such_file_xyz.csv 2>&1; echo \"Exit: $?\""
Enter
Sleep 1s
Screenshot vhs-output/1.2.3-missing-csv.png

Hide
Type "th '1.2.7' 'Validation: lowercase epic key is uppercased internally'"
Enter
Sleep 0.5s
Show
Type "./jira-tui -ccfe proj-1 2>&1 | head -3"
Enter
Sleep 1s
Screenshot vhs-output/1.2.7-key-uppercased.png

Hide
Type "clear"
Enter
Show

# ===========================================================================
# SECTION 2  Normal mode — CSV ticket creation
# Covers: 3.3.5 (saved creds), 5.x (settings), 6.x (ticket list),
#         9.x (dup check), 10.x (creation), 11.x (done screen)
#
# --project-key skips board selection for a deterministic flow.
# Credentials pre-saved in JIRA_TEST_DIR/config, so auth is automatic.
# ===========================================================================

Hide
Type "th 'SECTION 2' 'Normal mode: CSV ticket creation end-to-end flow'"
Enter
Sleep 0.5s
Show
Type "./jira-tui --project-key $TEST_PROJECT_KEY \"$TEST_CSV\""
Enter

Hide
Type "th '3.3.5' 'Saved credentials: screenVerify spinner -> skips screenAuth'"
Enter
Sleep 0.5s
Show
Sleep 6s
Screenshot vhs-output/2-settings-initial.png

# --- Settings screen ---

Hide
Type "th '5.1' 'Settings: Down arrow moves cursor to Issue Type row'"
Enter
Sleep 0.5s
Show
Down
Sleep 0.3s

Hide
Type "th '5.7' 'Settings: Right arrow cycles Issue Type forward (Task -> Story)'"
Enter
Sleep 0.5s
Show
Right
Sleep 0.3s
Screenshot vhs-output/2-settings-story-selected.png

Hide
Type "th '5.8' 'Settings: Left arrow cycles Issue Type backward (Story -> Task)'"
Enter
Sleep 0.5s
Show
Left
Sleep 0.3s

Hide
Type "th '5.2' 'Settings: Up arrow moves cursor back to Assignee Fallback row'"
Enter
Sleep 0.5s
Show
Up
Sleep 0.3s

Hide
Type "th '5.3' 'Settings: Space toggles Assignee Fallback to requester'"
Enter
Sleep 0.5s
Show
Space
Sleep 0.3s
Screenshot vhs-output/2-settings-requester.png

Hide
Type "th '5.3' 'Settings: Space toggles Assignee Fallback back to unassigned'"
Enter
Sleep 0.5s
Show
Space
Sleep 0.3s

Hide
Type "th '5.11' 'Settings: Enter confirms and advances to ticket list'"
Enter
Sleep 0.5s
Show
Enter
Sleep 1s

# --- Ticket list screen ---

Hide
Type "th '6.1' 'Ticket list: all tickets pre-selected (checkboxes show checked)'"
Enter
Sleep 0.5s
Show
Screenshot vhs-output/2-ticket-list.png

Hide
Type "th '6.2' 'Ticket list: Down arrow moves cursor down; preview panel updates'"
Enter
Sleep 0.5s
Show
Down
Sleep 0.3s
Down
Sleep 0.3s

Hide
Type "th '6.3' 'Ticket list: Up arrow moves cursor up'"
Enter
Sleep 0.5s
Show
Up
Sleep 0.3s
Screenshot vhs-output/2-ticket-nav.png

Hide
Type "th '6.5' 'Ticket list: a deselects all when all are currently selected'"
Enter
Sleep 0.5s
Show
Type "a"
Sleep 0.3s
Screenshot vhs-output/2-all-deselected.png

Hide
Type "th '6.6' 'Ticket list: a selects all when any are currently unselected'"
Enter
Sleep 0.5s
Show
Type "a"
Sleep 0.3s
Screenshot vhs-output/2-all-selected.png

Hide
Type "th '6.4' 'Ticket list: Space toggles selection of current row'"
Enter
Sleep 0.5s
Show
Space
Sleep 0.3s
Screenshot vhs-output/2-first-deselected.png

Space
Sleep 0.3s

Hide
Type "th '6.13' 'Ticket list: Enter starts creation (may show dup check first)'"
Enter
Sleep 0.5s
Show
Enter
Sleep 2s
Screenshot vhs-output/2-creating-or-dupcheck.png

# --- Dup check (fires on repeated runs) ---

Hide
Type "th '9.2' 'Dup check: default state is SKIP; Enter proceeds skipping all dups'"
Enter
Sleep 0.5s
Show
Enter
Sleep 2s

# --- Creation screen ---

Hide
Type "th '10.1' 'Creating: each row updates in real time (spinner -> checkmark/X)'"
Enter
Sleep 0.5s
Show
Screenshot vhs-output/2-creating-progress.png

Hide
Type "th '10.2' 'Creating: progress bar fills as tickets complete'"
Enter
Sleep 0.5s
Show
Sleep 12s

# --- Done screen ---

Hide
Type "th '11.1' 'Done: summary line shows correct created/failed counts'"
Enter
Sleep 0.5s
Show
Screenshot vhs-output/2-done-initial.png

Hide
Type "th '11.3' 'Done: Down arrow moves cursor down the result list'"
Enter
Sleep 0.5s
Show
Down
Sleep 0.3s

Hide
Type "th '11.4' 'Done: Up arrow moves cursor up the result list'"
Enter
Sleep 0.5s
Show
Down
Sleep 0.3s
Up
Sleep 0.3s
Screenshot vhs-output/2-done-navigated.png

Hide
Type "th '11.10' 'Done: e exports results to jira_tickets_results.csv'"
Enter
Sleep 0.5s
Show
Type "e"
Sleep 1s
Screenshot vhs-output/2-exported-csv.png

Hide
Type "th '11.15' 'Done: q quits the application'"
Enter
Sleep 0.5s
Show
Type "q"
Sleep 0.5s

Hide
Type "clear"
Enter
Show

# ===========================================================================
# SECTION 3  Show tickets (--show-tickets)
# Covers: 13.x  Uses the tickets created in Section 2.
# ===========================================================================

Hide
Type "th 'SECTION 3' 'History mode: --show-tickets browse and manage local records'"
Enter
Sleep 0.5s
Show
Type "./jira-tui --show-tickets"
Enter
Sleep 2s

Hide
Type "th '13.1' 'History: list of previously created tickets shown from local DB'"
Enter
Sleep 0.5s
Show
Screenshot vhs-output/3-show-tickets-initial.png

Hide
Type "th '13.2' 'History: Down arrow moves cursor down'"
Enter
Sleep 0.5s
Show
Down
Sleep 0.3s

Hide
Type "th '13.3' 'History: Up arrow moves cursor up'"
Enter
Sleep 0.5s
Show
Down
Sleep 0.3s
Up
Sleep 0.3s
Screenshot vhs-output/3-show-tickets-nav.png

Hide
Type "th '13.10' 'History: d shows delete confirmation with key and title'"
Enter
Sleep 0.5s
Show
Type "d"
Sleep 0.5s
Screenshot vhs-output/3-delete-confirm.png

Hide
Type "th '13.11' 'History: y confirms deletion; record removed from list'"
Enter
Sleep 0.5s
Show
Type "y"
Sleep 1s
Screenshot vhs-output/3-after-delete.png

Hide
Type "th '13.15' 'History: q quits the application'"
Enter
Sleep 0.5s
Show
Type "q"
Sleep 0.5s

Hide
Type "clear"
Enter
Show

# ===========================================================================
# SECTION 4  Epic → CSV export (--create-csv-from-epic)
# Covers: 14.x
# ===========================================================================

Hide
Type "th 'SECTION 4' 'Epic -> CSV export: --create-csv-from-epic flow'"
Enter
Sleep 0.5s
Show
Type "./jira-tui --create-csv-from-epic $TEST_EPIC_KEY"
Enter

Hide
Type "th '14.1.1' 'Epic query: spinner shown while fetching epic and children'"
Enter
Sleep 0.5s
Show
Sleep 6s
Screenshot vhs-output/4-epic-query-spinner.png

Sleep 8s

Hide
Type "th '14.2.1' 'Epic review: header shows key and EPIC tag with summary'"
Enter
Sleep 0.5s
Show
Screenshot vhs-output/4-epic-review-initial.png

Hide
Type "th '14.2.2' 'Epic review: child count line is correct'"
Enter
Sleep 0.5s
Show

Hide
Type "th '14.2.5' 'Epic review: Down arrow moves cursor down child list'"
Enter
Sleep 0.5s
Show
Down
Sleep 0.3s

Hide
Type "th '14.2.6' 'Epic review: Up arrow moves cursor up child list'"
Enter
Sleep 0.5s
Show
Down
Sleep 0.3s
Up
Sleep 0.3s

Hide
Type "th '14.2.7' 'Epic review: PgDn jumps cursor one page down'"
Enter
Sleep 0.5s
Show
PageDown
Sleep 0.3s

Hide
Type "th '14.2.8' 'Epic review: PgUp jumps cursor one page up'"
Enter
Sleep 0.5s
Show
PageUp
Sleep 0.3s
Screenshot vhs-output/4-epic-review-navigated.png

Hide
Type "th '14.2.11' 'Epic review: Enter proceeds to save-path input screen'"
Enter
Sleep 0.5s
Show
Enter
Sleep 0.5s

Hide
Type "th '14.3.1' 'Save path: input pre-filled with KEY.csv default filename'"
Enter
Sleep 0.5s
Show
Screenshot vhs-output/4-save-path-default.png

Hide
Type "th '14.3.2' 'Save path: Enter with default path writes CSV to CWD'"
Enter
Sleep 0.5s
Show
Enter
Sleep 1s
Screenshot vhs-output/4-save-success.png

Hide
Type "th '14.4.1' 'CSV output: verify file exists and header row is correct'"
Enter
Sleep 0.5s
Show
Type "head -1 \"${TEST_EPIC_KEY}.csv\" 2>/dev/null && echo 'file found' || echo 'CSV not found'"
Enter
Sleep 1s
Screenshot vhs-output/4-csv-header-check.png

Hide
Type "th '14.3.7' 'After save: q quits the application'"
Enter
Sleep 0.5s
Show
Type "q"
Sleep 0.5s

Hide
Type "clear"
Enter
Show

# ===========================================================================
# SECTION 5  Interactive ticket form (--create-ticket)
# Covers: 12.x
# ===========================================================================

Hide
Type "th 'SECTION 5' 'Interactive form: --create-ticket single-ticket entry mode'"
Enter
Sleep 0.5s
Show
Type "./jira-tui --create-ticket --project-key $TEST_PROJECT_KEY"
Enter

Hide
Type "th '3.3.3' 'Auth: --create-ticket with --project-key goes straight to form'"
Enter
Sleep 0.5s
Show
Sleep 6s
Screenshot vhs-output/5-form-initial.png

Hide
Type "th '12.1.1' 'Form: Tab advances focus from Title to next field'"
Enter
Sleep 0.5s
Show
Type "[VHS] Interactive form test"
Tab
Sleep 0.3s

Hide
Type "th '12.1.4' 'Form: Enter in Description inserts newline (does not advance)'"
Enter
Sleep 0.5s
Show
Type "Automated VHS test. Created by run_vhs_tests.sh."
Tab
Sleep 0.3s

Hide
Type "th '12.1.5' 'Form: Enter on Assignee advances to Labels field'"
Enter
Sleep 0.5s
Show
Tab
Sleep 0.3s

Hide
Type "th '12.1.6' 'Form: Enter on Labels advances to Issue Type field'"
Enter
Sleep 0.5s
Show
Type "vhs-test;automated"
Tab
Sleep 0.3s
Screenshot vhs-output/5-form-issue-type.png

Hide
Type "th '12.1.9' 'Form: Right/Left arrows cycle Issue Type when that field is focused'"
Enter
Sleep 0.5s
Show
Right
Sleep 0.2s
Right
Sleep 0.2s
Left
Sleep 0.2s
Left
Sleep 0.2s
Screenshot vhs-output/5-form-cycle-type.png

Hide
Type "th '12.1.11' 'Form: Ctrl+S submits from any field (title is filled)'"
Enter
Sleep 0.5s
Show
Ctrl+S
Sleep 6s

Hide
Type "th '12.2.1' 'Form: Task submission -> spinner -> Done screen'"
Enter
Sleep 0.5s
Show
Screenshot vhs-output/5-form-done.png

Hide
Type "th '11.15' 'Done: q quits the application'"
Enter
Sleep 0.5s
Show
Type "q"
Sleep 0.5s

Hide
Type "clear"
Enter
Show

# ===========================================================================
# SECTION 6  Epic mode (--create-epic)
# Covers: 7.x, 8.x
# ===========================================================================

Hide
Type "th 'SECTION 6' 'Epic creation mode: --create-epic flow with CSV subtasks'"
Enter
Sleep 0.5s
Show
Type "./jira-tui --create-epic --project-key $TEST_PROJECT_KEY \"$TEST_CSV\""
Enter

Hide
Type "th '3.3.1' 'Auth: valid saved creds + normal mode -> screenBoards (or settings)'"
Enter
Sleep 0.5s
Show
Sleep 6s
Screenshot vhs-output/6-epic-settings.png

Hide
Type "th '5.12' 'Settings: in --create-epic mode Enter advances to Epic Setup screen'"
Enter
Sleep 0.5s
Show
Enter
Sleep 1s
Screenshot vhs-output/6-epic-setup.png

Hide
Type "th '7.1.1' 'Epic setup: Title field is focused on entry'"
Enter
Sleep 0.5s
Show
Type "[VHS] Epic Test"

Hide
Type "th '7.1.2' 'Epic setup: Tab moves focus to Description textarea'"
Enter
Sleep 0.5s
Show
Tab
Sleep 0.3s
Type "VHS-generated epic for automated testing."

Hide
Type "th '7.1.3' 'Epic setup: Tab moves focus to Requester field'"
Enter
Sleep 0.5s
Show
Tab
Sleep 0.3s
Type "vhs-test-requester"
Screenshot vhs-output/6-epic-form-filled.png

Hide
Type "th '7.2.1' 'Epic setup: Enter submits; unique title -> proceeds to ticket list'"
Enter
Sleep 0.5s
Show
Enter
Sleep 2s
Screenshot vhs-output/6-epic-ticket-list.png

Hide
Type "th '6.6' 'Ticket list: a selects all tickets for creation with epic'"
Enter
Sleep 0.5s
Show
Type "a"
Sleep 0.3s

Hide
Type "th '6.13' 'Ticket list: Enter starts epic + subtask creation'"
Enter
Sleep 0.5s
Show
Enter
Sleep 2s

Hide
Type "th '9.2' 'Dup check: Enter skips all duplicates and proceeds'"
Enter
Sleep 0.5s
Show
Enter
Sleep 2s

Hide
Type "th '10.8' 'Creating: epic row appears first; spinner then checkmark'"
Enter
Sleep 0.5s
Show
Screenshot vhs-output/6-epic-creating.png
Sleep 15s

Hide
Type "th '11.9' 'Done: first row is the epic; child tickets follow'"
Enter
Sleep 0.5s
Show
Screenshot vhs-output/6-epic-done.png

Hide
Type "th '11.15' 'Done: q quits the application'"
Enter
Sleep 0.5s
Show
Type "q"
Sleep 0.5s

Hide
Type "clear"
Enter
Show

# ===========================================================================
# END
# ===========================================================================

Hide
Type "th 'COMPLETE' 'All VHS flows finished. Artifacts in vhs-output/'"
Enter
Sleep 0.5s
Show
Type "ls vhs-output/*.png | wc -l | xargs -I{} echo '{} screenshots captured'"
Enter
Sleep 1s
