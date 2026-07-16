package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	csvPath := "jira_tickets.csv"
	if len(os.Args) > 1 {
		csvPath = os.Args[1]
	}

	tickets, err := parseCSV(csvPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading CSV: %v\n", err)
		os.Exit(1)
	}
	if len(tickets) == 0 {
		fmt.Fprintln(os.Stderr, "no tickets found in CSV")
		os.Exit(1)
	}

	cfg := loadConfig()
	m := newModel(cfg, tickets)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
