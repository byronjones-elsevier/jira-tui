package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Screens ──────────────────────────────────────────────────────────────────

type screen int

const (
	screenAuth     screen = iota // credential entry
	screenVerify                 // verifying creds (spinner)
	screenBoards                 // board list / manual key entry
	screenSettings               // assignee fallback + issue type
	screenTickets                // ticket list + preview
	screenCreating               // creation progress
	screenDone                   // results summary
)

// ── Messages ─────────────────────────────────────────────────────────────────

type authVerifiedMsg struct {
	user User
	err  error
}

type boardsLoadedMsg struct {
	boards []Board
	err    error
}

// boardsSyncedMsg is sent when the background board refresh finishes.
type boardsSyncedMsg struct {
	boards []Board
	err    error
}

type ticketCreatedMsg struct {
	index int
	key   string
	err   error
}

// ── Model ─────────────────────────────────────────────────────────────────────

var issueTypes = []string{"Task", "Story", "Bug", "Subtask", "Epic"}

type model struct {
	screen  screen
	config  Config
	tickets []Ticket
	width   int
	height  int

	// Jira
	client       *JiraClient
	user         User
	boards       []Board
	selectedBoard Board
	projectKey   string

	// Auth inputs
	authInputs   []textinput.Model
	focusedInput int

	// Boards
	boardCursor     int
	boardOffset     int // first visible index in filtered list
	boardSearch     textinput.Model
	manualKeyMode   bool
	manualKeyInput  textinput.Model
	boardsSyncing   bool      // background refresh in progress
	boardsCacheAge  time.Time // when the cache was last written

	// Settings (0 = assignee fallback, 1 = issue type)
	settingsCursor   int
	assigneeFallback string
	issueType        string

	// Tickets
	ticketCursor    int
	selectedTickets map[int]bool

	// Creation
	creating  int // index of ticket currently being created
	results   []CreateResult
	progress  float64

	// UI helpers
	spinner    spinner.Model
	loading    bool
	loadingMsg string
	err        error
}

// ── Constructor ───────────────────────────────────────────────────────────────

func newModel(cfg Config, tickets []Ticket) model {
	// Auth inputs: URL, email, token
	inputs := make([]textinput.Model, 3)
	placeholders := []string{
		"https://company.atlassian.net",
		"you@company.com",
		"API token (input hidden)",
	}
	for i := range inputs {
		t := textinput.New()
		t.CharLimit = 256
		t.Placeholder = placeholders[i]
		inputs[i] = t
	}
	inputs[0].SetValue(cfg.BaseURL)
	inputs[1].SetValue(cfg.Email)
	inputs[2].EchoMode = textinput.EchoPassword
	inputs[2].SetValue(cfg.APIToken)

	manualInput := textinput.New()
	manualInput.Placeholder = "e.g. FINOPS"
	manualInput.CharLimit = 32

	boardSearch := textinput.New()
	boardSearch.Placeholder = "Search boards…"
	boardSearch.CharLimit = 100

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(purple)

	selected := make(map[int]bool, len(tickets))
	for i := range tickets {
		selected[i] = true
	}

	af := cfg.AssigneeFallback
	if af == "" {
		af = "unassigned"
	}
	it := cfg.IssueType
	if it == "" {
		it = "Task"
	}

	m := model{
		config:           cfg,
		tickets:          tickets,
		authInputs:       inputs,
		manualKeyInput:   manualInput,
		boardSearch:      boardSearch,
		spinner:          s,
		selectedTickets:  selected,
		assigneeFallback: af,
		issueType:        it,
		results:          make([]CreateResult, len(tickets)),
	}

	hasCreds := cfg.BaseURL != "" && cfg.Email != "" && cfg.APIToken != ""
	if hasCreds {
		m.screen = screenVerify
		m.loading = true
		m.loadingMsg = "Verifying saved credentials…"
		m.client = newJiraClient(cfg.BaseURL, cfg.Email, cfg.APIToken)
	} else {
		m.screen = screenAuth
		inputs[0].Focus()
	}

	return m
}

// ── Init ──────────────────────────────────────────────────────────────────────

func (m model) Init() tea.Cmd {
	cmds := []tea.Cmd{textinput.Blink, m.spinner.Tick}
	if m.screen == screenVerify {
		cmds = append(cmds, m.cmdVerifyAuth())
	}
	return tea.Batch(cmds...)
}

// ── Commands ──────────────────────────────────────────────────────────────────

func (m model) cmdVerifyAuth() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		user, err := client.VerifyAuth()
		return authVerifiedMsg{user: user, err: err}
	}
}

func (m model) cmdLoadBoards() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		boards, err := client.GetBoards()
		return boardsLoadedMsg{boards: boards, err: err}
	}
}

// cmdSyncBoards fetches boards in the background and returns boardsSyncedMsg.
func (m model) cmdSyncBoards() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		boards, err := client.GetBoards()
		return boardsSyncedMsg{boards: boards, err: err}
	}
}

func (m model) cmdCreateTicket(index int) tea.Cmd {
	client := m.client
	ticket := m.tickets[index]
	projectKey := m.projectKey
	issueType := m.issueType
	fallback := m.assigneeFallback
	requesterID := m.user.AccountID

	return func() tea.Msg {
		assigneeID := ""
		if ticket.Assignee != "" {
			id, _ := client.ResolveAccountID(ticket.Assignee)
			assigneeID = id
		}
		if assigneeID == "" && fallback == "requester" {
			assigneeID = requesterID
		}
		key, err := client.CreateIssue(projectKey, issueType, ticket, assigneeID)
		return ticketCreatedMsg{index: index, key: key, err: err}
	}
}

// ── Update ────────────────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m.handleKey(msg)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case authVerifiedMsg:
		if msg.err != nil {
			m.loading = false
			m.err = fmt.Errorf("authentication failed — check URL, email, and token")
			m.screen = screenAuth
			focusCmd := m.authInputs[m.focusedInput].Focus()
			return m, focusCmd
		}
		m.user = msg.user
		// Auto-save credentials.
		m.config.BaseURL = m.client.baseURL
		m.config.Email = m.client.email
		m.config.APIToken = m.client.token
		_ = saveConfig(m.config)
		// Advance to board screen — use cache if available.
		m.screen = screenBoards
		if cached, cacheAge, ok := loadBoardsCache(m.client.baseURL); ok {
			// Show cached boards immediately; refresh in background.
			m.boards = cached
			m.boardsCacheAge = cacheAge
			m.loading = false
			m.boardsSyncing = true
			return m, tea.Batch(m.cmdSyncBoards(), m.boardSearch.Focus())
		}
		// No cache — show spinner until first load completes.
		m.loading = true
		m.loadingMsg = "Loading all boards…"
		return m, m.cmdLoadBoards()

	case boardsLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.err = fmt.Errorf("could not load boards: %v", msg.err)
		} else {
			m.boards = msg.boards
			m.boardsCacheAge = time.Now()
			_ = saveBoardsCache(m.client.baseURL, msg.boards)
		}
		return m, m.boardSearch.Focus()

	case boardsSyncedMsg:
		m.boardsSyncing = false
		if msg.err == nil {
			m.boards = msg.boards
			m.boardsCacheAge = time.Now()
			_ = saveBoardsCache(m.client.baseURL, msg.boards)
			// Clamp cursor in case list shrank.
			filtered := m.filteredBoards()
			if m.boardCursor >= len(filtered) {
				m.boardCursor = maxInt(0, len(filtered)-1)
				m.boardOffset = maxInt(0, m.boardCursor-m.boardPageSize()+1)
			}
		}
		return m, nil

	case ticketCreatedMsg:
		m.results[msg.index] = CreateResult{
			Ticket: m.tickets[msg.index],
			Key:    msg.key,
			URL:    m.config.BaseURL + "/browse/" + msg.key,
			Err:    msg.err,
		}
		// Find next selected ticket.
		next := -1
		for i := msg.index + 1; i < len(m.tickets); i++ {
			if m.selectedTickets[i] {
				next = i
				break
			}
		}
		if next >= 0 {
			m.creating = next
			m.progress = float64(m.countDone()) / float64(m.countSelected())
			return m, m.cmdCreateTicket(next)
		}
		m.progress = 1.0
		m.screen = screenDone
		return m, nil
	}

	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.screen {
	case screenAuth:
		return m.handleAuthKey(msg)
	case screenBoards:
		return m.handleBoardsKey(msg)
	case screenSettings:
		return m.handleSettingsKey(msg)
	case screenTickets:
		return m.handleTicketsKey(msg)
	case screenDone:
		return m, tea.Quit
	}
	return m, nil
}

// ── Auth keys ─────────────────────────────────────────────────────────────────

func (m model) handleAuthKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg.String() {
	case "tab", "down":
		m.authInputs[m.focusedInput].Blur()
		m.focusedInput = (m.focusedInput + 1) % len(m.authInputs)
		cmds = append(cmds, m.authInputs[m.focusedInput].Focus())

	case "shift+tab", "up":
		m.authInputs[m.focusedInput].Blur()
		m.focusedInput = (m.focusedInput - 1 + len(m.authInputs)) % len(m.authInputs)
		cmds = append(cmds, m.authInputs[m.focusedInput].Focus())

	case "enter":
		if m.focusedInput < len(m.authInputs)-1 {
			m.authInputs[m.focusedInput].Blur()
			m.focusedInput++
			cmds = append(cmds, m.authInputs[m.focusedInput].Focus())
		} else {
			baseURL := strings.TrimRight(m.authInputs[0].Value(), "/")
			email := m.authInputs[1].Value()
			token := m.authInputs[2].Value()
			if baseURL == "" || email == "" || token == "" {
				m.err = fmt.Errorf("all three fields are required")
				break
			}
			m.err = nil
			m.config.BaseURL = baseURL
			m.config.Email = email
			m.config.APIToken = token
			m.client = newJiraClient(baseURL, email, token)
			m.screen = screenVerify
			m.loading = true
			m.loadingMsg = "Verifying credentials…"
			cmds = append(cmds, m.cmdVerifyAuth())
		}

	default:
		var cmd tea.Cmd
		m.authInputs[m.focusedInput], cmd = m.authInputs[m.focusedInput].Update(msg)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

// ── Board keys ────────────────────────────────────────────────────────────────

// filteredBoards returns boards matching the current search query.
func (m model) filteredBoards() []Board {
	query := strings.ToLower(strings.TrimSpace(m.boardSearch.Value()))
	if query == "" {
		return m.boards
	}
	var out []Board
	for _, b := range m.boards {
		if strings.Contains(strings.ToLower(b.Name), query) ||
			strings.Contains(strings.ToLower(b.ProjectKey), query) {
			out = append(out, b)
		}
	}
	return out
}

// boardPageSize returns how many board rows fit given the current terminal height.
func (m model) boardPageSize() int {
	// Reserve rows: title + sub + search + pagination + footer + borders/padding
	n := m.height - 12
	if n < 4 {
		n = 4
	}
	if n > 25 {
		n = 25
	}
	return n
}

func (m model) handleBoardsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.loading {
		return m, nil
	}

	// ── Manual project-key entry ──────────────────────────────────────────────
	if m.manualKeyMode {
		var cmd tea.Cmd
		switch msg.String() {
		case "enter":
			key := strings.ToUpper(strings.TrimSpace(m.manualKeyInput.Value()))
			if key != "" {
				m.projectKey = key
				m.manualKeyMode = false
				m.screen = screenSettings
			}
		case "esc":
			m.manualKeyMode = false
			m.manualKeyInput.Blur()
			return m, m.boardSearch.Focus()
		default:
			m.manualKeyInput, cmd = m.manualKeyInput.Update(msg)
		}
		return m, cmd
	}

	filtered := m.filteredBoards()
	pageSize := m.boardPageSize()

	// Clamp cursor to filtered length.
	if m.boardCursor >= len(filtered) {
		m.boardCursor = maxInt(0, len(filtered)-1)
	}

	switch msg.String() {
	case "up", "k":
		if m.boardCursor > 0 {
			m.boardCursor--
		}
		// Scroll up if cursor moved above the visible window.
		if m.boardCursor < m.boardOffset {
			m.boardOffset = m.boardCursor
		}

	case "down", "j":
		if m.boardCursor < len(filtered)-1 {
			m.boardCursor++
		}
		// Scroll down if cursor moved below the visible window.
		if m.boardCursor >= m.boardOffset+pageSize {
			m.boardOffset = m.boardCursor - pageSize + 1
		}

	case "pgup", "ctrl+b":
		m.boardCursor -= pageSize
		if m.boardCursor < 0 {
			m.boardCursor = 0
		}
		m.boardOffset -= pageSize
		if m.boardOffset < 0 {
			m.boardOffset = 0
		}

	case "pgdown", "ctrl+f":
		m.boardCursor += pageSize
		if m.boardCursor >= len(filtered) {
			m.boardCursor = maxInt(0, len(filtered)-1)
		}
		m.boardOffset += pageSize
		maxOffset := maxInt(0, len(filtered)-pageSize)
		if m.boardOffset > maxOffset {
			m.boardOffset = maxOffset
		}

	case "enter":
		if len(filtered) > 0 && m.boardCursor < len(filtered) {
			m.selectedBoard = filtered[m.boardCursor]
			m.projectKey = m.selectedBoard.ProjectKey
			m.screen = screenSettings
		}

	case "r":
		// Trigger a background refresh when the search box is empty and not already syncing.
		// Otherwise fall through to the default search-box handler so 'r' is typed normally.
		if m.boardSearch.Value() == "" && !m.boardsSyncing {
			m.boardsSyncing = true
			return m, m.cmdSyncBoards()
		}
		prevValR := m.boardSearch.Value()
		var cmdR tea.Cmd
		m.boardSearch, cmdR = m.boardSearch.Update(msg)
		if m.boardSearch.Value() != prevValR {
			m.boardCursor = 0
			m.boardOffset = 0
		}
		return m, cmdR

	case "ctrl+m", "m":
		// Only treat bare 'm' as manual-mode trigger when the search box is empty,
		// otherwise it's a search character — fall through to the search handler.
		if m.boardSearch.Value() == "" {
			m.manualKeyMode = true
			m.manualKeyInput.SetValue("")
			m.boardSearch.Blur()
			return m, m.manualKeyInput.Focus()
		}
		fallthrough

	default:
		// Route every other keystroke into the search box.
		prevVal := m.boardSearch.Value()
		var cmd tea.Cmd
		m.boardSearch, cmd = m.boardSearch.Update(msg)
		// Reset cursor/offset whenever the query changes.
		if m.boardSearch.Value() != prevVal {
			m.boardCursor = 0
			m.boardOffset = 0
		}
		return m, cmd
	}

	return m, nil
}

// ── Settings keys ─────────────────────────────────────────────────────────────

func (m model) handleSettingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.settingsCursor > 0 {
			m.settingsCursor--
		}
	case "down", "j":
		if m.settingsCursor < 1 {
			m.settingsCursor++
		}
	case "left", "h":
		switch m.settingsCursor {
		case 0:
			m.assigneeFallback = "unassigned"
		case 1:
			m.issueType = prevIn(issueTypes, m.issueType)
		}
	case "right", "l", " ":
		switch m.settingsCursor {
		case 0:
			if m.assigneeFallback == "unassigned" {
				m.assigneeFallback = "requester"
			} else {
				m.assigneeFallback = "unassigned"
			}
		case 1:
			m.issueType = nextIn(issueTypes, m.issueType)
		}
	case "enter":
		m.screen = screenTickets
	}
	return m, nil
}

// ── Ticket keys ───────────────────────────────────────────────────────────────

func (m model) handleTicketsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.ticketCursor > 0 {
			m.ticketCursor--
		}
	case "down", "j":
		if m.ticketCursor < len(m.tickets)-1 {
			m.ticketCursor++
		}
	case " ":
		m.selectedTickets[m.ticketCursor] = !m.selectedTickets[m.ticketCursor]
	case "a":
		all := m.countSelected() == len(m.tickets)
		for i := range m.tickets {
			m.selectedTickets[i] = !all
		}
	case "enter":
		if m.countSelected() == 0 {
			return m, nil
		}
		m.config.AssigneeFallback = m.assigneeFallback
		m.config.IssueType = m.issueType
		_ = saveConfig(m.config)

		// Find first selected ticket.
		first := -1
		for i := range m.tickets {
			if m.selectedTickets[i] {
				first = i
				break
			}
		}
		if first < 0 {
			return m, nil
		}
		m.screen = screenCreating
		m.creating = first
		return m, m.cmdCreateTicket(first)
	}
	return m, nil
}

// ── View ──────────────────────────────────────────────────────────────────────

func (m model) View() string {
	if m.width == 0 {
		return ""
	}
	switch m.screen {
	case screenAuth:
		return m.viewAuth()
	case screenVerify:
		return m.viewSpinner(m.loadingMsg)
	case screenBoards:
		if m.loading {
			return m.viewSpinner(m.loadingMsg)
		}
		return m.viewBoards()
	case screenSettings:
		return m.viewSettings()
	case screenTickets:
		return m.viewTickets()
	case screenCreating:
		return m.viewCreating()
	case screenDone:
		return m.viewDone()
	}
	return ""
}

func (m model) viewSpinner(msg string) string {
	content := m.spinner.View() + "  " + subtitleStyle.Render(msg)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

// ── Auth view ─────────────────────────────────────────────────────────────────

func (m model) viewAuth() string {
	labels := []string{"Site URL", "Email", "API Token"}
	var rows []string
	for i, inp := range m.authInputs {
		style := blurredInputStyle
		if i == m.focusedInput {
			style = focusedInputStyle
		}
		rows = append(rows, subtitleStyle.Render(labels[i])+"\n"+style.Width(44).Render(inp.View()))
	}

	errLine := ""
	if m.err != nil {
		errLine = "\n" + errorStyle.Render("⚠  "+m.err.Error())
	}

	hint := subtitleStyle.Render("Create a token at https://id.atlassian.com/manage-profile/security/api-tokens")

	footer := footerStyle.Width(48).Render("Tab/↑↓ move field  •  Enter next/connect  •  Ctrl+C quit")

	body := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("🎫 Jira Ticket Creator"),
		subtitleStyle.Render("Connect to your Jira instance"),
		"",
		strings.Join(rows, "\n\n"),
		errLine,
		"",
		hint,
		footer,
	)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		panelStyle.Width(52).Render(body))
}

// ── Boards view ───────────────────────────────────────────────────────────────

func (m model) viewBoards() string {
	w := clamp(m.width-8, 60, 88)

	title := titleStyle.Render("Select a Board")

	// Sub-line: user + optional sync badge on the right.
	userStr := "Authenticated as " + m.user.DisplayName
	var syncBadge string
	if m.boardsSyncing {
		syncBadge = "  " + m.spinner.View() + dimStyle.Render(" syncing…")
	} else if !m.boardsCacheAge.IsZero() {
		syncBadge = "  " + dimStyle.Render("synced "+formatAge(m.boardsCacheAge))
	}
	sub := subtitleStyle.Render(userStr) + syncBadge

	// Search bar (always shown, always focused).
	searchBar := focusedInputStyle.Width(w - 8).Render(m.boardSearch.View())

	// Filter + paginate.
	filtered := m.filteredBoards()
	pageSize := m.boardPageSize()

	cursor := m.boardCursor
	if cursor >= len(filtered) {
		cursor = maxInt(0, len(filtered)-1)
	}
	offset := m.boardOffset
	end := offset + pageSize
	if end > len(filtered) {
		end = len(filtered)
	}

	// Pagination indicator.
	var pageInfo string
	switch {
	case len(m.boards) == 0:
		pageInfo = dimStyle.Render("No boards found on this site")
	case len(filtered) == 0:
		pageInfo = dimStyle.Render("No boards match your search")
	case len(filtered) > pageSize:
		pageInfo = subtitleStyle.Render(fmt.Sprintf("%d – %d  of  %d boards", offset+1, end, len(filtered)))
	default:
		pageInfo = subtitleStyle.Render(fmt.Sprintf("%d board(s)", len(filtered)))
	}

	// Board rows.
	var rows []string
	for i := offset; i < end; i++ {
		b := filtered[i]
		cur := "  "
		style := dimStyle
		if i == cursor {
			cur = cursorStyle.Render("▶ ")
			style = selectedItemStyle
		}
		nameW := w - 16
		row := cur + style.Render(truncate(b.Name, nameW)) + "  " + dimStyle.Render("["+b.ProjectKey+"]")
		rows = append(rows, row)
	}

	// Scroll arrows.
	var scrollHint string
	if offset > 0 && end < len(filtered) {
		scrollHint = dimStyle.Render("  ↑ more above  ·  ↓ more below")
	} else if offset > 0 {
		scrollHint = dimStyle.Render("  ↑ more above")
	} else if end < len(filtered) {
		scrollHint = dimStyle.Render("  ↓ more below")
	}

	// Manual key entry overlay.
	manual := ""
	if m.manualKeyMode {
		manual = "\n" + subtitleStyle.Render("Enter project key:") + "\n" +
			focusedInputStyle.Width(24).Render(m.manualKeyInput.View()) +
			"\n" + dimStyle.Render("Enter confirm  •  Esc cancel")
	}

	errLine := ""
	if m.err != nil {
		errLine = "\n" + errorStyle.Render("⚠  "+m.err.Error())
	}

	footerText := "↑↓/jk navigate  •  PgUp/PgDn jump  •  Type to search  •  Esc clear  •  Enter select"
	if len(m.boards) == 0 || m.manualKeyMode {
		footerText = "Enter project key  •  Esc cancel  •  Ctrl+C quit"
	} else {
		footerText += "  •  M manual key  •  R refresh"
	}
	footer := footerStyle.Width(w).Render(footerText)

	parts := []string{title, sub, "", searchBar, "", pageInfo}
	if len(rows) > 0 {
		parts = append(parts, strings.Join(rows, "\n"))
	}
	if scrollHint != "" {
		parts = append(parts, scrollHint)
	}
	if manual != "" {
		parts = append(parts, manual)
	}
	parts = append(parts, errLine, footer)

	body := lipgloss.JoinVertical(lipgloss.Left, parts...)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Top,
		panelStyle.Width(w).Render(body))
}

// ── Settings view ─────────────────────────────────────────────────────────────

func (m model) viewSettings() string {
	title := titleStyle.Render("Settings")
	sub := subtitleStyle.Render(fmt.Sprintf("Project: %s", m.projectKey))
	if m.selectedBoard.Name != "" {
		sub = subtitleStyle.Render(fmt.Sprintf("Project: %s  ·  Board: %s", m.projectKey, m.selectedBoard.Name))
	}

	rowStyle := func(idx int) lipgloss.Style {
		if m.settingsCursor == idx {
			return selectedItemStyle
		}
		return dimStyle
	}

	afVal := fmt.Sprintf("‹ %s / %s ›",
		radio(m.assigneeFallback == "unassigned", "unassigned"),
		radio(m.assigneeFallback == "requester", fmt.Sprintf("requester (%s)", m.user.DisplayName)),
	)
	itVal := fmt.Sprintf("‹ %s ›", m.issueType)

	af := rowStyle(0).Render("  Assignee fallback  ") + afVal
	it := rowStyle(1).Render("  Issue type         ") + itVal

	footer := footerStyle.Width(54).Render("↑↓ navigate  •  ←→/Space toggle  •  Enter continue  •  Ctrl+C quit")

	body := lipgloss.JoinVertical(lipgloss.Left,
		title, sub, "", af, "", it, "", footer)

	w := clamp(m.width-8, 58, 84)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		panelStyle.Width(w).Render(body))
}

// ── Tickets view ──────────────────────────────────────────────────────────────

func (m model) viewTickets() string {
	halfW := m.width/2 - 3

	// Left: ticket list
	var rows []string
	for i, t := range m.tickets {
		checked := checkStyle.Render("☑")
		if !m.selectedTickets[i] {
			checked = dimStyle.Render("☐")
		}
		cursor := "  "
		nameStyle := lipgloss.NewStyle()
		if i == m.ticketCursor {
			cursor = cursorStyle.Render("▶ ")
			nameStyle = selectedItemStyle
		} else if !m.selectedTickets[i] {
			nameStyle = dimStyle
		}
		rows = append(rows, cursor+checked+" "+nameStyle.Render(truncate(t.Title, halfW-6)))
	}

	nSel := m.countSelected()
	listHeader := titleStyle.Render(fmt.Sprintf("Tickets  %s/%d",
		successStyle.Render(fmt.Sprintf("%d", nSel)), len(m.tickets)))
	leftPanel := panelStyle.Width(halfW).Height(m.height - 6).Render(
		lipgloss.JoinVertical(lipgloss.Left, listHeader, "", strings.Join(rows, "\n")))

	// Right: preview
	var previewLines []string
	if m.ticketCursor < len(m.tickets) {
		t := m.tickets[m.ticketCursor]
		previewLines = append(previewLines,
			titleStyle.Render(truncate(t.Title, halfW-4)),
			"",
		)
		if t.Description != "" {
			// Wrap description manually
			words := strings.Fields(t.Description)
			line := ""
			maxW := halfW - 4
			for _, w := range words {
				if len(line)+len(w)+1 > maxW {
					previewLines = append(previewLines, line)
					line = w
				} else {
					if line == "" {
						line = w
					} else {
						line += " " + w
					}
				}
			}
			if line != "" {
				previewLines = append(previewLines, line)
			}
			previewLines = append(previewLines, "")
		}
		if t.Assignee != "" {
			previewLines = append(previewLines, subtitleStyle.Render("Assignee"), t.Assignee, "")
		}
		if len(t.Labels) > 0 {
			var lbls []string
			for _, l := range t.Labels {
				lbls = append(lbls, labelStyle.Render(l))
			}
			previewLines = append(previewLines, subtitleStyle.Render("Labels"), strings.Join(lbls, " "))
		}
	}
	rightPanel := activePanelStyle.Width(halfW).Height(m.height - 6).Render(
		lipgloss.JoinVertical(lipgloss.Left,
			titleStyle.Render("Preview"),
			"",
			strings.Join(previewLines, "\n"),
		))

	top := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)

	footer := footerStyle.Width(m.width - 4).Render(
		"↑↓/jk navigate  •  Space toggle  •  a select/deselect all  •  Enter create selected  •  Ctrl+C quit")

	return lipgloss.JoinVertical(lipgloss.Left, top, footer)
}

// ── Creating view ─────────────────────────────────────────────────────────────

func (m model) viewCreating() string {
	title := titleStyle.Render("Creating Tickets")
	bar := progressBar(44, m.progress) + fmt.Sprintf("  %d%%", int(m.progress*100))

	var rows []string
	for i, t := range m.tickets {
		if !m.selectedTickets[i] {
			continue
		}
		r := m.results[i]
		var status string
		switch {
		case r.Key != "":
			status = successStyle.Render(fmt.Sprintf("✓ %-12s", r.Key))
		case r.Err != nil:
			status = errorStyle.Render("✗ failed      ")
		case i == m.creating:
			status = m.spinner.View() + " creating…   "
		default:
			status = dimStyle.Render("· queued       ")
		}
		rows = append(rows, "  "+status+"  "+truncate(t.Title, 42))
	}

	body := lipgloss.JoinVertical(lipgloss.Left,
		title, "", bar, "", strings.Join(rows, "\n"))

	w := clamp(m.width-8, 60, 84)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		panelStyle.Width(w).Render(body))
}

// ── Done view ─────────────────────────────────────────────────────────────────

func (m model) viewDone() string {
	created, failed := 0, 0
	var rows []string
	for i, r := range m.results {
		if !m.selectedTickets[i] {
			continue
		}
		if r.Key != "" {
			created++
			rows = append(rows, successStyle.Render("✓")+" "+
				dimStyle.Render(fmt.Sprintf("%-12s", r.Key))+" "+r.URL)
		} else if r.Err != nil {
			failed++
			rows = append(rows, errorStyle.Render("✗")+" "+
				truncate(r.Ticket.Title, 36)+"  "+dimStyle.Render(r.Err.Error()))
		}
	}

	failStr := dimStyle.Render("0")
	if failed > 0 {
		failStr = errorStyle.Render(fmt.Sprintf("%d", failed))
	}
	summary := fmt.Sprintf("Created: %s   Failed: %s",
		successStyle.Render(fmt.Sprintf("%d", created)), failStr)

	footer := footerStyle.Width(54).Render("Any key to exit")

	body := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("✓ Done"), "", summary, "",
		strings.Join(rows, "\n"), "", footer)

	w := clamp(m.width-8, 60, 90)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		panelStyle.Width(w).Render(body))
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func (m model) countSelected() int {
	n := 0
	for _, v := range m.selectedTickets {
		if v {
			n++
		}
	}
	return n
}

func (m model) countDone() int {
	n := 0
	for i, r := range m.results {
		if m.selectedTickets[i] && (r.Key != "" || r.Err != nil) {
			n++
		}
	}
	return n
}

func radio(active bool, label string) string {
	if active {
		return successStyle.Render("● " + label)
	}
	return dimStyle.Render("○ " + label)
}

func nextIn(list []string, cur string) string {
	for i, v := range list {
		if v == cur {
			return list[(i+1)%len(list)]
		}
	}
	return list[0]
}

func prevIn(list []string, cur string) string {
	for i, v := range list {
		if v == cur {
			return list[(i-1+len(list))%len(list)]
		}
	}
	return list[0]
}

func clamp(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
