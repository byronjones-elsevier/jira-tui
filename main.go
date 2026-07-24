package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

// appMode controls which workflow the TUI runs.
type appMode int

const (
	modeNormal appMode = iota // create tickets from CSV
	modeEpic                  // create an epic, then link CSV rows as children
	modeShow                  // display local ticket history only
)

func main() {
	mode := modeNormal
	csvPath := "jira_tickets.csv"
	showHelp := false

	for _, arg := range os.Args[1:] {
		switch arg {
		case "--create-epic", "-ce":
			mode = modeEpic
		case "--show-tickets", "-st":
			mode = modeShow
		case "-h", "-H", "--help", "--HELP", "-?":
			showHelp = true
		default:
			csvPath = arg
		}
	}

	if showHelp {
		printHelp()
		return
	}

	db, err := openDB()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	var tickets []Ticket
	if mode != modeShow {
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
	m := newModel(cfg, tickets, db, mode)
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
  -ce, --create-epic    Create an epic and link CSV rows as child tickets
  -st, --show-tickets   Display previously created tickets from local history
  -h, --help            Show this help message

ARGUMENTS:
  csv-file    Path to CSV file (default: jira_tickets.csv)
              Columns: Title, Description, Assignee, Labels (semicolon-separated)

EXAMPLES:
  jira-tui
  jira-tui tickets.csv
  jira-tui --create-epic tickets.csv
  jira-tui --show-tickets

CONFIG & DATA:
  Settings  ~/.jira-tui/config
  History   ~/.jira-tui/history.db
  Cache     ~/.jira-tui/boards_cache.json

KEYBOARD (TUI):
  Tab / Shift+Tab   Move between fields
  ↑ ↓ / j k         Navigate lists
  Space              Toggle selection
  Enter              Confirm / advance
  Ctrl+C             Quit at any time
`)
}
