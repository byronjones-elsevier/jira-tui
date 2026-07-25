package main

import (
	"database/sql"
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
	screenAuth          screen = iota // credential entry
	screenVerify                      // verifying creds (spinner)
	screenBoards                      // board list / manual key entry
	screenSettings                    // assignee fallback + issue type
	screenTickets                     // ticket list + preview
	screenCreating                    // creation progress
	screenDone                        // results summary
	screenEpicSetup                   // enter epic title / desc / requester
	screenEpicDupWarn                 // confirm when a matching epic already exists
	screenDupCheck                    // per-ticket duplicate decision
	screenShowTickets                 // browse local history
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

type boardsSyncedMsg struct {
	boards []Board
	err    error
}

type ticketCreatedMsg struct {
	index        int
	key          string
	err          error
	assigneeWarn string // non-empty when requested assignee could not be resolved
}

type epicCreatedMsg struct {
	key string
	url string
	err error
}

type historyLoadedMsg struct {
	records []TicketRecord
	err     error
}

// ── dupCheckItem holds a ticket whose title+description matches a history record. ─

type dupCheckItem struct {
	ticketIdx    int
	ticket       Ticket
	dups         []TicketRecord
	createAnyway bool // default false = skip
}

// ── Model ─────────────────────────────────────────────────────────────────────

var issueTypes = []string{"Task", "Story", "Bug", "Subtask", "Epic"}

type model struct {
	// Core
	screen  screen
	mode    appMode
	config  Config
	tickets []Ticket
	db      *sql.DB
	width   int
	height  int

	// Jira
	client        *JiraClient
	user          User
	boards        []Board
	selectedBoard Board
	projectKey    string

	// Auth inputs
	authInputs   []textinput.Model
	focusedInput int

	// Boards
	boardCursor    int
	boardOffset    int
	boardSearch    textinput.Model
	manualKeyMode  bool
	manualKeyInput textinput.Model
	boardsSyncing  bool
	boardsCacheAge time.Time

	// Settings (0 = assignee fallback, 1 = issue type)
	settingsCursor   int
	assigneeFallback string
	issueType        string

	// Tickets
	ticketCursor    int
	selectedTickets map[int]bool

	// Creation
	creating    int // index of ticket currently being created
	results     []CreateResult
	progress    float64
	epicPending bool // waiting for epic creation before starting tickets

	// Epic setup (screenEpicSetup)
	epicInputs    []textinput.Model
	epicFocus     int
	epicTitle     string
	epicDesc      string
	epicReq       string
	epicKey       string // set after successful creation
	epicURL       string
	existingEpics []TicketRecord // populated when same-title epic found

	// Dup check (screenDupCheck)
	dupItems  []dupCheckItem
	dupCursor int

	// Done (screenDone)
	doneOffset int

	// Show tickets (screenShowTickets)
	histRecords []TicketRecord
	histCursor  int
	histOffset  int
	histLoading bool

	// UI helpers
	spinner    spinner.Model
	loading    bool
	loadingMsg string
	err        error
}

// ── Constructor ───────────────────────────────────────────────────────────────

func newModel(cfg Config, tickets []Ticket, db *sql.DB, mode appMode) model {
	// Auth inputs: URL, email, token.
	authInps := make([]textinput.Model, 3)
	for i, ph := range []string{
		"https://company.atlassian.net",
		"you@company.com",
		"API token (input hidden)",
	} {
		t := textinput.New()
		t.CharLimit = 256
		t.Placeholder = ph
		authInps[i] = t
	}
	authInps[0].SetValue(cfg.BaseURL)
	authInps[1].SetValue(cfg.Email)
	authInps[2].EchoMode = textinput.EchoPassword
	authInps[2].SetValue(cfg.APIToken)

	manualInput := textinput.New()
	manualInput.Placeholder = "e.g. FINOPS"
	manualInput.CharLimit = 32

	boardSearch := textinput.New()
	boardSearch.Placeholder = "Search boards…"
	boardSearch.CharLimit = 100

	// Epic setup inputs: title, description, requester.
	epicInps := make([]textinput.Model, 3)
	for i, ph := range []string{"Epic title", "Description", "Requester name or email"} {
		t := textinput.New()
		t.CharLimit = 256
		t.Placeholder = ph
		epicInps[i] = t
	}

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
		mode:             mode,
		db:               db,
		tickets:          tickets,
		authInputs:       authInps,
		manualKeyInput:   manualInput,
		boardSearch:      boardSearch,
		epicInputs:       epicInps,
		spinner:          s,
		selectedTickets:  selected,
		assigneeFallback: af,
		issueType:        it,
		results:          make([]CreateResult, len(tickets)),
	}

	// --show-tickets: skip Jira auth entirely.
	if mode == modeShow {
		m.screen = screenShowTickets
		m.histLoading = true
		return m
	}

	hasCreds := cfg.BaseURL != "" && cfg.Email != "" && cfg.APIToken != ""
	if hasCreds {
		m.screen = screenVerify
		m.loading = true
		m.loadingMsg = "Verifying saved credentials…"
		m.client = newJiraClient(cfg.BaseURL, cfg.Email, cfg.APIToken)
	} else {
		m.screen = screenAuth
		authInps[0].Focus()
	}

	return m
}

// ── Init ──────────────────────────────────────────────────────────────────────

func (m model) Init() tea.Cmd {
	if m.mode == modeShow {
		return tea.Batch(m.spinner.Tick, m.cmdLoadHistory())
	}
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
	parentKey := m.epicKey // empty in normal mode

	return func() tea.Msg {
		assigneeID := ""
		var assigneeWarn string
		if ticket.Assignee != "" {
			id, err := client.ResolveAccountID(ticket.Assignee)
			if err != nil || id == "" {
				assigneeWarn = ticket.Assignee
			}
			assigneeID = id
		}
		if assigneeID == "" && fallback == "requester" {
			assigneeID = requesterID
		}
		key, err := client.CreateIssue(projectKey, issueType, ticket, assigneeID, parentKey)
		return ticketCreatedMsg{index: index, key: key, err: err, assigneeWarn: assigneeWarn}
	}
}

func (m model) cmdCreateEpic() tea.Cmd {
	client := m.client
	projectKey := m.projectKey
	title, desc, req := m.epicTitle, m.epicDesc, m.epicReq

	return func() tea.Msg {
		key, err := client.CreateEpic(projectKey, title, desc, req)
		url := ""
		if err == nil {
			url = client.baseURL + "/browse/" + key
		}
		return epicCreatedMsg{key: key, url: url, err: err}
	}
}

func (m model) cmdLoadHistory() tea.Cmd {
	db := m.db
	return func() tea.Msg {
		records, err := allTickets(db)
		return historyLoadedMsg{records: records, err: err}
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
			return m, m.authInputs[m.focusedInput].Focus()
		}
		m.user = msg.user
		m.config.BaseURL = m.client.baseURL
		m.config.Email = m.client.email
		m.config.APIToken = m.client.token
		_ = saveConfig(m.config)
		m.screen = screenBoards
		if cached, cacheAge, ok := loadBoardsCache(m.client.baseURL); ok {
			m.boards = cached
			m.boardsCacheAge = cacheAge
			m.loading = false
			m.boardsSyncing = true
			return m, tea.Batch(m.cmdSyncBoards(), m.boardSearch.Focus())
		}
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
			filtered := m.filteredBoards()
			if m.boardCursor >= len(filtered) {
				m.boardCursor = maxInt(0, len(filtered)-1)
				m.boardOffset = maxInt(0, m.boardCursor-m.boardPageSize()+1)
			}
		}
		return m, nil

	case epicCreatedMsg:
		m.epicPending = false
		if msg.err != nil {
			m.err = fmt.Errorf("failed to create epic: %v", msg.err)
			m.screen = screenEpicSetup
			return m, m.epicInputs[0].Focus()
		}
		m.epicKey = msg.key
		m.epicURL = msg.url
		// Save epic to local history.
		_ = insertTicket(m.db, TicketRecord{
			Title:       m.epicTitle,
			Description: m.epicDesc,
			JiraKey:     msg.key,
			URL:         msg.url,
			CreatedAt:   time.Now(),
			TicketType:  "Epic",
			ProjectKey:  m.projectKey,
		})
		// Start creating child tickets, or finish if none are selected.
		first := m.firstSelected()
		if first >= 0 {
			m.creating = first
			return m, m.cmdCreateTicket(first)
		}
		m.progress = 1.0
		m.screen = screenDone
		return m, nil

	case ticketCreatedMsg:
		result := CreateResult{
			Ticket:       m.tickets[msg.index],
			Key:          msg.key,
			URL:          m.config.BaseURL + "/browse/" + msg.key,
			Err:          msg.err,
			AssigneeWarn: msg.assigneeWarn,
		}
		m.results[msg.index] = result
		if msg.err == nil && msg.key != "" {
			parentKey, parentURL := "", ""
			if m.mode == modeEpic {
				parentKey = m.epicKey
				parentURL = m.epicURL
			}
			_ = insertTicket(m.db, TicketRecord{
				Title:       m.tickets[msg.index].Title,
				Description: m.tickets[msg.index].Description,
				JiraKey:     msg.key,
				URL:         result.URL,
				CreatedAt:   time.Now(),
				TicketType:  m.issueType,
				ParentKey:   parentKey,
				ParentURL:   parentURL,
				ProjectKey:  m.projectKey,
				Assignee:    m.tickets[msg.index].Assignee,
				Labels:      m.tickets[msg.index].Labels,
			})
		}
		// Find the next selected ticket to create.
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

	case historyLoadedMsg:
		m.histLoading = false
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.histRecords = msg.records
		}
		return m, nil
	}

	return m, nil
}

// ── handleKey dispatch ────────────────────────────────────────────────────────

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
	case screenEpicSetup:
		return m.handleEpicSetupKey(msg)
	case screenEpicDupWarn:
		return m.handleEpicDupWarnKey(msg)
	case screenDupCheck:
		return m.handleDupCheckKey(msg)
	case screenShowTickets:
		return m.handleShowTicketsKey(msg)
	case screenDone:
		return m.handleDoneKey(msg)
	}
	return m, nil
}

// ── Done keys ─────────────────────────────────────────────────────────────────

func (m model) handleDoneKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc", "enter":
		return m, tea.Quit
	case "up", "k":
		if m.doneOffset > 0 {
			m.doneOffset--
		}
	case "down", "j":
		if m.doneOffset < m.doneMaxOffset() {
			m.doneOffset++
		}
	case "pgup", "ctrl+b":
		m.doneOffset -= m.donePageSize()
		if m.doneOffset < 0 {
			m.doneOffset = 0
		}
	case "pgdown", "ctrl+f":
		m.doneOffset += m.donePageSize()
		if max := m.doneMaxOffset(); m.doneOffset > max {
			m.doneOffset = max
		}
	}
	return m, nil
}

func (m model) donePageSize() int {
	n := m.height - 10
	if n < 4 {
		n = 4
	}
	return n
}

func (m model) doneRowCount() int {
	n := 0
	if m.mode == modeEpic && m.epicKey != "" {
		n++
	}
	for i, r := range m.results {
		if !m.selectedTickets[i] {
			continue
		}
		if r.Key != "" || r.Err != nil {
			n++
		}
	}
	return n
}

func (m model) doneMaxOffset() int {
	if max := m.doneRowCount() - m.donePageSize(); max > 0 {
		return max
	}
	return 0
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

func (m model) boardPageSize() int {
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
	m.err = nil

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

	if m.boardCursor >= len(filtered) {
		m.boardCursor = maxInt(0, len(filtered)-1)
	}

	switch msg.String() {
	case "up", "k":
		if m.boardCursor > 0 {
			m.boardCursor--
		}
		if m.boardCursor < m.boardOffset {
			m.boardOffset = m.boardCursor
		}

	case "down", "j":
		if m.boardCursor < len(filtered)-1 {
			m.boardCursor++
		}
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
		if m.boardSearch.Value() == "" && !m.boardsSyncing {
			m.boardsSyncing = true
			return m, m.cmdSyncBoards()
		}
		prevVal := m.boardSearch.Value()
		var cmd tea.Cmd
		m.boardSearch, cmd = m.boardSearch.Update(msg)
		if m.boardSearch.Value() != prevVal {
			m.boardCursor, m.boardOffset = 0, 0
		}
		return m, cmd

	case "ctrl+m", "m":
		if m.boardSearch.Value() == "" {
			m.manualKeyMode = true
			m.manualKeyInput.SetValue("")
			m.boardSearch.Blur()
			return m, m.manualKeyInput.Focus()
		}
		fallthrough

	default:
		prevVal := m.boardSearch.Value()
		var cmd tea.Cmd
		m.boardSearch, cmd = m.boardSearch.Update(msg)
		if m.boardSearch.Value() != prevVal {
			m.boardCursor, m.boardOffset = 0, 0
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
	case "esc":
		m.screen = screenBoards
	case "enter":
		if m.mode == modeEpic {
			m.screen = screenEpicSetup
			return m, m.epicInputs[0].Focus()
		}
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
	case "esc":
		m.screen = screenSettings
	case "enter":
		// In epic mode we can proceed with zero selected tickets (epic-only).
		if m.mode != modeEpic && m.countSelected() == 0 {
			return m, nil
		}
		m.config.AssigneeFallback = m.assigneeFallback
		m.config.IssueType = m.issueType
		_ = saveConfig(m.config)
		return m.checkDupsAndProceed()
	}
	return m, nil
}

// checkDupsAndProceed queries the DB for duplicates across all selected tickets.
// If any are found it transitions to screenDupCheck; otherwise starts creation.
func (m model) checkDupsAndProceed() (model, tea.Cmd) {
	var items []dupCheckItem
	for i := range m.tickets {
		if !m.selectedTickets[i] {
			continue
		}
		dups, err := findDuplicates(m.db, m.tickets[i].Title, m.tickets[i].Description)
		if err != nil {
			m.err = fmt.Errorf("duplicate check failed: %w", err)
			return m, nil
		}
		if len(dups) > 0 {
			items = append(items, dupCheckItem{
				ticketIdx:    i,
				ticket:       m.tickets[i],
				dups:         dups,
				createAnyway: false,
			})
		}
	}
	if len(items) > 0 {
		m.dupItems = items
		m.dupCursor = 0
		m.screen = screenDupCheck
		return m, nil
	}
	return m.startCreation()
}

// startCreation begins the screenCreating flow.
func (m model) startCreation() (model, tea.Cmd) {
	if m.mode == modeEpic {
		m.screen = screenCreating
		m.epicPending = true
		return m, m.cmdCreateEpic()
	}
	first := m.firstSelected()
	if first < 0 {
		// Nothing selected — skip creation and go straight to the done screen.
		m.progress = 1.0
		m.screen = screenDone
		return m, nil
	}
	m.screen = screenCreating
	m.creating = first
	return m, m.cmdCreateTicket(first)
}

// firstSelected returns the index of the first selected ticket, or -1.
func (m model) firstSelected() int {
	for i := range m.tickets {
		if m.selectedTickets[i] {
			return i
		}
	}
	return -1
}

// ── Epic setup keys ───────────────────────────────────────────────────────────

func (m model) handleEpicSetupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg.String() {
	case "tab", "down":
		m.epicInputs[m.epicFocus].Blur()
		m.epicFocus = (m.epicFocus + 1) % len(m.epicInputs)
		cmds = append(cmds, m.epicInputs[m.epicFocus].Focus())

	case "shift+tab", "up":
		m.epicInputs[m.epicFocus].Blur()
		m.epicFocus = (m.epicFocus - 1 + len(m.epicInputs)) % len(m.epicInputs)
		cmds = append(cmds, m.epicInputs[m.epicFocus].Focus())

	case "enter":
		if m.epicFocus < len(m.epicInputs)-1 {
			m.epicInputs[m.epicFocus].Blur()
			m.epicFocus++
			cmds = append(cmds, m.epicInputs[m.epicFocus].Focus())
		} else {
			title := strings.TrimSpace(m.epicInputs[0].Value())
			if title == "" {
				m.err = fmt.Errorf("epic title is required")
				break
			}
			m.err = nil
			m.epicTitle = title
			m.epicDesc = strings.TrimSpace(m.epicInputs[1].Value())
			m.epicReq = strings.TrimSpace(m.epicInputs[2].Value())

			existing, _ := findEpicsByTitle(m.db, title)
			if len(existing) > 0 {
				m.existingEpics = existing
				m.screen = screenEpicDupWarn
				return m, nil
			}
			m.screen = screenTickets
			return m, nil
		}

	case "esc":
		m.epicInputs[m.epicFocus].Blur()
		m.screen = screenSettings
		return m, nil

	default:
		var cmd tea.Cmd
		m.epicInputs[m.epicFocus], cmd = m.epicInputs[m.epicFocus].Update(msg)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

// ── Epic dup-warn keys ────────────────────────────────────────────────────────

func (m model) handleEpicDupWarnKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		m.screen = screenTickets
		return m, nil
	case "n", "N", "esc":
		m.screen = screenEpicSetup
		return m, m.epicInputs[0].Focus()
	}
	return m, nil
}

// ── Dup-check keys ────────────────────────────────────────────────────────────

func (m model) handleDupCheckKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.dupCursor > 0 {
			m.dupCursor--
		}
	case "down", "j":
		if m.dupCursor < len(m.dupItems)-1 {
			m.dupCursor++
		}
	case " ", "c":
		m.dupItems[m.dupCursor].createAnyway = !m.dupItems[m.dupCursor].createAnyway
	case "s":
		m.dupItems[m.dupCursor].createAnyway = false
	case "enter":
		// Apply skip decisions then proceed.
		for _, item := range m.dupItems {
			if !item.createAnyway {
				m.selectedTickets[item.ticketIdx] = false
			}
		}
		return m.startCreation()
	case "esc":
		m.screen = screenTickets
	}
	return m, nil
}

// ── Show-tickets keys ─────────────────────────────────────────────────────────

func (m model) handleShowTicketsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.histLoading {
		return m, nil
	}
	switch msg.String() {
	case "up", "k":
		if m.histCursor > 0 {
			m.histCursor--
			if m.histCursor < m.histOffset {
				m.histOffset = m.histCursor
			}
		}
	case "down", "j":
		if m.histCursor < len(m.histRecords)-1 {
			m.histCursor++
			ps := m.histPageSize()
			if m.histCursor >= m.histOffset+ps {
				m.histOffset = m.histCursor - ps + 1
			}
		}
	case "pgup", "ctrl+b":
		ps := m.histPageSize()
		m.histCursor -= ps
		if m.histCursor < 0 {
			m.histCursor = 0
		}
		m.histOffset -= ps
		if m.histOffset < 0 {
			m.histOffset = 0
		}
	case "pgdown", "ctrl+f":
		ps := m.histPageSize()
		m.histCursor += ps
		if m.histCursor >= len(m.histRecords) {
			m.histCursor = maxInt(0, len(m.histRecords)-1)
		}
		m.histOffset += ps
		maxOff := maxInt(0, len(m.histRecords)-ps)
		if m.histOffset > maxOff {
			m.histOffset = maxOff
		}
	case "q", "esc", "enter":
		return m, tea.Quit
	}
	return m, nil
}

func (m model) histPageSize() int {
	n := m.height - 10
	if n < 4 {
		n = 4
	}
	if n > 30 {
		n = 30
	}
	return n
}

// ── View dispatch ─────────────────────────────────────────────────────────────

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
	case screenEpicSetup:
		return m.viewEpicSetup()
	case screenEpicDupWarn:
		return m.viewEpicDupWarn()
	case screenDupCheck:
		return m.viewDupCheck()
	case screenShowTickets:
		return m.viewShowTickets()
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

	userStr := "Authenticated as " + m.user.DisplayName
	var syncBadge string
	if m.boardsSyncing {
		syncBadge = "  " + m.spinner.View() + dimStyle.Render(" syncing…")
	} else if !m.boardsCacheAge.IsZero() {
		syncBadge = "  " + dimStyle.Render("synced "+formatAge(m.boardsCacheAge))
	}
	sub := subtitleStyle.Render(userStr) + syncBadge

	searchBar := focusedInputStyle.Width(w - 8).Render(m.boardSearch.View())

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

	var scrollHint string
	if offset > 0 && end < len(filtered) {
		scrollHint = dimStyle.Render("  ↑ more above  ·  ↓ more below")
	} else if offset > 0 {
		scrollHint = dimStyle.Render("  ↑ more above")
	} else if end < len(filtered) {
		scrollHint = dimStyle.Render("  ↓ more below")
	}

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

	footerText := "↑↓/jk navigate  •  PgUp/PgDn jump  •  Type to search  •  Enter select  •  M manual key  •  R refresh"
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

	itLabel := "  Issue type         "
	if m.mode == modeEpic {
		itLabel = "  Child issue type   "
	}

	af := rowStyle(0).Render("  Assignee fallback  ") + afVal
	it := rowStyle(1).Render(itLabel) + itVal

	enterHint := "Enter continue"
	if m.mode == modeEpic {
		enterHint = "Enter set up epic"
	}
	footer := footerStyle.Width(58).Render(fmt.Sprintf("↑↓ navigate  •  ←→/Space toggle  •  %s  •  Ctrl+C quit", enterHint))

	body := lipgloss.JoinVertical(lipgloss.Left,
		title, sub, "", af, "", it, "", footer)

	w := clamp(m.width-8, 58, 84)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		panelStyle.Width(w).Render(body))
}

// ── Tickets view ──────────────────────────────────────────────────────────────

func (m model) viewTickets() string {
	halfW := m.width/2 - 3

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
	headerText := "Tickets"
	if m.mode == modeEpic && m.epicTitle != "" {
		headerText = "Subtasks for: " + truncate(m.epicTitle, 28)
	}
	listHeader := titleStyle.Render(fmt.Sprintf("%s  %s/%d",
		headerText, successStyle.Render(fmt.Sprintf("%d", nSel)), len(m.tickets)))
	leftPanel := panelStyle.Width(halfW).Height(m.height - 6).Render(
		lipgloss.JoinVertical(lipgloss.Left, listHeader, "", strings.Join(rows, "\n")))

	var previewLines []string
	if m.ticketCursor < len(m.tickets) {
		t := m.tickets[m.ticketCursor]
		previewLines = append(previewLines,
			titleStyle.Render(truncate(t.Title, halfW-4)),
			"",
		)
		if t.Description != "" {
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

// ── Dup-check view ────────────────────────────────────────────────────────────

func (m model) viewDupCheck() string {
	var rows []string
	for i, item := range m.dupItems {
		cursor := "  "
		if i == m.dupCursor {
			cursor = cursorStyle.Render("▶ ")
		}

		action := errorStyle.Render("[SKIP]   ")
		if item.createAnyway {
			action = successStyle.Render("[CREATE] ")
		}

		titleLine := action + truncate(item.ticket.Title, 46)
		dupInfo := ""
		if len(item.dups) > 0 {
			d := item.dups[0]
			dupInfo = "     " + dimStyle.Render(fmt.Sprintf("existing: %s  created %s",
				d.JiraKey, d.CreatedAt.Format("2006-01-02")))
		}

		rows = append(rows, cursor+titleLine)
		if dupInfo != "" {
			rows = append(rows, dupInfo)
		}
		rows = append(rows, "")
	}

	w := clamp(m.width-8, 64, 90)
	footer := footerStyle.Width(w).Render(
		"↑↓/jk navigate  •  Space/C create anyway  •  S skip  •  Enter proceed  •  Esc back")

	body := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("Duplicate Tickets Found"),
		subtitleStyle.Render("These tickets already exist in history — skip or create anyway."),
		"",
		strings.Join(rows, "\n"),
		footer,
	)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		panelStyle.Width(w).Render(body))
}

// ── Epic-setup view ───────────────────────────────────────────────────────────

func (m model) viewEpicSetup() string {
	labels := []string{"Epic Title", "Description", "Requester"}
	var rows []string
	for i, inp := range m.epicInputs {
		style := blurredInputStyle
		if i == m.epicFocus {
			style = focusedInputStyle
		}
		rows = append(rows, subtitleStyle.Render(labels[i])+"\n"+style.Width(46).Render(inp.View()))
	}

	errLine := ""
	if m.err != nil {
		errLine = "\n" + errorStyle.Render("⚠  "+m.err.Error())
	}

	footer := footerStyle.Width(54).Render(
		"Tab/↑↓ move field  •  Enter next/confirm  •  Esc back  •  Ctrl+C quit")

	body := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("Create Epic"),
		subtitleStyle.Render(fmt.Sprintf("Project: %s", m.projectKey)),
		"",
		strings.Join(rows, "\n\n"),
		errLine,
		"",
		footer,
	)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		panelStyle.Width(54).Render(body))
}

// ── Epic-dup-warn view ────────────────────────────────────────────────────────

func (m model) viewEpicDupWarn() string {
	var rows []string
	for _, r := range m.existingEpics {
		rows = append(rows, "  "+keyStyle.Render(r.JiraKey)+"  "+
			truncate(r.Title, 36)+"  "+
			dimStyle.Render(r.CreatedAt.Format("2006-01-02")))
	}

	w := clamp(m.width-8, 52, 76)
	body := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("Epic Already Exists"),
		"",
		subtitleStyle.Render("An epic with this title was already created:"),
		"",
		strings.Join(rows, "\n"),
		"",
		subtitleStyle.Render("Create another epic anyway?"),
		"",
		footerStyle.Width(w).Render("[Y] Yes, create  •  [N] Cancel  •  Ctrl+C quit"),
	)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		panelStyle.Width(w).Render(body))
}

// ── Creating view ─────────────────────────────────────────────────────────────

func (m model) viewCreating() string {
	title := titleStyle.Render("Creating Tickets")
	w := clamp(m.width-8, 60, 84)
	barWidth := w - 14 // panel border (2) + padding (4) + "  100%" (6) + margin (2)
	if barWidth < 20 {
		barWidth = 20
	}
	bar := progressBar(barWidth, m.progress) + fmt.Sprintf("  %d%%", int(m.progress*100))

	var rows []string

	// Epic row (epic mode only).
	if m.mode == modeEpic {
		var epicStatus string
		switch {
		case m.epicPending:
			epicStatus = m.spinner.View() + " creating epic…   "
		case m.epicKey != "":
			epicStatus = successStyle.Render(fmt.Sprintf("✓ %-12s", m.epicKey))
		default:
			epicStatus = errorStyle.Render("✗ epic failed     ")
		}
		rows = append(rows, "  "+epicStatus+"  "+
			labelStyle.Render("EPIC")+"  "+truncate(m.epicTitle, 36))
	}

	for i, t := range m.tickets {
		if !m.selectedTickets[i] {
			continue
		}
		r := m.results[i]
		var status string
		switch {
		case r.Key != "" && r.AssigneeWarn != "":
			status = successStyle.Render(fmt.Sprintf("✓ %-12s", r.Key)) +
				errorStyle.Render(" !assignee")
		case r.Key != "":
			status = successStyle.Render(fmt.Sprintf("✓ %-12s", r.Key))
		case r.Err != nil:
			status = errorStyle.Render("✗ failed      ")
		case i == m.creating && !m.epicPending:
			status = m.spinner.View() + " creating…   "
		default:
			status = dimStyle.Render("· queued       ")
		}
		rows = append(rows, "  "+status+"  "+truncate(t.Title, 36))
	}

	body := lipgloss.JoinVertical(lipgloss.Left,
		title, "", bar, "", strings.Join(rows, "\n"))

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		panelStyle.Width(w).Render(body))
}

// ── Done view ─────────────────────────────────────────────────────────────────

func (m model) viewDone() string {
	created, failed := 0, 0
	var allRows []string

	if m.mode == modeEpic && m.epicKey != "" {
		allRows = append(allRows,
			successStyle.Render("✓")+" "+
				labelStyle.Render("EPIC")+" "+
				dimStyle.Render(fmt.Sprintf("%-12s", m.epicKey))+" "+m.epicURL)
	}

	for i, r := range m.results {
		if !m.selectedTickets[i] {
			continue
		}
		if r.Key != "" {
			created++
			allRows = append(allRows, successStyle.Render("✓")+" "+
				dimStyle.Render(fmt.Sprintf("%-12s", r.Key))+" "+r.URL)
		} else if r.Err != nil {
			failed++
			allRows = append(allRows, errorStyle.Render("✗")+" "+
				truncate(r.Ticket.Title, 36)+"  "+dimStyle.Render(r.Err.Error()))
		}
	}

	failStr := dimStyle.Render("0")
	if failed > 0 {
		failStr = errorStyle.Render(fmt.Sprintf("%d", failed))
	}
	summary := fmt.Sprintf("Created: %s   Failed: %s",
		successStyle.Render(fmt.Sprintf("%d", created)), failStr)

	w := clamp(m.width-8, 60, 90)

	ps := m.donePageSize()
	end := m.doneOffset + ps
	if end > len(allRows) {
		end = len(allRows)
	}
	visible := allRows[m.doneOffset:end]

	var scrollHint string
	if m.doneOffset > 0 && end < len(allRows) {
		scrollHint = dimStyle.Render("  ↑ more above  ·  ↓ more below")
	} else if m.doneOffset > 0 {
		scrollHint = dimStyle.Render("  ↑ more above")
	} else if end < len(allRows) {
		scrollHint = dimStyle.Render("  ↓ more below")
	}

	footer := footerStyle.Width(w).Render("↑↓/jk scroll  •  q / Esc / Enter to exit")

	parts := []string{titleStyle.Render("✓ Done"), "", summary, "", strings.Join(visible, "\n")}
	if scrollHint != "" {
		parts = append(parts, scrollHint)
	}
	parts = append(parts, "", footer)

	body := lipgloss.JoinVertical(lipgloss.Left, parts...)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		panelStyle.Width(w).Render(body))
}

// ── Show-tickets view ─────────────────────────────────────────────────────────

func (m model) viewShowTickets() string {
	if m.histLoading {
		return m.viewSpinner("Loading ticket history…")
	}

	w := clamp(m.width-8, 72, 110)
	title := titleStyle.Render("Ticket History")

	if len(m.histRecords) == 0 {
		body := lipgloss.JoinVertical(lipgloss.Left,
			title, "",
			dimStyle.Render("No tickets have been created yet."),
			"",
			footerStyle.Width(w).Render("q / Esc / Enter to quit"),
		)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			panelStyle.Width(w).Render(body))
	}

	ps := m.histPageSize()
	end := m.histOffset + ps
	if end > len(m.histRecords) {
		end = len(m.histRecords)
	}

	pageInfo := subtitleStyle.Render(fmt.Sprintf(
		"%d – %d  of  %d tickets", m.histOffset+1, end, len(m.histRecords)))

	var rows []string
	for i := m.histOffset; i < end; i++ {
		r := m.histRecords[i]
		cur := "  "
		nameStyle := lipgloss.NewStyle()
		if i == m.histCursor {
			cur = cursorStyle.Render("▶ ")
			nameStyle = selectedItemStyle
		}

		typeTag := labelStyle.Render(truncate(r.TicketType, 8))
		keyTag := keyStyle.Render(r.JiraKey)
		date := dimStyle.Render(r.CreatedAt.Format("2006-01-02"))

		parent := ""
		if r.ParentKey != "" {
			parent = dimStyle.Render("↳ "+r.ParentKey) + "  "
		}

		maxTitle := w - 42
		row := cur + keyTag + "  " + typeTag + "  " + parent + nameStyle.Render(truncate(r.Title, maxTitle)) + "  " + date
		rows = append(rows, row)
	}

	var scrollHint string
	if m.histOffset > 0 && end < len(m.histRecords) {
		scrollHint = dimStyle.Render("  ↑ more above  ·  ↓ more below")
	} else if m.histOffset > 0 {
		scrollHint = dimStyle.Render("  ↑ more above")
	} else if end < len(m.histRecords) {
		scrollHint = dimStyle.Render("  ↓ more below")
	}

	parts := []string{title, pageInfo, "", strings.Join(rows, "\n")}
	if scrollHint != "" {
		parts = append(parts, scrollHint)
	}
	parts = append(parts, footerStyle.Width(w).Render(
		"↑↓/jk navigate  •  PgUp/PgDn jump  •  q / Esc to quit"))

	body := lipgloss.JoinVertical(lipgloss.Left, parts...)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Top,
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
