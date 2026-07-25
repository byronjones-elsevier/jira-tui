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
# GLOBAL SETUP — point every jira-tui invocation at the pre-built test dir
# ---------------------------------------------------------------------------
Hide
Type "export JIRA_TUI_DIR=\"$JIRA_TEST_DIR\""
Enter
Type "export TEST_CSV=\"$JIRA_TEST_CSV\""
Enter
Type "clear"
Enter
Show

# ===========================================================================
# SECTION 1.1  Help flags
# ===========================================================================
Type "echo '=== 1.1.1  --help ==='"
Enter
Type "./jira-tui --help"
Enter
Sleep 2s
Screenshot vhs-output/1.1.1-help-long.png

Type "echo '=== 1.1.2  -h ==='"
Enter
Type "./jira-tui -h 2>&1 | head -6"
Enter
Sleep 1s
Screenshot vhs-output/1.1.2-help-short.png

# ===========================================================================
# SECTION 1.2  Input validation (all exit immediately, no TUI launched)
# ===========================================================================
Hide
Type "clear"
Enter
Show

Type "echo '=== 1.2.1  -ccfe with no key ==='"
Enter
Type "./jira-tui -ccfe 2>&1; echo \"Exit: $?\""
Enter
Sleep 1s
Screenshot vhs-output/1.2.1-missing-epic-key.png

Type "echo '=== 1.2.2  --create-csv-from-epic with no key ==='"
Enter
Type "./jira-tui --create-csv-from-epic 2>&1; echo \"Exit: $?\""
Enter
Sleep 1s
Screenshot vhs-output/1.2.2-missing-epic-key-long.png

Type "echo '=== 1.2.3  nonexistent CSV ==='"
Enter
Type "./jira-tui /tmp/no_such_file_xyz.csv 2>&1; echo \"Exit: $?\""
Enter
Sleep 1s
Screenshot vhs-output/1.2.3-missing-csv.png

Hide
Type "clear"
Enter
Show

# ===========================================================================
# SECTION 2  Normal mode — CSV ticket creation
# Covers: 5.x (settings), 6.x (ticket list), 10.x (creation), 11.x (done)
#
# We use --project-key to skip board selection (more predictable).
# Credentials are pre-saved in JIRA_TEST_DIR/config, so auth is automatic.
# ===========================================================================
Type "echo '=== SECTION 2  Normal mode (CSV → tickets) ==='"
Enter
Sleep 0.5s
Type "./jira-tui --project-key $TEST_PROJECT_KEY \"$TEST_CSV\""
Enter

# 3.3.5 / 5.x: credentials pre-saved → screenVerify spinner, then screenSettings
Sleep 6s
Screenshot vhs-output/2-settings-initial.png

# 5.1/5.3: navigate settings
# Row 0 = Assignee Fallback, Row 1 = Issue Type
Down
Sleep 0.3s
# 5.7: cycle issue type → Story
Right
Sleep 0.3s
Screenshot vhs-output/2-settings-story-selected.png

# Cycle back to Task
Left
Sleep 0.3s
# Move back to Assignee Fallback row
Up
Sleep 0.3s
# 5.3: toggle assignee fallback to "requester"
Space
Sleep 0.3s
Screenshot vhs-output/2-settings-requester.png

# Toggle back to unassigned
Space
Sleep 0.3s

# 5.11: confirm settings → screenTickets
Enter
Sleep 1s
Screenshot vhs-output/2-ticket-list.png

# 6.1: all pre-selected; navigate a few rows
# 6.2/6.3: down then up
Down
Sleep 0.3s
Down
Sleep 0.3s
Up
Sleep 0.3s
Screenshot vhs-output/2-ticket-nav.png

# 6.5: deselect all
Type "a"
Sleep 0.3s
Screenshot vhs-output/2-all-deselected.png

# 6.6: reselect all
Type "a"
Sleep 0.3s
Screenshot vhs-output/2-all-selected.png

# 6.4: deselect first ticket (cursor is at row 0)
Space
Sleep 0.3s
Screenshot vhs-output/2-first-deselected.png

# Reselect it
Space
Sleep 0.3s

# 6.13: start creation (dup check will fire if same titles ran before)
Enter
Sleep 2s
Screenshot vhs-output/2-creating-or-dupcheck.png

# If dup check fired (screenDupCheck), skip all and proceed.
# These key presses are harmless on screenCreating if dup check is absent.
# 9.2: default is SKIP; Enter will skip all duplicates.
Enter
Sleep 2s

# 10.1/10.2: creation in progress
Screenshot vhs-output/2-creating-progress.png
Sleep 12s

# 11.1: done screen
Screenshot vhs-output/2-done-initial.png

# 11.3/11.4: navigate done list
Down
Sleep 0.3s
Down
Sleep 0.3s
Up
Sleep 0.3s
Screenshot vhs-output/2-done-navigated.png

# 11.10: export results CSV
Type "e"
Sleep 1s
Screenshot vhs-output/2-exported-csv.png

# 11.15: quit
Type "q"
Sleep 0.5s

Hide
Type "clear"
Enter
Show

# ===========================================================================
# SECTION 3  Show tickets (--show-tickets)
# Covers: 13.x
# Uses the tickets created in Section 2.
# ===========================================================================
Type "echo '=== SECTION 3  Show tickets ==='"
Enter
Sleep 0.5s
Type "./jira-tui --show-tickets"
Enter
Sleep 2s
Screenshot vhs-output/3-show-tickets-initial.png

# 13.2/13.3: navigate
Down
Sleep 0.3s
Down
Sleep 0.3s
Up
Sleep 0.3s
Screenshot vhs-output/3-show-tickets-nav.png

# 13.10/13.11: delete the currently highlighted ticket
Type "d"
Sleep 0.5s
Screenshot vhs-output/3-delete-confirm.png

# Confirm deletion
Type "y"
Sleep 1s
Screenshot vhs-output/3-after-delete.png

# 13.15: quit
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
Type "echo '=== SECTION 4  Epic → CSV export ==='"
Enter
Sleep 0.5s
Type "./jira-tui --create-csv-from-epic $TEST_EPIC_KEY"
Enter

# 14.1.1: screenVerify spinner, then screenEpicCSVQuery spinner
Sleep 6s
Screenshot vhs-output/4-epic-query-spinner.png

# Wait for epic + children to load
Sleep 8s
Screenshot vhs-output/4-epic-review-initial.png

# 14.2.5/14.2.6: navigate child list
Down
Sleep 0.3s
Down
Sleep 0.3s
Up
Sleep 0.3s

# 14.2.7/14.2.8: page navigation
PageDown
Sleep 0.3s
PageUp
Sleep 0.3s
Screenshot vhs-output/4-epic-review-navigated.png

# 14.2.11: proceed to save path screen
Enter
Sleep 0.5s
Screenshot vhs-output/4-save-path-default.png

# 14.3.2: accept default filename (press Enter)
Enter
Sleep 1s
Screenshot vhs-output/4-save-success.png

# Verify the file was created
Hide
Type "ls -lh \"${TEST_EPIC_KEY}.csv\" 2>/dev/null || echo 'CSV not found in CWD'"
Enter
Sleep 0.5s
Show
Screenshot vhs-output/4-csv-file-check.png

# 14.3.7: quit
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
Type "echo '=== SECTION 5  Interactive form (--create-ticket) ==='"
Enter
Sleep 0.5s
Type "./jira-tui --create-ticket --project-key $TEST_PROJECT_KEY"
Enter

# screenVerify spinner
Sleep 6s
Screenshot vhs-output/5-form-initial.png

# 12.1.1: Tab through fields
# Field 0 = Title
Type "[VHS] Interactive form test"
Tab
Sleep 0.3s

# Field 1 = Description (textarea)
Type "Automated VHS test. Created by run_vhs_tests.sh."
Tab
Sleep 0.3s

# Field 2 = Assignee (leave blank)
Tab
Sleep 0.3s

# Field 3 = Labels
Type "vhs-test;automated"
Tab
Sleep 0.3s

# Field 4 = Issue Type
Screenshot vhs-output/5-form-issue-type.png

# 12.1.9: cycle issue type using arrow keys
Right
Sleep 0.2s
Right
Sleep 0.2s
Left
Sleep 0.2s
Screenshot vhs-output/5-form-cycle-type.png

# Cycle back to Task (default)
Left
Sleep 0.2s

# 12.1.11: submit with Ctrl+S
Ctrl+S
Sleep 6s
Screenshot vhs-output/5-form-done.png

# 11.15: quit
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
Type "echo '=== SECTION 6  Epic creation mode ==='"
Enter
Sleep 0.5s
Type "./jira-tui --create-epic --project-key $TEST_PROJECT_KEY \"$TEST_CSV\""
Enter

# screenVerify → screenSettings
Sleep 6s
Screenshot vhs-output/6-epic-settings.png

# Accept default settings
Enter
Sleep 1s
Screenshot vhs-output/6-epic-setup.png

# 7.1.1: Title field is focused; type epic title
Type "[VHS] Epic Test"
Tab
Sleep 0.3s

# 7.1.2: Description field
Type "VHS-generated epic for automated testing."
Tab
Sleep 0.3s

# 7.1.3: Requester field
Type "vhs-test-requester"
Screenshot vhs-output/6-epic-form-filled.png

# 7.2.1: submit
Enter
Sleep 2s
Screenshot vhs-output/6-epic-ticket-list.png

# Select all tickets and create
Type "a"
Sleep 0.3s
Enter
Sleep 2s

# Handle potential dup check
Enter
Sleep 2s
Screenshot vhs-output/6-epic-creating.png
Sleep 15s
Screenshot vhs-output/6-epic-done.png

# Quit
Type "q"
Sleep 0.5s

Hide
Type "clear"
Enter
Show

# ===========================================================================
# END
# ===========================================================================
Type "echo '=== VHS test run complete. Screenshots in vhs-output/ ==='"
Enter
Sleep 1s
