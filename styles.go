package main

import "github.com/charmbracelet/lipgloss"

var (
	purple  = lipgloss.Color("#7C3AED")
	green   = lipgloss.Color("#10B981")
	red     = lipgloss.Color("#EF4444")
	muted   = lipgloss.Color("#6B7280")
	subtle  = lipgloss.Color("#374151")
	text    = lipgloss.Color("#F9FAFB")
	subtext = lipgloss.Color("#9CA3AF")

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(purple)

	subtitleStyle = lipgloss.NewStyle().
			Foreground(subtext)

	successStyle = lipgloss.NewStyle().
			Foreground(green).
			Bold(true)

	errorStyle = lipgloss.NewStyle().
			Foreground(red).
			Bold(true)

	dimStyle = lipgloss.NewStyle().
			Foreground(muted)

	cursorStyle = lipgloss.NewStyle().
			Foreground(green).
			Bold(true)

	selectedItemStyle = lipgloss.NewStyle().
				Foreground(green).
				Bold(true)

	checkStyle = lipgloss.NewStyle().
			Foreground(green)

	labelStyle = lipgloss.NewStyle().
			Background(purple).
			Foreground(text).
			Padding(0, 1)

	keyStyle = lipgloss.NewStyle().
			Background(subtle).
			Foreground(text).
			Padding(0, 1)

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(purple).
			Padding(1, 2)

	activePanelStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(green).
				Padding(1, 2)

	focusedInputStyle = lipgloss.NewStyle().
				BorderStyle(lipgloss.NormalBorder()).
				BorderForeground(green).
				Padding(0, 1)

	blurredInputStyle = lipgloss.NewStyle().
				BorderStyle(lipgloss.NormalBorder()).
				BorderForeground(muted).
				Padding(0, 1)

	footerStyle = lipgloss.NewStyle().
			Foreground(subtext).
			BorderTop(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(subtle).
			PaddingTop(1).
			MarginTop(1)
)

// progressBar renders a simple inline progress bar without animation overhead.
func progressBar(width int, pct float64) string {
	if pct > 1 {
		pct = 1
	}
	filled := int(float64(width) * pct)
	empty := width - filled
	bar := successStyle.Render(repeatChar("█", filled)) +
		dimStyle.Render(repeatChar("░", empty))
	return bar
}

func repeatChar(ch string, n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, n*len(ch))
	for i := 0; i < n; i++ {
		copy(b[i*len(ch):], ch)
	}
	return string(b)
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}
