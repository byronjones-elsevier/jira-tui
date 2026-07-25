package main

import (
	"encoding/csv"
	"os"
	"strings"
)

// Ticket represents one row from the CSV (Title, Description, Assignee, Labels).
type Ticket struct {
	Title       string
	Description string
	Assignee    string
	Labels      []string
}

// parseCSV reads a CSV file with header row Title,Description,Assignee,Labels.
// Labels are semicolon-separated; spaces are converted to dashes.
// A UTF-8 BOM (\xef\xbb\xbf) is stripped if present — Excel emits one on UTF-8 exports.
func parseCSV(path string) ([]Ticket, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}

	col := func(row []string, i int) string {
		if i < len(row) {
			return strings.TrimSpace(row[i])
		}
		return ""
	}

	var tickets []Ticket
	for i, row := range records {
		if i == 0 {
			// Strip UTF-8 BOM from the first field of the header row if present.
			if len(row) > 0 {
				row[0] = strings.TrimPrefix(row[0], "\xef\xbb\xbf")
			}
			continue // skip header
		}
		title := col(row, 0)
		// Strip BOM from first data field too (in case the file has no header BOM).
		title = strings.TrimPrefix(title, "\xef\xbb\xbf")
		if title == "" {
			continue
		}
		var labels []string
		if raw := col(row, 3); raw != "" {
			for _, l := range strings.Split(raw, ";") {
				l = strings.TrimSpace(l)
				l = strings.ReplaceAll(l, " ", "-")
				if l != "" {
					labels = append(labels, l)
				}
			}
		}
		tickets = append(tickets, Ticket{
			Title:       title,
			Description: col(row, 1),
			Assignee:    col(row, 2),
			Labels:      labels,
		})
	}
	return tickets, nil
}
