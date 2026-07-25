package main

import (
	"database/sql"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// appMode controls which workflow the TUI runs.
type appMode int

const (
	modeNormal    appMode = iota // create tickets from CSV
	modeEpic                     // create an epic, then link CSV rows as children
	modeShow                     // display local ticket history only
	modeManual                   // interactively enter a single ticket (no CSV)
	modeEpicToCSV                // query an epic and export its child issues to CSV
)

func main() {
	mode := modeNormal
	csvPath := "jira_tickets.csv"
	projectKey := ""
	epicCSVKey := ""
	showHelp := false

	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--create-epic", "-ce":
			mode = modeEpic
		case "--show-tickets", "-st":
			mode = modeShow
		case "--create-ticket", "-ct":
			mode = modeManual
		case "--create-csv-from-epic", "-ccfe":
			mode = modeEpicToCSV
			if i+1 < len(args) {
				i++
				epicCSVKey = strings.ToUpper(strings.TrimSpace(args[i]))
			}
		case "--project-key", "-pk":
			if i+1 < len(args) {
				i++
				projectKey = strings.ToUpper(strings.TrimSpace(args[i]))
			}
		case "-h", "-H", "--help", "--HELP", "-?":
			showHelp = true
		default:
			csvPath = args[i]
		}
	}

	if mode == modeEpicToCSV && epicCSVKey == "" {
		fmt.Fprintln(os.Stderr, "error: --create-csv-from-epic requires a ticket ID (e.g. jira-tui -ccfe PROJ-123)")
		os.Exit(1)
	}

	if showHelp {
		printHelp()
		return
	}

	firstRun := isFirstRun()

	var (
		db  *sql.DB
		err error
	)
	if !firstRun {
		db, err = openDB()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error opening database: %v\n", err)
			os.Exit(1)
		}
		defer db.Close()
	}

	var tickets []Ticket
	if mode != modeShow && mode != modeManual && mode != modeEpicToCSV {
		tickets, err = parseCSV(csvPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading CSV: %v\n", err)
			os.Exit(1)
		}
		if mode == modeNormal && len(tickets) == 0 {
			fmt.Fprintln(os.Stderr, "no tickets found in CSV")
			os.Exit(1)
		}
	}

	cfg := loadConfig()
	m := newModel(cfg, tickets, db, mode, firstRun)
	if projectKey != "" {
		m.projectKey = projectKey
	}
	if epicCSVKey != "" {
		m.epicCSVKey = epicCSVKey
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Print(`jira-tui — Interactive Jira ticket creator

USAGE:
  jira-tui [options] [csv-file]

OPTIONS:
  -ce,   --create-epic                  Create an epic and link CSV rows as child tickets
  -ct,   --create-ticket                Interactively enter a single ticket (no CSV needed)
  -st,   --show-tickets                 Display previously created tickets from local history
  -ccfe, --create-csv-from-epic <KEY>   Query a ticket and export its child issues to CSV
  -pk,   --project-key <KEY>            Skip board picker and use this Jira project key
  -h,    --help                         Show this help message

ARGUMENTS:
  csv-file    Path to CSV file (default: jira_tickets.csv)
              Columns: Title, Description, Assignee, Labels (semicolon-separated)

EXAMPLES:
  jira-tui
  jira-tui tickets.csv
  jira-tui --create-epic tickets.csv
  jira-tui --create-ticket
  jira-tui --create-csv-from-epic PROJ-123
  jira-tui --project-key MYPROJ --create-ticket
  jira-tui --project-key MYPROJ tickets.csv
  jira-tui --show-tickets

CSV EXPORT (--create-csv-from-epic):
  Queries the given ticket key and displays its child issues interactively.
  Works for any issue type that has subtasks or child issues (epics, stories,
  tasks, etc.). After confirmation the user can choose a file path
  (default: <KEY>.csv) before saving.
  Output columns: Title, Description, Assignee, Labels, Requester
  The first four columns match the standard input CSV format so the
  exported file can be re-used directly as input to jira-tui.

CONFIG & DATA:
  Settings  ~/.jira-tui/config
  History   ~/.jira-tui/history.db
  Cache     ~/.jira-tui/boards_cache.json

ENVIRONMENT VARIABLES (override config file):
  JIRA_BASE_URL              Jira instance URL (e.g. https://myorg.atlassian.net)
  JIRA_EMAIL                 Atlassian account email
  JIRA_API_TOKEN             Atlassian API token
  JIRA_TUI_DIR               Override data directory (default: ~/.jira-tui/)
  JIRA_BOARD_CACHE_TTL_HOURS Board list cache lifetime in hours (default: 24)
  JIRA_USE_ADF               Send descriptions as Atlassian Document Format via REST v3 (true/1)

KEYBOARD (TUI):
  Tab / Shift+Tab   Move between fields
  ↑ ↓ / j k         Navigate lists
  Space              Toggle selection
  Enter              Confirm / advance
  Ctrl+C             Quit at any time
`)
}
