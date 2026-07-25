# VHS_Testing.tape
#
# Drives jira-tui through its key interactive flows.
# Run via:  ./run_vhs_tests.sh
#
# IMPORTANT: th() headers are only shown at the shell prompt — never while
# the TUI is running.  Typing Enter into a running Bubbletea app quits it
# (Enter is mapped to quit on most screens), which would send the TUI to the
# background before any screenshots are taken.
#
# Required environment (set by run_vhs_tests.sh, or export manually):
#   JIRA_TEST_DIR       – pre-populated data directory (config + history.db)
#   JIRA_TEST_CSV       – path to the test CSV file created by the script
#   TEST_PROJECT_KEY    – a real Jira project key (e.g. "PROJ")
#   TEST_EPIC_KEY       – a real Jira Epic key (e.g. "PROJ-1")

Output vhs-output/jira-tui-tests.gif

Set Shell      bash
Set Width      1600
Set Height     900
Set FontSize   13
Set Framerate  10
Set Theme      "Dracula"

# ---------------------------------------------------------------------------
# GLOBAL SETUP — export env vars and define the th() header function
# run_vhs_tests.sh writes /tmp/jira-tui-th.sh before launching VHS.
# ---------------------------------------------------------------------------
Hide
Type "export JIRA_TUI_DIR=$JIRA_TEST_DIR"
Enter
Type "export TEST_CSV=$JIRA_TEST_CSV"
Enter
Type "source /tmp/jira-tui-th.sh"
Enter
Type "clear"
Enter
Show

# ===========================================================================
# SECTION 1.1  Help flags — all at shell, th() safe here
# ===========================================================================

Hide
Type "th '1.1.1' 'Help flag: --help exits 0 and prints full usage'"
Enter
Sleep 0.5s
Show
Type "./jira-tui --help"
Enter
Sleep 2s
Screenshot vhs-output/1.1.1-help-long.png

Hide
Type "th '1.1.2' 'Help flag: -h exits 0 and prints usage'"
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

# ===========================================================================
# SECTION 1.2  Input validation — all at shell, th() safe here
# ===========================================================================

Hide
Type "clear"
Enter
Type "th '1.2.1' 'Validation: -ccfe with no key -> stderr error, exit 1'"
Enter
Sleep 0.5s
Show
Type "./jira-tui -ccfe 2>&1; echo exit=$?"
Enter
Sleep 1s
Screenshot vhs-output/1.2.1-missing-epic-key.png

Hide
Type "th '1.2.2' 'Validation: --create-csv-from-epic with no key -> exit 1'"
Enter
Sleep 0.5s
Show
Type "./jira-tui --create-csv-from-epic 2>&1; echo exit=$?"
Enter
Sleep 1s
Screenshot vhs-output/1.2.2-missing-epic-key-long.png

Hide
Type "th '1.2.3' 'Validation: nonexistent CSV path -> stderr error, exit 1'"
Enter
Sleep 0.5s
Show
Type "./jira-tui /tmp/no_such_file_xyz.csv 2>&1; echo exit=$?"
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
# Tests covered inside the TUI: 3.3.5, 5.1-5.11, 6.1-6.13, 9.2,
#                                10.1-10.2, 11.1-11.10, 11.15
#
# th() headers shown at shell before launch only; no th() inside the TUI.
# ===========================================================================

Hide
Type "th 'SECTION 2' 'Normal mode — tests: 3.3.5 | 5.1-5.11 | 6.1-6.13 | 9.2 | 10.1-10.2 | 11.1-11.15'"
Enter
Sleep 0.5s
Show

Type "./jira-tui --project-key $TEST_PROJECT_KEY $TEST_CSV"
Enter

# 3.3.5: credentials pre-saved → screenVerify spinner → screenSettings
Sleep 7s
Screenshot vhs-output/2-settings-initial.png

# 5.1: Down moves cursor to Issue Type row
Down
Sleep 0.3s
# 5.7: Right cycles Issue Type forward (Task → Story)
Right
Sleep 0.3s
Screenshot vhs-output/2-settings-story-selected.png
# 5.8: Left cycles Issue Type backward (Story → Task)
Left
Sleep 0.3s
# 5.2: Up returns to Assignee Fallback row
Up
Sleep 0.3s
# 5.3: Space toggles Assignee Fallback to requester
Space
Sleep 0.3s
Screenshot vhs-output/2-settings-requester.png
# 5.3: Space toggles back to unassigned
Space
Sleep 0.3s
# 5.11: Enter confirms settings → screenTickets
Enter
Sleep 1s
Screenshot vhs-output/2-ticket-list.png

# 6.1: all pre-selected; 6.2: Down moves cursor
Down
Sleep 0.3s
Down
Sleep 0.3s
# 6.3: Up moves cursor
Up
Sleep 0.3s
Screenshot vhs-output/2-ticket-nav.png
# 6.5: a deselects all
Type "a"
Sleep 0.3s
Screenshot vhs-output/2-all-deselected.png
# 6.6: a reselects all
Type "a"
Sleep 0.3s
Screenshot vhs-output/2-all-selected.png
# 6.4: Space toggles single ticket
Space
Sleep 0.3s
Screenshot vhs-output/2-first-deselected.png
Space
Sleep 0.3s

# 6.13: Enter starts creation (dup check may appear on repeated runs)
Enter
Sleep 2s
Screenshot vhs-output/2-creating-or-dupcheck.png
# 9.2: if dup check fired, default is SKIP; Enter skips all and proceeds
Enter
Sleep 2s

# 10.1/10.2: creation in progress
Screenshot vhs-output/2-creating-progress.png
Sleep 14s

# 11.1: done screen summary
Screenshot vhs-output/2-done-initial.png
# 11.3/11.4: navigate done list
Down
Sleep 0.3s
Down
Sleep 0.3s
Up
Sleep 0.3s
Screenshot vhs-output/2-done-navigated.png
# 11.10: export results CSV with e
Type "e"
Sleep 1s
Screenshot vhs-output/2-exported-csv.png
# 11.15: quit
Type "q"
Sleep 2s

Hide
Type "clear"
Enter
Show

# ===========================================================================
# SECTION 3  Show tickets (--show-tickets)
# Tests covered inside the TUI: 13.1-13.3, 13.10-13.11, 13.15
# ===========================================================================

Hide
Type "th 'SECTION 3' 'History mode — tests: 13.1 | 13.2-13.3 | 13.10-13.11 | 13.15'"
Enter
Sleep 0.5s
Show

Type "./jira-tui --show-tickets"
Enter
Sleep 2s
Screenshot vhs-output/3-show-tickets-initial.png

# 13.2: Down moves cursor; 13.3: Up moves cursor
Down
Sleep 0.3s
Down
Sleep 0.3s
Up
Sleep 0.3s
Screenshot vhs-output/3-show-tickets-nav.png

# 13.10: d shows delete confirmation
Type "d"
Sleep 0.5s
Screenshot vhs-output/3-delete-confirm.png

# 13.11: y confirms deletion
Type "y"
Sleep 1s
Screenshot vhs-output/3-after-delete.png

# 13.15: q quits
Type "q"
Sleep 2s

Hide
Type "clear"
Enter
Show

# ===========================================================================
# SECTION 4  Epic → CSV export (--create-csv-from-epic)
# Tests covered inside the TUI: 14.1.1, 14.2.1-14.2.11, 14.3.1-14.3.2
# ===========================================================================

Hide
Type "th 'SECTION 4' 'Epic->CSV — tests: 14.1.1 | 14.2.1-14.2.11 | 14.3.1-14.3.2 | 14.4.1'"
Enter
Sleep 0.5s
Show

Type "./jira-tui --create-csv-from-epic $TEST_EPIC_KEY"
Enter

# 14.1.1: screenVerify spinner then screenEpicCSVQuery spinner
Sleep 7s
Screenshot vhs-output/4-epic-query-spinner.png
# Wait for epic + children to load
Sleep 9s

# 14.2.1: epic header; 14.2.2: child count line
Screenshot vhs-output/4-epic-review-initial.png

# 14.2.5/14.2.6: Down and Up navigate child list
Down
Sleep 0.3s
Down
Sleep 0.3s
Up
Sleep 0.3s
# 14.2.7/14.2.8: PgDn / PgUp page navigation
PageDown
Sleep 0.3s
PageUp
Sleep 0.3s
Screenshot vhs-output/4-epic-review-navigated.png

# 14.2.11: Enter proceeds to save-path screen
Enter
Sleep 0.5s
# 14.3.1: save path pre-filled with KEY.csv
Screenshot vhs-output/4-save-path-default.png

# 14.3.2: Enter accepts default filename and writes CSV
Enter
Sleep 2s
Screenshot vhs-output/4-save-success.png

# 14.3.7: quit after save
Type "q"
Sleep 2s

# 14.4.1: verify CSV file and header at shell
Hide
Type "th '14.4.1' 'CSV output: file exists with correct header row'"
Enter
Sleep 0.5s
Show
Type "head -1 ${TEST_EPIC_KEY}.csv 2>/dev/null && echo found || echo not-found"
Enter
Sleep 1s
Screenshot vhs-output/4-csv-header-check.png

Hide
Type "clear"
Enter
Show

# ===========================================================================
# SECTION 5  Interactive ticket form (--create-ticket)
# Tests covered inside the TUI: 3.3.3, 12.1.1-12.1.11, 12.2.1
# ===========================================================================

Hide
Type "th 'SECTION 5' 'Interactive form — tests: 3.3.3 | 12.1.1-12.1.11 | 12.2.1'"
Enter
Sleep 0.5s
Show

Type "./jira-tui --create-ticket --project-key $TEST_PROJECT_KEY"
Enter

# 3.3.3: --create-ticket + --project-key goes straight to form
Sleep 7s
Screenshot vhs-output/5-form-initial.png

# 12.1.1: Tab advances from Title field
Type "[VHS] Interactive form test"
Tab
Sleep 0.3s

# 12.1.4: Enter in Description inserts newline, does not advance
Type "Automated VHS test. Created by run_vhs_tests.sh."
Tab
Sleep 0.3s

# 12.1.5: Tab on Assignee advances to Labels
Tab
Sleep 0.3s

# 12.1.6: Tab on Labels advances to Issue Type
Type "vhs-test;automated"
Tab
Sleep 0.3s
Screenshot vhs-output/5-form-issue-type.png

# 12.1.9: Right/Left cycle Issue Type when Issue Type field is focused
Right
Sleep 0.2s
Right
Sleep 0.2s
Left
Sleep 0.2s
Left
Sleep 0.2s
Screenshot vhs-output/5-form-cycle-type.png

# 12.1.11: Ctrl+S submits from any field
Ctrl+S
Sleep 7s

# 12.2.1: Task submission → Done screen
Screenshot vhs-output/5-form-done.png

Type "q"
Sleep 2s

Hide
Type "clear"
Enter
Show

# ===========================================================================
# SECTION 6  Epic creation mode (--create-epic)
# Tests covered inside the TUI: 3.3.1, 5.12, 7.1.1-7.2.1, 6.6, 6.13,
#                                9.2, 10.8, 11.9, 11.15
# ===========================================================================

Hide
Type "th 'SECTION 6' 'Epic mode — tests: 5.12 | 7.1.1-7.2.1 | 6.6 | 6.13 | 10.8 | 11.9'"
Enter
Sleep 0.5s
Show

Type "./jira-tui --create-epic --project-key $TEST_PROJECT_KEY $TEST_CSV"
Enter

# 3.3.1: saved creds → screenVerify → screenSettings
Sleep 7s
Screenshot vhs-output/6-epic-settings.png

# 5.12: in --create-epic mode, Enter advances to Epic Setup screen
Enter
Sleep 1s
Screenshot vhs-output/6-epic-setup.png

# 7.1.1: Title field focused
Type "[VHS] Epic Test"
# 7.1.2: Tab moves to Description
Tab
Sleep 0.3s
Type "VHS-generated epic for automated testing."
# 7.1.3: Tab moves to Requester
Tab
Sleep 0.3s
Type "vhs-test-requester"
Screenshot vhs-output/6-epic-form-filled.png

# 7.2.1: Enter submits; unique title → proceeds to ticket list
Enter
Sleep 2s
Screenshot vhs-output/6-epic-ticket-list.png

# 6.6: a selects all tickets
Type "a"
Sleep 0.3s
# 6.13: Enter starts epic + subtask creation
Enter
Sleep 2s
# 9.2: if dup check fired, Enter skips all dups
Enter
Sleep 2s

# 10.8: epic row appears first; spinner then checkmark
Screenshot vhs-output/6-epic-creating.png
Sleep 16s

# 11.9: first row is the epic; child tickets follow
Screenshot vhs-output/6-epic-done.png

# 11.15: quit
Type "q"
Sleep 2s

Hide
Type "clear"
Enter
Show

# ===========================================================================
# END
# ===========================================================================

Hide
Type "th 'COMPLETE' 'All VHS flows finished — artifacts in vhs-output/'"
Enter
Sleep 0.5s
Show
Type "ls vhs-output/*.png | wc -l | xargs -I{} echo '{} screenshots captured'"
Enter
Sleep 1s
