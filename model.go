package main

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Screens ──────────────────────────────────────────────────────────────────

type screen int

const (
	screenAuth           screen = iota // credential entry
	screenVerify                       // verifying creds (spinner)
	screenBoards                       // board list / manual key entry
	screenSettings                     // assignee fallback + issue type
	screenTickets                      // ticket list + preview
	screenCreating                     // creation progress
	screenDone                         // results summary
	screenEpicSetup                    // enter epic title / desc / requester
	screenEpicDupWarn                  // confirm when a matching epic already exists
	screenDupCheck                     // per-ticket duplicate decision
	screenShowTickets                  // browse local history
	screenManualEntry                  // interactive single-ticket form (modeManual)
	screenManualContinue               // "add subtask or finish?" prompt after epic creation
	screenExportCSV                    // file-path input for Done-screen CSV export
	screenFirstRun                     // first-launch welcome / data-dir creation prompt
	screenEpicCSVQuery                 // loading spinner while querying epic + children
	screenEpicCSVReview                // scrollable preview of epic children, confirm before save
	screenEpicCSVPath                  // file-path input and save confirmation
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

type manualTicketCreatedMsg struct {
	key          string
	url          string
	ticket       Ticket
	issueType    string
	err          error
	assigneeWarn string
}

type epicQueryMsg struct {
	issue    *JiraIssue
	children []JiraIssue
	err      error
}

type jiraDeleteMsg struct {
	recordID int64
	err      error
}

type transitionsLoadedMsg struct {
	transitions   []Transition
	currentStatus string
	err           error
}

type transitionAppliedMsg struct {
	recordID int64
	status   string
	err      error
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
	creating         int // index of ticket currently being created
	results          []CreateResult
	progress         float64
	epicPending      bool // waiting for epic creation before starting tickets
	creationAborted  bool // user pressed Esc/q during screenCreating

	// Epic setup (screenEpicSetup)
	// epicInputs[0]=title, epicInputs[1]=requester; description is epicDescTA.
	epicInputs    []textinput.Model
	epicDescTA    textarea.Model
	epicFocus     int // 0=title 1=description 2=requester
	epicTitle     string
	epicDesc      string
	epicReq       string
	epicKey       string // set after successful creation
	epicURL       string
	existingEpics []TicketRecord // populated when same-title epic found

	// Manual entry (screenManualEntry / screenManualContinue)
	// manualInputs[0]=title, [1]=assignee, [2]=labels; description uses manualDesc.
	manualInputs     []textinput.Model
	manualDesc       textarea.Model
	manualTypeCursor int // index into issueTypes
	manualFocus      int // 0=title,1=desc,2=assignee,3=labels,4=issuetype
	manualResults    []CreateResult // accumulated results across manual loop
	manualParentKey  string         // non-empty when creating subtasks under an epic
	manualCreating   bool           // API call in flight

	// Dup check (screenDupCheck)
	dupItems  []dupCheckItem
	dupCursor int

	// Done (screenDone)
	doneCursor int
	doneOffset int

	// Export CSV (screenExportCSV)
	exportCSVResultInp     textinput.Model // path textinput on screenExportCSV
	exportCSVResultSaved   bool            // true after successful write
	exportCSVResultPath    string          // path of last successful write
	exportCSVResultErr     error           // last write error
	exportCSVResultConfirm bool            // true while waiting for overwrite y/n
	exportCSVResultPending string          // staged path awaiting overwrite confirm

	// Show tickets (screenShowTickets)
	histRecords           []TicketRecord
	histCursor            int
	histOffset            int
	histLoading           bool
	histSearch            textinput.Model
	histSortField         string // "date" | "key" | "title" | "type"
	histSortAsc           bool
	histConfirmDelete     bool  // true while waiting for y/n on local-only delete
	histConfirmDeleteJira bool  // true while waiting for y/n on Jira + local delete
	histJiraDeleteErr     error // set when a Jira delete fails

	histTransitions       []Transition // available transitions for the selected ticket
	histTransitionCursor  int
	histTransitionActive  bool         // true while the transition picker is shown
	histTransitionLoading bool         // true while fetching transitions
	histTransitionErr     error
	histTransitionCurrent string       // current status name fetched from Jira

	// UI helpers
	spinner    spinner.Model
	loading    bool
	loadingMsg string
	err        error
	firstRun   bool // true on first launch before data dir is created

	// Epic → CSV export (modeEpicToCSV)
	epicCSVKey      string          // the ticket key supplied on the command line
	epicCSVIssue    *JiraIssue      // fetched epic issue
	epicCSVChildren []JiraIssue     // child issues of the epic
	epicCSVCursor   int             // review-screen list cursor
	epicCSVOffset   int             // review-screen scroll offset
	epicCSVPathInp  textinput.Model // file-path text input on save screen
	epicCSVSaved    bool            // true after a successful CSV write
	epicCSVSavePath string          // path used for the last successful write
	epicCSVSaveErr  error           // last save error (nil when none)
}

// ── Constructor ───────────────────────────────────────────────────────────────

func newModel(cfg Config, tickets []Ticket, db *sql.DB, mode appMode, firstRun bool) model {
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

	// Epic setup inputs: title and requester (description uses a textarea).
	epicInps := make([]textinput.Model, 2)
	for i, ph := range []string{"Epic title", "Requester name or email"} {
		t := textinput.New()
		t.CharLimit = 256
		t.Placeholder = ph
		epicInps[i] = t
	}
	epicDesc := textarea.New()
	epicDesc.Placeholder = "Description (Enter for newlines, Tab to advance)"
	epicDesc.SetWidth(46)
	epicDesc.SetHeight(4)
	epicDesc.CharLimit = 2048

	// Manual entry form: title, assignee, labels inputs + separate description textarea.
	manualInps := make([]textinput.Model, 3)
	for i, ph := range []string{"Ticket title", "Assignee email or display name", "Labels (semicolon-separated)"} {
		t := textinput.New()
		t.CharLimit = 256
		t.Placeholder = ph
		manualInps[i] = t
	}
	manualInps[2].CharLimit = 512
	manualDesc := textarea.New()
	manualDesc.Placeholder = "Description (Enter for newlines, Tab to advance)"
	manualDesc.SetWidth(50)
	manualDesc.SetHeight(5)
	manualDesc.CharLimit = 4096

	epicCSVInp := textinput.New()
	epicCSVInp.Placeholder = "e.g. PROJ-123.csv"
	epicCSVInp.CharLimit = 256

	exportResultInp := textinput.New()
	exportResultInp.CharLimit = 256

	histSearchInp := textinput.New()
	histSearchInp.Placeholder = "Filter tickets…"
	histSearchInp.CharLimit = 100

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
		epicDescTA:       epicDesc,
		manualInputs:     manualInps,
		manualDesc:       manualDesc,
		epicCSVPathInp:     epicCSVInp,
		exportCSVResultInp: exportResultInp,
		histSearch:         histSearchInp,
		histSortField:      "date",
		histSortAsc:        false,
		spinner:            s,
		selectedTickets:  selected,
		assigneeFallback: af,
		issueType:        it,
		results:          make([]CreateResult, len(tickets)),
	}

	// First-run: show setup prompt before doing anything else.
	if firstRun {
		m.firstRun = true
		m.screen = screenFirstRun
		return m
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
		m.client.useADF = cfg.UseADF
	} else {
		m.screen = screenAuth
		authInps[0].Focus()
	}

	return m
}

// ── Init ──────────────────────────────────────────────────────────────────────

func (m model) Init() tea.Cmd {
	if m.screen == screenFirstRun {
		return nil
	}
	if m.mode == modeShow {
		return tea.Batch(textinput.Blink, m.spinner.Tick, m.cmdLoadHistory())
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

func (m model) cmdCreateManualTicket(t Ticket, issueType, parentKey string) tea.Cmd {
	client := m.client
	projectKey := m.projectKey
	return func() tea.Msg {
		var assigneeWarn string
		assigneeID := ""
		if t.Assignee != "" {
			var err error
			assigneeID, err = client.ResolveAccountID(t.Assignee)
			if err != nil || assigneeID == "" {
				assigneeWarn = t.Assignee
				assigneeID = ""
			}
		}
		key, err := client.CreateIssue(projectKey, issueType, t, assigneeID, parentKey)
		url := ""
		if err == nil && key != "" {
			url = client.baseURL + "/browse/" + key
		}
		return manualTicketCreatedMsg{key: key, url: url, ticket: t, issueType: issueType, err: err, assigneeWarn: assigneeWarn}
	}
}

func (m model) cmdGetTransitions(key string) tea.Cmd {
	client := m.client
	cfg := m.config
	return func() tea.Msg {
		c := client
		if c == nil {
			if cfg.BaseURL == "" || cfg.Email == "" || cfg.APIToken == "" {
				return transitionsLoadedMsg{err: fmt.Errorf("no Jira credentials configured")}
			}
			c = newJiraClient(cfg.BaseURL, cfg.Email, cfg.APIToken)
			c.useADF = cfg.UseADF
		}
		issue, err := c.GetIssue(key)
		if err != nil {
			return transitionsLoadedMsg{err: fmt.Errorf("fetch issue: %w", err)}
		}
		transitions, err := c.GetTransitions(key)
		if err != nil {
			return transitionsLoadedMsg{err: fmt.Errorf("fetch transitions: %w", err)}
		}
		return transitionsLoadedMsg{
			transitions:   transitions,
			currentStatus: issue.Fields.Status.Name,
		}
	}
}

func (m model) cmdTransitionIssue(key, transitionID, statusName string, recordID int64) tea.Cmd {
	client := m.client
	cfg := m.config
	db := m.db
	return func() tea.Msg {
		c := client
		if c == nil {
			if cfg.BaseURL == "" || cfg.Email == "" || cfg.APIToken == "" {
				return transitionAppliedMsg{err: fmt.Errorf("no Jira credentials configured")}
			}
			c = newJiraClient(cfg.BaseURL, cfg.Email, cfg.APIToken)
			c.useADF = cfg.UseADF
		}
		if err := c.TransitionIssue(key, transitionID); err != nil {
			return transitionAppliedMsg{recordID: recordID, err: err}
		}
		_ = updateTicketStatus(db, recordID, statusName)
		return transitionAppliedMsg{recordID: recordID, status: statusName}
	}
}

func (m model) cmdDeleteJiraIssue(key string, recordID int64) tea.Cmd {
	client := m.client
	cfg := m.config
	return func() tea.Msg {
		c := client
		if c == nil {
			if cfg.BaseURL == "" || cfg.Email == "" || cfg.APIToken == "" {
				return jiraDeleteMsg{recordID: recordID, err: fmt.Errorf("no Jira credentials configured")}
			}
			c = newJiraClient(cfg.BaseURL, cfg.Email, cfg.APIToken)
			c.useADF = cfg.UseADF
		}
		err := c.DeleteIssue(key)
		return jiraDeleteMsg{recordID: recordID, err: err}
	}
}

func (m model) cmdLoadHistory() tea.Cmd {
	db := m.db
	return func() tea.Msg {
		records, err := allTickets(db)
		return historyLoadedMsg{records: records, err: err}
	}
}

func (m model) cmdQueryEpic() tea.Cmd {
	client := m.client
	key := m.epicCSVKey
	return func() tea.Msg {
		issue, err := client.GetIssue(key)
		if err != nil {
			return epicQueryMsg{err: fmt.Errorf("fetching %s: %w", key, err)}
		}
		children, err := client.GetEpicChildren(key)
		if err != nil {
			return epicQueryMsg{err: fmt.Errorf("fetching children of %s: %w", key, err)}
		}
		return epicQueryMsg{issue: issue, children: children}
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

		// modeEpicToCSV: skip board picker, go straight to epic query.
		if m.mode == modeEpicToCSV {
			m.screen = screenEpicCSVQuery
			m.loading = true
			m.loadingMsg = fmt.Sprintf("Querying %s…", m.epicCSVKey)
			return m, m.cmdQueryEpic()
		}

		// --project-key supplied: skip board picker entirely.
		if m.projectKey != "" {
			m.loading = false
			if m.mode == modeManual {
				m.screen = screenManualEntry
				return m, m.manualInputs[0].Focus()
			}
			m.screen = screenSettings
			return m, nil
		}

		m.screen = screenBoards
		cacheTTLHours := m.config.BoardCacheTTLHours
		if cacheTTLHours <= 0 {
			cacheTTLHours = 24
		}
		cacheTTL := time.Duration(cacheTTLHours) * time.Hour
		if cached, cacheAge, ok := loadBoardsCache(m.client.baseURL); ok && time.Since(cacheAge) < cacheTTL {
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
		// Start creating child tickets, or finish if none are selected / aborted.
		first := -1
		if !m.creationAborted {
			first = m.firstSelected()
		}
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
		if !m.creationAborted {
			for i := msg.index + 1; i < len(m.tickets); i++ {
				if m.selectedTickets[i] {
					next = i
					break
				}
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

	case manualTicketCreatedMsg:
		m.manualCreating = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		result := CreateResult{
			Ticket:       msg.ticket,
			Key:          msg.key,
			URL:          msg.url,
			Err:          msg.err,
			AssigneeWarn: msg.assigneeWarn,
		}
		if msg.key != "" {
			_ = insertTicket(m.db, TicketRecord{
				Title:       msg.ticket.Title,
				Description: msg.ticket.Description,
				JiraKey:     msg.key,
				URL:         msg.url,
				CreatedAt:   time.Now(),
				TicketType:  msg.issueType,
				ParentKey:   m.manualParentKey,
				ProjectKey:  m.projectKey,
				Assignee:    msg.ticket.Assignee,
				Labels:      msg.ticket.Labels,
			})
		}
		m.manualResults = append(m.manualResults, result)
		if msg.issueType == "Epic" && msg.key != "" {
			m.epicKey = msg.key
			m.epicURL = msg.url
			m.epicTitle = msg.ticket.Title
			m.screen = screenManualContinue
			return m, nil
		}
		return m.manualDone()

	case epicQueryMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			// Stay on screenEpicCSVQuery to display the error.
			return m, nil
		}
		m.epicCSVIssue = msg.issue
		m.epicCSVChildren = msg.children
		m.epicCSVCursor = 0
		m.epicCSVOffset = 0
		m.screen = screenEpicCSVReview
		return m, nil

	case jiraDeleteMsg:
		m.histLoading = false
		if msg.err != nil {
			m.histJiraDeleteErr = msg.err
			return m, nil
		}
		// Jira delete succeeded — remove from local DB and list.
		_ = deleteTicket(m.db, msg.recordID)
		for i, r := range m.histRecords {
			if r.ID == msg.recordID {
				m.histRecords = append(m.histRecords[:i], m.histRecords[i+1:]...)
				if m.histCursor >= len(m.histRecords) {
					m.histCursor = maxInt(0, len(m.histRecords)-1)
				}
				ps := m.histPageSize()
				if m.histOffset > maxInt(0, len(m.histRecords)-ps) {
					m.histOffset = maxInt(0, len(m.histRecords)-ps)
				}
				break
			}
		}
		return m, nil

	case transitionsLoadedMsg:
		m.histTransitionLoading = false
		if msg.err != nil {
			m.histTransitionErr = msg.err
			return m, nil
		}
		m.histTransitions = msg.transitions
		m.histTransitionCurrent = msg.currentStatus
		m.histTransitionCursor = 0
		m.histTransitionActive = true
		return m, nil

	case transitionAppliedMsg:
		if msg.err != nil {
			m.histTransitionErr = msg.err
			return m, nil
		}
		for i, r := range m.histRecords {
			if r.ID == msg.recordID {
				m.histRecords[i].Status = msg.status
				break
			}
		}
		return m, nil
	}

	return m, nil
}

// ── handleKey dispatch ────────────────────────────────────────────────────────

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.screen {
	case screenFirstRun:
		return m.handleFirstRunKey(msg)
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
	case screenCreating:
		return m.handleCreatingKey(msg)
	case screenManualEntry:
		return m.handleManualEntryKey(msg)
	case screenManualContinue:
		return m.handleManualContinueKey(msg)
	case screenDupCheck:
		return m.handleDupCheckKey(msg)
	case screenShowTickets:
		return m.handleShowTicketsKey(msg)
	case screenDone:
		return m.handleDoneKey(msg)
	case screenExportCSV:
		return m.handleExportCSVKey(msg)
	case screenEpicCSVQuery:
		return m.handleEpicCSVQueryKey(msg)
	case screenEpicCSVReview:
		return m.handleEpicCSVReviewKey(msg)
	case screenEpicCSVPath:
		return m.handleEpicCSVPathKey(msg)
	}
	return m, nil
}

// ── Creating keys ─────────────────────────────────────────────────────────────

func (m model) handleCreatingKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.creationAborted = true
	}
	return m, nil
}

// ── Epic CSV keys ─────────────────────────────────────────────────────────────

// handleEpicCSVQueryKey handles keys on the query/loading screen.
// Only reachable when not loading (i.e. an error occurred).
func (m model) handleEpicCSVQueryKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc", "enter":
		return m, tea.Quit
	}
	return m, nil
}

func (m model) handleEpicCSVReviewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	ps := m.epicCSVPageSize()
	n := len(m.epicCSVChildren)
	switch msg.String() {
	case "up", "k":
		if m.epicCSVCursor > 0 {
			m.epicCSVCursor--
			if m.epicCSVCursor < m.epicCSVOffset {
				m.epicCSVOffset = m.epicCSVCursor
			}
		}
	case "down", "j":
		if m.epicCSVCursor < n-1 {
			m.epicCSVCursor++
			if m.epicCSVCursor >= m.epicCSVOffset+ps {
				m.epicCSVOffset = m.epicCSVCursor - ps + 1
			}
		}
	case "pgup", "ctrl+b":
		m.epicCSVCursor -= ps
		if m.epicCSVCursor < 0 {
			m.epicCSVCursor = 0
		}
		m.epicCSVOffset -= ps
		if m.epicCSVOffset < 0 {
			m.epicCSVOffset = 0
		}
	case "pgdown", "ctrl+f":
		m.epicCSVCursor += ps
		if m.epicCSVCursor >= n {
			m.epicCSVCursor = maxInt(0, n-1)
		}
		m.epicCSVOffset += ps
		maxOff := maxInt(0, n-ps)
		if m.epicCSVOffset > maxOff {
			m.epicCSVOffset = maxOff
		}
	case "enter":
		defaultPath := m.epicCSVKey + ".csv"
		m.epicCSVPathInp.SetValue(defaultPath)
		m.epicCSVSaved = false
		m.epicCSVSaveErr = nil
		m.screen = screenEpicCSVPath
		return m, m.epicCSVPathInp.Focus()
	case "q", "esc":
		return m, tea.Quit
	}
	return m, nil
}

func (m model) handleEpicCSVPathKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.epicCSVSaved {
		switch msg.String() {
		case "q", "esc", "enter":
			return m, tea.Quit
		}
		return m, nil
	}

	switch msg.String() {
	case "enter":
		path := strings.TrimSpace(m.epicCSVPathInp.Value())
		if path == "" {
			path = m.epicCSVKey + ".csv"
		}
		if err := m.writeEpicChildrenCSV(path); err != nil {
			m.epicCSVSaveErr = err
			return m, nil
		}
		m.epicCSVSaved = true
		m.epicCSVSavePath = path
		m.epicCSVSaveErr = nil
		return m, nil
	case "esc":
		m.epicCSVSaveErr = nil
		m.screen = screenEpicCSVReview
		return m, nil
	default:
		var cmd tea.Cmd
		m.epicCSVPathInp, cmd = m.epicCSVPathInp.Update(msg)
		return m, cmd
	}
}

// ── Manual entry helpers ───────────────────────────────────────────────────────

const numManualFields = 5 // title(0), desc(1), assignee(2), labels(3), issuetype(4)

func (m model) blurManualField() model {
	switch m.manualFocus {
	case 0:
		m.manualInputs[0].Blur()
	case 1:
		m.manualDesc.Blur()
	case 2:
		m.manualInputs[1].Blur()
	case 3:
		m.manualInputs[2].Blur()
	}
	return m
}

func (m model) focusManualField(i int) (model, tea.Cmd) {
	m.manualFocus = i
	switch i {
	case 0:
		return m, m.manualInputs[0].Focus()
	case 1:
		return m, m.manualDesc.Focus()
	case 2:
		return m, m.manualInputs[1].Focus()
	case 3:
		return m, m.manualInputs[2].Focus()
	}
	return m, nil
}

func (m model) resetManualForm() model {
	m.manualInputs[0].Reset()
	m.manualInputs[1].Reset()
	m.manualInputs[2].Reset()
	m.manualDesc.Reset()
	m.manualFocus = 0
	m.manualTypeCursor = 0
	m.err = nil
	return m
}

func (m model) submitManualTicket() (model, tea.Cmd) {
	title := strings.TrimSpace(m.manualInputs[0].Value())
	if title == "" {
		m.err = fmt.Errorf("title is required")
		return m, nil
	}
	desc := m.manualDesc.Value()
	assignee := strings.TrimSpace(m.manualInputs[1].Value())
	labelsRaw := m.manualInputs[2].Value()
	var labels []string
	for _, l := range strings.Split(labelsRaw, ";") {
		if l = strings.TrimSpace(l); l != "" {
			labels = append(labels, l)
		}
	}
	issueType := issueTypes[m.manualTypeCursor]
	t := Ticket{Title: title, Description: desc, Assignee: assignee, Labels: labels}
	m.manualCreating = true
	m.err = nil
	return m, m.cmdCreateManualTicket(t, issueType, m.manualParentKey)
}

// manualDone populates m.results/tickets/selectedTickets from manualResults then
// transitions to screenDone so the existing done view works without modification.
func (m model) manualDone() (model, tea.Cmd) {
	m.tickets = make([]Ticket, len(m.manualResults))
	m.results = make([]CreateResult, len(m.manualResults))
	m.selectedTickets = make(map[int]bool, len(m.manualResults))
	for i, r := range m.manualResults {
		m.tickets[i] = r.Ticket
		m.results[i] = r
		m.selectedTickets[i] = true
	}
	m.doneCursor = 0
	m.doneOffset = 0
	m.screen = screenDone
	return m, nil
}

// ── Manual entry keys ─────────────────────────────────────────────────────────

func (m model) handleManualEntryKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.manualCreating {
		return m, nil
	}

	switch msg.String() {
	case "tab":
		m = m.blurManualField()
		var cmd tea.Cmd
		m, cmd = m.focusManualField((m.manualFocus + 1) % numManualFields)
		return m, cmd

	case "shift+tab":
		m = m.blurManualField()
		var cmd tea.Cmd
		m, cmd = m.focusManualField((m.manualFocus - 1 + numManualFields) % numManualFields)
		return m, cmd

	case "up", "down":
		if m.manualFocus == 1 {
			var cmd tea.Cmd
			m.manualDesc, cmd = m.manualDesc.Update(msg)
			return m, cmd
		}
		if m.manualFocus == 4 {
			if msg.String() == "up" && m.manualTypeCursor > 0 {
				m.manualTypeCursor--
			} else if msg.String() == "down" && m.manualTypeCursor < len(issueTypes)-1 {
				m.manualTypeCursor++
			}
			return m, nil
		}

	case "left":
		if m.manualFocus == 4 && m.manualTypeCursor > 0 {
			m.manualTypeCursor--
		}
		return m, nil

	case "right":
		if m.manualFocus == 4 && m.manualTypeCursor < len(issueTypes)-1 {
			m.manualTypeCursor++
		}
		return m, nil

	case "enter":
		if m.manualFocus == 1 {
			// Pass enter to textarea so the user can insert newlines.
			var cmd tea.Cmd
			m.manualDesc, cmd = m.manualDesc.Update(msg)
			return m, cmd
		}
		if m.manualFocus == numManualFields-1 {
			return m.submitManualTicket()
		}
		m = m.blurManualField()
		var cmd tea.Cmd
		m, cmd = m.focusManualField(m.manualFocus + 1)
		return m, cmd

	case "ctrl+s":
		return m.submitManualTicket()

	case "esc":
		m = m.blurManualField()
		if len(m.manualResults) > 0 {
			return m.manualDone()
		}
		m.screen = screenBoards
		return m, m.boardSearch.Focus()
	}

	// Route keypresses to the focused widget.
	var cmd tea.Cmd
	switch m.manualFocus {
	case 0:
		m.manualInputs[0], cmd = m.manualInputs[0].Update(msg)
	case 1:
		m.manualDesc, cmd = m.manualDesc.Update(msg)
	case 2:
		m.manualInputs[1], cmd = m.manualInputs[1].Update(msg)
	case 3:
		m.manualInputs[2], cmd = m.manualInputs[2].Update(msg)
	}
	return m, cmd
}

func (m model) handleManualContinueKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch strings.ToLower(msg.String()) {
	case "y", "enter":
		m.manualParentKey = m.epicKey
		m = m.resetManualForm()
		m.screen = screenManualEntry
		return m, m.manualInputs[0].Focus()
	case "n", "esc", "q":
		return m.manualDone()
	}
	return m, nil
}

// ── Done keys ─────────────────────────────────────────────────────────────────

func (m model) handleDoneKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	total := m.doneRowCount()
	ps := m.donePageSize()
	switch msg.String() {
	case "q", "esc", "enter":
		return m, tea.Quit
	case "up", "k":
		if m.doneCursor > 0 {
			m.doneCursor--
			if m.doneCursor < m.doneOffset {
				m.doneOffset = m.doneCursor
			}
		}
	case "down", "j":
		if m.doneCursor < total-1 {
			m.doneCursor++
			if m.doneCursor >= m.doneOffset+ps {
				m.doneOffset = m.doneCursor - ps + 1
			}
		}
	case "pgup", "ctrl+b":
		m.doneCursor -= ps
		if m.doneCursor < 0 {
			m.doneCursor = 0
		}
		if m.doneCursor < m.doneOffset {
			m.doneOffset = m.doneCursor
		}
	case "pgdown", "ctrl+f":
		m.doneCursor += ps
		if m.doneCursor >= total {
			m.doneCursor = maxInt(0, total-1)
		}
		if m.doneCursor >= m.doneOffset+ps {
			m.doneOffset = m.doneCursor - ps + 1
		}
	case "o":
		if url := m.doneRowURL(m.doneCursor); url != "" {
			_ = openURL(url)
		}
	case "e":
		m.exportCSVResultInp.SetValue(m.defaultExportPath())
		m.exportCSVResultSaved = false
		m.exportCSVResultErr = nil
		m.exportCSVResultConfirm = false
		m.exportCSVResultPending = ""
		m.screen = screenExportCSV
		return m, m.exportCSVResultInp.Focus()
	case "r":
		// Retry is not supported for modeManual (no cmdCreateTicket infrastructure).
		if m.mode == modeManual {
			return m, nil
		}
		// Retry failed tickets. Deselect successful ones and reset failed results.
		hasFailed := false
		for i, r := range m.results {
			if m.selectedTickets[i] && r.Err != nil {
				hasFailed = true
				break
			}
		}
		if !hasFailed {
			return m, nil
		}
		for i := range m.results {
			if m.selectedTickets[i] && m.results[i].Err != nil {
				m.results[i] = CreateResult{}
			} else {
				m.selectedTickets[i] = false
			}
		}
		m.progress = 0
		m.doneCursor = 0
		m.doneOffset = 0
		return m.startCreation()
	}
	return m, nil
}

// ── Export CSV keys ───────────────────────────────────────────────────────────

func (m model) handleExportCSVKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// After a successful save, only quit keys are active.
	if m.exportCSVResultSaved {
		switch msg.String() {
		case "q", "esc", "enter":
			return m, tea.Quit
		}
		return m, nil
	}

	// Overwrite confirmation sub-state.
	if m.exportCSVResultConfirm {
		switch strings.ToLower(msg.String()) {
		case "y":
			if err := m.exportResultsCSV(m.exportCSVResultPending); err != nil {
				m.exportCSVResultErr = err
			} else {
				m.exportCSVResultSaved = true
				m.exportCSVResultPath = m.exportCSVResultPending
				m.exportCSVResultErr = nil
			}
			m.exportCSVResultConfirm = false
			m.exportCSVResultPending = ""
		default:
			m.exportCSVResultConfirm = false
			m.exportCSVResultPending = ""
		}
		return m, nil
	}

	switch msg.String() {
	case "enter":
		path := strings.TrimSpace(m.exportCSVResultInp.Value())
		if path == "" {
			path = m.defaultExportPath()
		}
		if _, err := os.Stat(path); err == nil {
			// File exists — ask for overwrite confirmation.
			m.exportCSVResultConfirm = true
			m.exportCSVResultPending = path
			return m, nil
		}
		if err := m.exportResultsCSV(path); err != nil {
			m.exportCSVResultErr = err
		} else {
			m.exportCSVResultSaved = true
			m.exportCSVResultPath = path
			m.exportCSVResultErr = nil
		}
	case "esc":
		m.exportCSVResultErr = nil
		m.exportCSVResultConfirm = false
		m.screen = screenDone
	default:
		var cmd tea.Cmd
		m.exportCSVResultInp, cmd = m.exportCSVResultInp.Update(msg)
		return m, cmd
	}
	return m, nil
}

// doneRowURL returns the browse URL for the nth visible result row (0-indexed).
func (m model) doneRowURL(cursor int) string {
	i := 0
	if m.mode == modeEpic && m.epicKey != "" {
		if i == cursor {
			return m.epicURL
		}
		i++
	}
	for j, r := range m.results {
		if !m.selectedTickets[j] {
			continue
		}
		if r.Key == "" && r.Err == nil {
			continue
		}
		if i == cursor {
			return r.URL
		}
		i++
	}
	return ""
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

// ── First-run keys ────────────────────────────────────────────────────────────

func (m model) handleFirstRunKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch strings.ToLower(msg.String()) {
	case "y", "enter":
		return m.confirmFirstRun()
	case "n", "esc", "q":
		return m, tea.Quit
	}
	return m, nil
}

// confirmFirstRun creates the data directory and database, then transitions
// to the normal startup screen (auth or verify).
func (m model) confirmFirstRun() (model, tea.Cmd) {
	m.firstRun = false
	if err := ensureAppDir(); err != nil {
		m.err = fmt.Errorf("could not create %s: %v", appDir(), err)
		m.screen = screenFirstRun
		return m, nil
	}
	db, err := openDB()
	if err != nil {
		m.err = fmt.Errorf("could not open database: %v", err)
		m.screen = screenFirstRun
		return m, nil
	}
	m.db = db

	if m.mode == modeShow {
		m.screen = screenShowTickets
		m.histLoading = true
		return m, tea.Batch(m.spinner.Tick, m.cmdLoadHistory())
	}

	hasCreds := m.config.BaseURL != "" && m.config.Email != "" && m.config.APIToken != ""
	if hasCreds {
		m.screen = screenVerify
		m.loading = true
		m.loadingMsg = "Verifying saved credentials…"
		m.client = newJiraClient(m.config.BaseURL, m.config.Email, m.config.APIToken)
		m.client.useADF = m.config.UseADF
		return m, tea.Batch(textinput.Blink, m.spinner.Tick, m.cmdVerifyAuth())
	}
	m.screen = screenAuth
	return m, tea.Batch(textinput.Blink, m.spinner.Tick, m.authInputs[0].Focus())
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
			m.client.useADF = m.config.UseADF
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
				if m.mode == modeManual {
					m.screen = screenManualEntry
					return m, m.manualInputs[0].Focus()
				}
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
			if m.mode == modeManual {
				m.screen = screenManualEntry
				return m, m.manualInputs[0].Focus()
			}
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
// In modeEpic, if the epic was already created (m.epicKey != ""), skip to tickets.
func (m model) startCreation() (model, tea.Cmd) {
	m.creationAborted = false
	if m.mode == modeEpic && m.epicKey == "" {
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

const numEpicFields = 3 // title(0), description(1), requester(2)

func (m model) blurCurrentEpicField() model {
	switch m.epicFocus {
	case 0:
		m.epicInputs[0].Blur()
	case 1:
		m.epicDescTA.Blur()
	case 2:
		m.epicInputs[1].Blur()
	}
	return m
}

func (m model) focusEpicField(i int) (model, tea.Cmd) {
	m.epicFocus = i
	switch i {
	case 0:
		return m, m.epicInputs[0].Focus()
	case 1:
		return m, m.epicDescTA.Focus()
	case 2:
		return m, m.epicInputs[1].Focus()
	}
	return m, nil
}

func (m model) handleEpicSetupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg.String() {
	case "tab":
		m = m.blurCurrentEpicField()
		var cmd tea.Cmd
		m, cmd = m.focusEpicField((m.epicFocus + 1) % numEpicFields)
		return m, cmd

	case "shift+tab":
		m = m.blurCurrentEpicField()
		var cmd tea.Cmd
		m, cmd = m.focusEpicField((m.epicFocus - 1 + numEpicFields) % numEpicFields)
		return m, cmd

	case "up", "down":
		if m.epicFocus == 1 {
			// Let textarea handle cursor movement within description.
			var cmd tea.Cmd
			m.epicDescTA, cmd = m.epicDescTA.Update(msg)
			return m, cmd
		}
		m = m.blurCurrentEpicField()
		next := m.epicFocus
		if msg.String() == "down" {
			next = (m.epicFocus + 1) % numEpicFields
		} else {
			next = (m.epicFocus - 1 + numEpicFields) % numEpicFields
		}
		var cmd tea.Cmd
		m, cmd = m.focusEpicField(next)
		return m, cmd

	case "enter":
		if m.epicFocus == 1 {
			// Insert newline in the description textarea.
			var cmd tea.Cmd
			m.epicDescTA, cmd = m.epicDescTA.Update(msg)
			return m, cmd
		}
		if m.epicFocus < numEpicFields-1 {
			m = m.blurCurrentEpicField()
			var cmd tea.Cmd
			m, cmd = m.focusEpicField(m.epicFocus + 1)
			return m, cmd
		}
		// Submit from requester field.
		title := strings.TrimSpace(m.epicInputs[0].Value())
		if title == "" {
			m.err = fmt.Errorf("epic title is required")
			break
		}
		m.err = nil
		m.epicTitle = title
		m.epicDesc = strings.TrimSpace(m.epicDescTA.Value())
		m.epicReq = strings.TrimSpace(m.epicInputs[1].Value())

		existing, _ := findEpicsByTitle(m.db, title)
		if len(existing) > 0 {
			m.existingEpics = existing
			m.screen = screenEpicDupWarn
			return m, nil
		}
		m.screen = screenTickets
		return m, nil

	case "esc":
		m = m.blurCurrentEpicField()
		m.screen = screenSettings
		return m, nil

	default:
		var cmd tea.Cmd
		switch m.epicFocus {
		case 0:
			m.epicInputs[0], cmd = m.epicInputs[0].Update(msg)
		case 1:
			m.epicDescTA, cmd = m.epicDescTA.Update(msg)
		case 2:
			m.epicInputs[1], cmd = m.epicInputs[1].Update(msg)
		}
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

	// Clear any lingering Jira delete error on any keypress.
	m.histJiraDeleteErr = nil

	// Clear transition error on any keypress.
	m.histTransitionErr = nil

	filtered := m.histFiltered()

	if m.histConfirmDelete {
		switch msg.String() {
		case "y", "Y":
			if len(filtered) > 0 {
				rec := filtered[m.histCursor]
				_ = deleteTicket(m.db, rec.ID)
				for i, r := range m.histRecords {
					if r.ID == rec.ID {
						m.histRecords = append(m.histRecords[:i], m.histRecords[i+1:]...)
						break
					}
				}
				newFiltered := m.histFiltered()
				if m.histCursor >= len(newFiltered) {
					m.histCursor = maxInt(0, len(newFiltered)-1)
				}
				ps := m.histPageSize()
				if m.histOffset > maxInt(0, len(newFiltered)-ps) {
					m.histOffset = maxInt(0, len(newFiltered)-ps)
				}
			}
			m.histConfirmDelete = false
		default:
			m.histConfirmDelete = false
		}
		return m, nil
	}

	if m.histConfirmDeleteJira {
		switch msg.String() {
		case "y", "Y":
			if len(filtered) > 0 {
				rec := filtered[m.histCursor]
				m.histConfirmDeleteJira = false
				m.histLoading = true
				return m, m.cmdDeleteJiraIssue(rec.JiraKey, rec.ID)
			}
			m.histConfirmDeleteJira = false
		default:
			m.histConfirmDeleteJira = false
		}
		return m, nil
	}

	if m.histTransitionActive {
		switch msg.String() {
		case "up", "k":
			if m.histTransitionCursor > 0 {
				m.histTransitionCursor--
			}
		case "down", "j":
			if m.histTransitionCursor < len(m.histTransitions)-1 {
				m.histTransitionCursor++
			}
		case "enter":
			if len(m.histTransitions) > 0 {
				t := m.histTransitions[m.histTransitionCursor]
				filtered := m.histFiltered()
				if len(filtered) > 0 {
					rec := filtered[m.histCursor]
					m.histTransitionActive = false
					return m, m.cmdTransitionIssue(rec.JiraKey, t.ID, t.Name, rec.ID)
				}
			}
			m.histTransitionActive = false
		case "esc", "q":
			m.histTransitionActive = false
		}
		return m, nil
	}

	// Navigation keys work whether or not the search input is focused.
	switch msg.String() {
	case "up", "k":
		if m.histCursor > 0 {
			m.histCursor--
			if m.histCursor < m.histOffset {
				m.histOffset = m.histCursor
			}
		}
		return m, nil
	case "down", "j":
		if m.histCursor < len(filtered)-1 {
			m.histCursor++
			ps := m.histPageSize()
			if m.histCursor >= m.histOffset+ps {
				m.histOffset = m.histCursor - ps + 1
			}
		}
		return m, nil
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
		return m, nil
	case "pgdown", "ctrl+f":
		ps := m.histPageSize()
		m.histCursor += ps
		if m.histCursor >= len(filtered) {
			m.histCursor = maxInt(0, len(filtered)-1)
		}
		m.histOffset += ps
		maxOff := maxInt(0, len(filtered)-ps)
		if m.histOffset > maxOff {
			m.histOffset = maxOff
		}
		return m, nil
	}

	// When the search input is focused, route remaining keys to it.
	if m.histSearch.Focused() {
		switch msg.String() {
		case "esc":
			if m.histSearch.Value() != "" {
				m.histSearch.SetValue("")
				m.histCursor, m.histOffset = 0, 0
				return m, nil
			}
			m.histSearch.Blur()
			return m, tea.Quit
		case "enter":
			m.histSearch.Blur()
			return m, nil
		default:
			prevVal := m.histSearch.Value()
			var cmd tea.Cmd
			m.histSearch, cmd = m.histSearch.Update(msg)
			if m.histSearch.Value() != prevVal {
				m.histCursor, m.histOffset = 0, 0
			}
			return m, cmd
		}
	}

	// Normal (search not focused) key handling.
	switch msg.String() {
	case "t":
		filtered := m.histFiltered()
		if len(filtered) > 0 {
			m.histTransitionLoading = true
			m.histTransitionErr = nil
			m.histTransitionActive = false
			return m, m.cmdGetTransitions(filtered[m.histCursor].JiraKey)
		}
	case "/":
		return m, m.histSearch.Focus()
	case "s":
		fields := []string{"date", "key", "title", "type"}
		naturalAsc := map[string]bool{"date": false, "key": true, "title": true, "type": true}
		for i, f := range fields {
			if f == m.histSortField {
				m.histSortField = fields[(i+1)%len(fields)]
				m.histSortAsc = naturalAsc[m.histSortField]
				m.histCursor, m.histOffset = 0, 0
				break
			}
		}
	case "S":
		m.histSortAsc = !m.histSortAsc
		m.histCursor, m.histOffset = 0, 0
	case "o":
		if len(filtered) > 0 {
			if url := filtered[m.histCursor].URL; url != "" {
				_ = openURL(url)
			}
		}
	case "d":
		if len(filtered) > 0 {
			m.histConfirmDelete = true
			m.histConfirmDeleteJira = false
		}
	case "D":
		if len(filtered) > 0 {
			m.histConfirmDeleteJira = true
			m.histConfirmDelete = false
		}
	case "q", "esc", "enter":
		return m, tea.Quit
	}
	return m, nil
}

func (m model) histFiltered() []TicketRecord {
	// Sort a copy of the full list.
	sorted := make([]TicketRecord, len(m.histRecords))
	copy(sorted, m.histRecords)
	asc := m.histSortAsc
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		var less bool
		switch m.histSortField {
		case "key":
			less = a.JiraKey < b.JiraKey
		case "title":
			less = strings.ToLower(a.Title) < strings.ToLower(b.Title)
		case "type":
			less = strings.ToLower(a.TicketType) < strings.ToLower(b.TicketType)
		default: // "date"
			less = a.CreatedAt.Before(b.CreatedAt)
		}
		if asc {
			return less
		}
		return !less
	})

	// Filter.
	q := strings.ToLower(strings.TrimSpace(m.histSearch.Value()))
	if q == "" {
		return sorted
	}
	var out []TicketRecord
	for _, r := range sorted {
		if strings.Contains(strings.ToLower(r.Title), q) ||
			strings.Contains(strings.ToLower(r.JiraKey), q) ||
			strings.Contains(strings.ToLower(r.TicketType), q) ||
			strings.Contains(strings.ToLower(r.ParentKey), q) {
			out = append(out, r)
		}
	}
	return out
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
	case screenExportCSV:
		return m.viewExportCSV()
	case screenEpicSetup:
		return m.viewEpicSetup()
	case screenEpicDupWarn:
		return m.viewEpicDupWarn()
	case screenDupCheck:
		return m.viewDupCheck()
	case screenShowTickets:
		return m.viewShowTickets()
	case screenManualEntry:
		return m.viewManualEntry()
	case screenManualContinue:
		return m.viewManualContinue()
	case screenFirstRun:
		return m.viewFirstRun()
	case screenEpicCSVQuery:
		if m.loading {
			return m.viewSpinner(m.loadingMsg)
		}
		return m.viewEpicCSVQuery()
	case screenEpicCSVReview:
		return m.viewEpicCSVReview()
	case screenEpicCSVPath:
		return m.viewEpicCSVPath()
	}
	return ""
}

func (m model) viewSpinner(msg string) string {
	content := m.spinner.View() + "  " + subtitleStyle.Render(msg)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

// ── First-run view ────────────────────────────────────────────────────────────

func (m model) viewFirstRun() string {
	dir := appDir()

	errLine := ""
	if m.err != nil {
		errLine = "\n" + errorStyle.Render("⚠  "+m.err.Error())
	}

	prompt := successStyle.Render("[Y]") + dimStyle.Render("es, create it") + "   " +
		dimStyle.Render("[N]") + dimStyle.Render("o, quit")

	envHint := dimStyle.Render("Override location: JIRA_TUI_DIR=/your/path jira-tui")

	body := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("Welcome to jira-tui"),
		"",
		subtitleStyle.Render("This appears to be your first time running the app."),
		"",
		"A data directory will be created at:",
		successStyle.Render("  "+dir+"/"),
		"",
		dimStyle.Render("It will contain:"),
		dimStyle.Render("  config      — Jira credentials and settings"),
		dimStyle.Render("  history.db  — local ticket creation history"),
		errLine,
		"",
		"Create it now?  "+prompt,
		"",
		envHint,
	)

	w := clamp(m.width-4, 54, 70)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		panelStyle.Width(w).Render(body))
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

	w := clamp(m.width-4, 52, 70)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		panelStyle.Width(w).Render(body))
}

// ── Boards view ───────────────────────────────────────────────────────────────

func (m model) viewBoards() string {
	w := m.width - 4
	if w < 60 {
		w = 60
	}

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

	w := m.width - 4
	if w < 58 {
		w = 58
	}
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
	w := m.width - 4
	if w < 64 {
		w = 64
	}
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

		titleLine := action + truncate(item.ticket.Title, w-17)
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
	// Title field
	titleStyle2 := blurredInputStyle
	if m.epicFocus == 0 {
		titleStyle2 = focusedInputStyle
	}
	titleRow := subtitleStyle.Render("Epic Title") + "\n" + titleStyle2.Width(46).Render(m.epicInputs[0].View())

	// Description field (textarea)
	descBorder := blurredInputStyle
	if m.epicFocus == 1 {
		descBorder = focusedInputStyle
	}
	descRow := subtitleStyle.Render("Description (Enter for newlines, Tab to advance)") + "\n" +
		descBorder.Width(46).Render(m.epicDescTA.View())

	// Requester field
	reqStyle := blurredInputStyle
	if m.epicFocus == 2 {
		reqStyle = focusedInputStyle
	}
	reqRow := subtitleStyle.Render("Requester") + "\n" + reqStyle.Width(46).Render(m.epicInputs[1].View())

	errLine := ""
	if m.err != nil {
		errLine = "\n" + errorStyle.Render("⚠  "+m.err.Error())
	}

	w := clamp(m.width-4, 54, 70)
	footer := footerStyle.Width(w).Render(
		"Tab / Shift+Tab move field  •  Enter next/newline  •  Esc back")

	body := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("Create Epic"),
		subtitleStyle.Render(fmt.Sprintf("Project: %s", m.projectKey)),
		"",
		titleRow, "",
		descRow, "",
		reqRow,
		errLine,
		"",
		footer,
	)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		panelStyle.Width(w).Render(body))
}

// ── Epic-dup-warn view ────────────────────────────────────────────────────────

func (m model) viewEpicDupWarn() string {
	var rows []string
	for _, r := range m.existingEpics {
		rows = append(rows, "  "+keyStyle.Render(r.JiraKey)+"  "+
			truncate(r.Title, 36)+"  "+
			dimStyle.Render(r.CreatedAt.Format("2006-01-02")))
	}

	w := clamp(m.width-4, 52, 76)
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
	w := m.width - 4
	if w < 60 {
		w = 60
	}
	barWidth := w - 14 // panel inner (w-6) minus "  100%" (6) and margin (2)
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
			labelStyle.Render("EPIC")+"  "+truncate(m.epicTitle, w-34))
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
		rows = append(rows, "  "+status+"  "+truncate(t.Title, w-28))
	}

	footer := footerStyle.Width(w - 4).Render("Esc / q  abort (in-flight ticket finishes first)")
	if m.creationAborted {
		footer = errorStyle.Render("Aborting… waiting for in-flight ticket to complete")
	}

	body := lipgloss.JoinVertical(lipgloss.Left,
		title, "", bar, "", strings.Join(rows, "\n"), "", footer)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		panelStyle.Width(w).Render(body))
}

// ── Manual entry view ─────────────────────────────────────────────────────────

func (m model) viewManualEntry() string {
	if m.manualCreating {
		return m.viewSpinner("Creating ticket…")
	}

	w := m.width - 4
	if w < 60 {
		w = 60
	}
	inputW := w - 8

	header := titleStyle.Render("Create Ticket")
	if m.manualParentKey != "" {
		header += "  " + labelStyle.Render("child of") + " " + successStyle.Render(m.manualParentKey)
	}

	fieldStyle := func(focus int) lipgloss.Style {
		if m.manualFocus == focus {
			return focusedInputStyle
		}
		return blurredInputStyle
	}

	titleRow := subtitleStyle.Render("Title *") + "\n" +
		fieldStyle(0).Width(inputW).Render(m.manualInputs[0].View())

	descRow := subtitleStyle.Render("Description") + "\n" +
		fieldStyle(1).Width(inputW).Render(m.manualDesc.View())

	assigneeRow := subtitleStyle.Render("Assignee") + "\n" +
		fieldStyle(2).Width(inputW).Render(m.manualInputs[1].View())

	labelsRow := subtitleStyle.Render("Labels (semicolon-separated)") + "\n" +
		fieldStyle(3).Width(inputW).Render(m.manualInputs[2].View())

	// Issue type picker: horizontal list, selected highlighted.
	typeLabel := subtitleStyle.Render("Issue Type")
	if m.manualFocus == 4 {
		typeLabel = selectedItemStyle.Render("Issue Type")
	}
	var typeOpts []string
	for i, it := range issueTypes {
		if i == m.manualTypeCursor {
			typeOpts = append(typeOpts, successStyle.Render("● "+it))
		} else {
			typeOpts = append(typeOpts, dimStyle.Render("○ "+it))
		}
	}
	typeRow := typeLabel + "\n  " + strings.Join(typeOpts, "   ")

	errLine := ""
	if m.err != nil {
		errLine = errorStyle.Render("⚠  " + m.err.Error())
	}

	footer := footerStyle.Width(w - 4).Render("Tab move field  •  ←/→ change type  •  Enter / Ctrl+S create  •  Esc back")

	parts := []string{header, "", titleRow, "", descRow, "", assigneeRow, "", labelsRow, "", typeRow}
	if errLine != "" {
		parts = append(parts, "", errLine)
	}
	parts = append(parts, "", footer)

	body := lipgloss.JoinVertical(lipgloss.Left, parts...)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		panelStyle.Width(w).Render(body))
}

// ── Manual continue view ──────────────────────────────────────────────────────

func (m model) viewManualContinue() string {
	w := clamp(m.width-4, 52, 72)

	epicRow := successStyle.Render("✓") + " " + labelStyle.Render("EPIC") + " " +
		dimStyle.Render(fmt.Sprintf("%-12s", m.epicKey)) + " " + truncate(m.epicTitle, 36)
	urlRow := dimStyle.Render("  " + m.epicURL)

	prompt := "Add a subtask?   " +
		successStyle.Render("[Y]") + dimStyle.Render("es") + "  " +
		dimStyle.Render("[N]") + dimStyle.Render("o / Esc to finish")

	body := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("Epic Created!"),
		"",
		epicRow,
		urlRow,
		"",
		prompt,
	)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		panelStyle.Width(w).Render(body))
}

// ── Done view ─────────────────────────────────────────────────────────────────

func (m model) viewDone() string {
	created, failed := 0, 0
	var allRows []string
	rowIdx := 0

	if m.mode == modeEpic && m.epicKey != "" {
		cur := "  "
		if rowIdx == m.doneCursor {
			cur = cursorStyle.Render("▶ ")
		}
		allRows = append(allRows, cur+successStyle.Render("✓")+" "+
			labelStyle.Render("EPIC")+" "+
			dimStyle.Render(fmt.Sprintf("%-12s", m.epicKey))+" "+m.epicURL)
		rowIdx++
	}

	for i, r := range m.results {
		if !m.selectedTickets[i] {
			continue
		}
		cur := "  "
		if rowIdx == m.doneCursor {
			cur = cursorStyle.Render("▶ ")
		}
		if r.Key != "" {
			created++
			allRows = append(allRows, cur+successStyle.Render("✓")+" "+
				dimStyle.Render(fmt.Sprintf("%-12s", r.Key))+" "+r.URL)
			rowIdx++
		} else if r.Err != nil {
			failed++
			allRows = append(allRows, cur+errorStyle.Render("✗")+" "+
				truncate(r.Ticket.Title, 36)+"  "+dimStyle.Render(r.Err.Error()))
			rowIdx++
		}
	}

	failStr := dimStyle.Render("0")
	if failed > 0 {
		failStr = errorStyle.Render(fmt.Sprintf("%d", failed))
	}
	summary := fmt.Sprintf("Created: %s   Failed: %s",
		successStyle.Render(fmt.Sprintf("%d", created)), failStr)

	w := m.width - 4
	if w < 60 {
		w = 60
	}

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

	retryHint := ""
	if failed > 0 && m.mode != modeManual {
		retryHint = "  •  r retry failed"
	}
	footer := footerStyle.Width(w).Render("↑↓/jk  •  o open URL  •  e export CSV" + retryHint + "  •  q / Esc to exit")

	parts := []string{titleStyle.Render("✓ Done"), "", summary, "", strings.Join(visible, "\n")}
	if scrollHint != "" {
		parts = append(parts, scrollHint)
	}
	parts = append(parts, "", footer)

	body := lipgloss.JoinVertical(lipgloss.Left, parts...)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		panelStyle.Width(w).Render(body))
}

// ── Export CSV view ───────────────────────────────────────────────────────────

func (m model) viewExportCSV() string {
	w := clamp(m.width-4, 60, 84)

	if m.exportCSVResultSaved {
		rowCount := 0
		for i, r := range m.results {
			if m.selectedTickets[i] && (r.Key != "" || r.Err != nil) {
				rowCount++
			}
		}
		if m.mode == modeEpic && m.epicKey != "" {
			rowCount++
		}
		body := lipgloss.JoinVertical(lipgloss.Left,
			titleStyle.Render("CSV Saved"),
			"",
			successStyle.Render("✓ "+m.exportCSVResultPath),
			"",
			subtitleStyle.Render(fmt.Sprintf("%d result(s) exported", rowCount)),
			dimStyle.Render("Columns: Status, Key, Title, URL, Error"),
			"",
			footerStyle.Width(w).Render("Enter / q / Esc to exit"),
		)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			panelStyle.Width(w).Render(body))
	}

	var statusLine string
	if m.exportCSVResultConfirm {
		statusLine = errorStyle.Render("⚠  File exists — overwrite? y / any other key to cancel")
	} else if m.exportCSVResultErr != nil {
		statusLine = errorStyle.Render("⚠  " + m.exportCSVResultErr.Error())
	}

	defaultPath := m.defaultExportPath()
	body := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("Export Results CSV"),
		subtitleStyle.Render("Columns: Status, Key, Title, URL, Error"),
		"",
		subtitleStyle.Render("File path"),
		focusedInputStyle.Width(w-8).Render(m.exportCSVResultInp.View()),
		dimStyle.Render("Default: "+defaultPath),
		"",
		statusLine,
		"",
		footerStyle.Width(w).Render("Enter to save  •  Esc back to results  •  Ctrl+C quit"),
	)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		panelStyle.Width(w).Render(body))
}

// ── Show-tickets view ─────────────────────────────────────────────────────────

func (m model) viewShowTickets() string {
	if m.histLoading {
		return m.viewSpinner("Loading ticket history…")
	}

	w := m.width - 4
	if w < 72 {
		w = 72
	}
	title := titleStyle.Render("Ticket History")

	searchBar := focusedInputStyle.Width(w - 8).Render(m.histSearch.View())

	filtered := m.histFiltered()

	if len(m.histRecords) == 0 {
		body := lipgloss.JoinVertical(lipgloss.Left,
			title, "",
			searchBar, "",
			dimStyle.Render("No tickets have been created yet."),
			"",
			footerStyle.Width(w).Render("q / Esc / Enter to quit"),
		)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			panelStyle.Width(w).Render(body))
	}

	ps := m.histPageSize()
	end := m.histOffset + ps
	if end > len(filtered) {
		end = len(filtered)
	}

	sortArrow := "↓"
	if m.histSortAsc {
		sortArrow = "↑"
	}
	sortNames := map[string]string{"date": "Date", "key": "Key", "title": "Title", "type": "Type"}
	sortLabel := sortNames[m.histSortField] + sortArrow

	var pageInfo string
	if len(filtered) == 0 {
		pageInfo = subtitleStyle.Render(fmt.Sprintf("No matches  •  %s", sortLabel))
	} else if m.histSearch.Value() != "" {
		pageInfo = subtitleStyle.Render(fmt.Sprintf(
			"%d – %d  of  %d matches  (%d total)  •  %s", m.histOffset+1, end, len(filtered), len(m.histRecords), sortLabel))
	} else {
		pageInfo = subtitleStyle.Render(fmt.Sprintf(
			"%d – %d  of  %d tickets  •  %s", m.histOffset+1, end, len(filtered), sortLabel))
	}

	var rows []string
	for i := m.histOffset; i < end; i++ {
		r := filtered[i]
		cur := "  "
		nameStyle := lipgloss.NewStyle()
		if i == m.histCursor {
			cur = cursorStyle.Render("▶ ")
			nameStyle = selectedItemStyle
		}

		typeTag := labelStyle.Render(truncate(r.TicketType, 8))
		keyTag := keyStyle.Render(r.JiraKey)
		date := dimStyle.Render(r.CreatedAt.Format("2006-01-02"))

		var statusTag string
		var statusVisual int
		if r.Status != "" {
			st := truncate(r.Status, 10)
			statusTag = dimStyle.Render("[" + st + "]")
			statusVisual = 2 + len([]rune(st)) // "[" + status + "]"
		}

		statusPart := ""
		if statusTag != "" {
			statusPart = "  " + statusTag
			statusVisual += 2 // account for "  " separator
		}

		parent := ""
		parentVisual := 0
		if r.ParentKey != "" {
			parent = dimStyle.Render("↳ "+r.ParentKey) + "  "
			parentVisual = 2 + len([]rune(r.ParentKey)) + 2
		}

		typeVisual := len([]rune(truncate(r.TicketType, 8)))
		fixedUsed := 2 + 10 + 2 + len([]rune(r.JiraKey)) + 2 + typeVisual + statusVisual + 2 + parentVisual
		maxTitle := (w - 6) - fixedUsed
		if maxTitle < 10 {
			maxTitle = 10
		}
		row := cur + date + "  " + keyTag + "  " + typeTag + statusPart + "  " + parent + nameStyle.Render(truncate(r.Title, maxTitle))
		rows = append(rows, row)
	}

	var scrollHint string
	if m.histOffset > 0 && end < len(filtered) {
		scrollHint = dimStyle.Render("  ↑ more above  ·  ↓ more below")
	} else if m.histOffset > 0 {
		scrollHint = dimStyle.Render("  ↑ more above")
	} else if end < len(filtered) {
		scrollHint = dimStyle.Render("  ↓ more below")
	}

	listBody := strings.Join(rows, "\n")
	if len(filtered) == 0 {
		listBody = dimStyle.Render("No tickets match the filter.")
	}

	parts := []string{title, searchBar, pageInfo, "", listBody}
	if scrollHint != "" {
		parts = append(parts, scrollHint)
	}

	if m.histTransitionLoading {
		parts = append(parts, "", dimStyle.Render("  Fetching transitions…"))
	} else if m.histTransitionActive && len(m.histTransitions) > 0 {
		var tlines []string
		tlines = append(tlines, subtitleStyle.Render(fmt.Sprintf("Update status — current: %s", m.histTransitionCurrent)))
		tlines = append(tlines, "")
		for i, t := range m.histTransitions {
			if i == m.histTransitionCursor {
				tlines = append(tlines, cursorStyle.Render("▶ ")+selectedItemStyle.Render(t.Name))
			} else {
				tlines = append(tlines, "  "+t.Name)
			}
		}
		tlines = append(tlines, "")
		tlines = append(tlines, dimStyle.Render("Enter to apply  •  Esc to cancel"))
		parts = append(parts, "", strings.Join(tlines, "\n"))
	} else if m.histTransitionErr != nil {
		parts = append(parts, errorStyle.Render("Transition error: "+m.histTransitionErr.Error()))
	}

	var footerText string
	if m.histConfirmDelete && len(filtered) > 0 {
		rec := filtered[m.histCursor]
		footerText = errorStyle.Render(fmt.Sprintf("Delete local record for %s %q? (y/N)", rec.JiraKey, truncate(rec.Title, 28)))
	} else if m.histConfirmDeleteJira && len(filtered) > 0 {
		rec := filtered[m.histCursor]
		footerText = errorStyle.Render(fmt.Sprintf("Delete %s from Jira AND local? (y/N)", rec.JiraKey))
	} else if m.histJiraDeleteErr != nil {
		footerText = errorStyle.Render("Jira delete failed: " + m.histJiraDeleteErr.Error())
	} else if m.histTransitionActive {
		footerText = "Enter apply  •  ↑↓/jk navigate  •  Esc cancel"
	} else if m.histTransitionLoading {
		footerText = dimStyle.Render("Fetching transitions from Jira…")
	} else if m.histSearch.Focused() {
		footerText = "Type to filter  •  Enter to browse results  •  Esc clear / quit"
	} else {
		footerText = "↑↓/jk navigate  •  / filter  •  s sort  •  S reverse  •  t status  •  o open  •  d local  •  D Jira delete  •  q quit"
	}
	parts = append(parts, footerStyle.Width(w).Render(footerText))

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

// defaultExportPath returns the default CSV export filename for the Done screen.
func (m model) defaultExportPath() string {
	if m.epicKey != "" {
		return m.epicKey + ".csv"
	}
	return "jira_tickets_results.csv"
}

// exportResultsCSV writes the Done-screen results to path.
// Columns: Status,Key,Title,URL,Error.
func (m model) exportResultsCSV(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	_ = w.Write([]string{"Status", "Key", "Title", "URL", "Error"})
	if m.mode == modeEpic && m.epicKey != "" {
		_ = w.Write([]string{"created", m.epicKey, m.epicTitle, m.epicURL, ""})
	}
	for i, r := range m.results {
		if !m.selectedTickets[i] {
			continue
		}
		if r.Key != "" {
			_ = w.Write([]string{"created", r.Key, r.Ticket.Title, r.URL, ""})
		} else if r.Err != nil {
			_ = w.Write([]string{"failed", "", r.Ticket.Title, "", r.Err.Error()})
		}
	}
	w.Flush()
	return w.Error()
}

// ── Epic CSV views ────────────────────────────────────────────────────────────

// viewEpicCSVQuery is shown when m.loading=false (error state after query).
func (m model) viewEpicCSVQuery() string {
	w := clamp(m.width-4, 52, 76)
	errLine := ""
	if m.err != nil {
		errLine = errorStyle.Render("⚠  " + m.err.Error())
	}
	body := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("Query Failed"),
		"",
		errLine,
		"",
		footerStyle.Width(w).Render("q / Esc / Enter to quit"),
	)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		panelStyle.Width(w).Render(body))
}

func (m model) viewEpicCSVReview() string {
	w := m.width - 4
	if w < 72 {
		w = 72
	}
	issue := m.epicCSVIssue

	issueTypeLabel := strings.ToUpper(issue.Fields.IssueType.Name)
	epicHeader := keyStyle.Render(issue.Key) + "  " +
		labelStyle.Render(issueTypeLabel) + "  " +
		titleStyle.Render(truncate(issue.Fields.Summary, w-30))

	childCount := len(m.epicCSVChildren)
	countLine := subtitleStyle.Render(fmt.Sprintf("%d child issue(s) found", childCount))

	ps := m.epicCSVPageSize()
	offset := m.epicCSVOffset
	end := offset + ps
	if end > childCount {
		end = childCount
	}

	var rows []string
	for i := offset; i < end; i++ {
		child := m.epicCSVChildren[i]
		cur := "  "
		nameStyle := lipgloss.NewStyle()
		if i == m.epicCSVCursor {
			cur = cursorStyle.Render("▶ ")
			nameStyle = selectedItemStyle
		}
		assigneePart := ""
		if child.Fields.Assignee != nil {
			name := child.Fields.Assignee.EmailAddress
			if name == "" {
				name = child.Fields.Assignee.DisplayName
			}
			assigneePart = "  " + dimStyle.Render(name)
		}
		typeTag := dimStyle.Render("[" + child.Fields.IssueType.Name + "]")
		rows = append(rows, cur+typeTag+"  "+nameStyle.Render(truncate(child.Fields.Summary, w-32))+assigneePart)
	}

	var scrollHint string
	if offset > 0 && end < childCount {
		scrollHint = dimStyle.Render("  ↑ more above  ·  ↓ more below")
	} else if offset > 0 {
		scrollHint = dimStyle.Render("  ↑ more above")
	} else if end < childCount {
		scrollHint = dimStyle.Render("  ↓ more below")
	}

	defaultPath := m.epicCSVKey + ".csv"
	footer := footerStyle.Width(w).Render(fmt.Sprintf(
		"↑↓/jk navigate  •  PgUp/PgDn jump  •  Enter save as %q  •  q / Esc quit", defaultPath))

	parts := []string{epicHeader, "", countLine}
	if childCount == 0 {
		parts = append(parts, "", dimStyle.Render("No child issues were found for this ticket."),
			dimStyle.Render("Team-managed projects use parent= JQL; classic projects use the Epic Link field."))
	} else {
		parts = append(parts, "", strings.Join(rows, "\n"))
	}
	if scrollHint != "" {
		parts = append(parts, scrollHint)
	}
	parts = append(parts, footer)

	body := lipgloss.JoinVertical(lipgloss.Left, parts...)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Top,
		panelStyle.Width(w).Render(body))
}

func (m model) viewEpicCSVPath() string {
	w := clamp(m.width-4, 60, 84)

	if m.epicCSVSaved {
		body := lipgloss.JoinVertical(lipgloss.Left,
			titleStyle.Render("CSV Saved"),
			"",
			successStyle.Render("✓ "+m.epicCSVSavePath),
			"",
			subtitleStyle.Render(fmt.Sprintf("%d child issues exported", len(m.epicCSVChildren))),
			dimStyle.Render("Columns: Title, Description, Assignee, Labels, Requester"),
			"",
			footerStyle.Width(w).Render("Enter / q / Esc to exit"),
		)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			panelStyle.Width(w).Render(body))
	}

	errLine := ""
	if m.epicCSVSaveErr != nil {
		errLine = "\n" + errorStyle.Render("⚠  "+m.epicCSVSaveErr.Error())
	}

	body := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("Save CSV"),
		subtitleStyle.Render(fmt.Sprintf(
			"Export %d child issues from %s", len(m.epicCSVChildren), m.epicCSVKey)),
		"",
		subtitleStyle.Render("File path"),
		focusedInputStyle.Width(w-8).Render(m.epicCSVPathInp.View()),
		errLine,
		"",
		footerStyle.Width(w).Render("Enter to save  •  Esc back to review  •  Ctrl+C quit"),
	)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		panelStyle.Width(w).Render(body))
}

// epicCSVPageSize returns the number of child-issue rows visible on the review screen.
func (m model) epicCSVPageSize() int {
	n := m.height - 14
	if n < 4 {
		n = 4
	}
	if n > 30 {
		n = 30
	}
	return n
}

// writeEpicChildrenCSV writes the epic's child issues to path in the standard
// jira-tui CSV format (Title, Description, Assignee, Labels, Requester).
// The first four columns are compatible with the input CSV format.
func (m model) writeEpicChildrenCSV(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	if err := w.Write([]string{"Title", "Description", "Assignee", "Labels", "Requester"}); err != nil {
		return err
	}

	for _, child := range m.epicCSVChildren {
		assignee := ""
		if child.Fields.Assignee != nil {
			if child.Fields.Assignee.EmailAddress != "" {
				assignee = child.Fields.Assignee.EmailAddress
			} else {
				assignee = child.Fields.Assignee.DisplayName
			}
		}
		requester := ""
		if child.Fields.Reporter != nil {
			if child.Fields.Reporter.EmailAddress != "" {
				requester = child.Fields.Reporter.EmailAddress
			} else {
				requester = child.Fields.Reporter.DisplayName
			}
		}
		labels := strings.Join(child.Fields.Labels, ";")
		desc := IssueDescription(child.Fields.Description)

		if err := w.Write([]string{child.Fields.Summary, desc, assignee, labels, requester}); err != nil {
			return err
		}
	}

	w.Flush()
	return w.Error()
}

// openURL launches url in the system default browser.
func openURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
