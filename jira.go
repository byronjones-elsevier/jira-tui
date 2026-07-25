package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// JiraUser represents a Jira user embedded in issue fields.
type JiraUser struct {
	AccountID    string `json:"accountId"`
	DisplayName  string `json:"displayName"`
	EmailAddress string `json:"emailAddress"`
}

// JiraIssueFields holds the fields returned for a Jira issue.
type JiraIssueFields struct {
	Summary     string          `json:"summary"`
	Description json.RawMessage `json:"description"` // string (v2) or ADF object (v3)
	Assignee    *JiraUser       `json:"assignee"`
	Reporter    *JiraUser       `json:"reporter"`
	Labels      []string        `json:"labels"`
	IssueType   struct {
		Name string `json:"name"`
	} `json:"issuetype"`
	Status struct {
		Name string `json:"name"`
	} `json:"status"`
}

// JiraIssue represents a Jira issue as returned by the REST API.
type JiraIssue struct {
	Key    string          `json:"key"`
	Fields JiraIssueFields `json:"fields"`
}

// JiraClient wraps Jira's REST API with basic-auth credentials.
type JiraClient struct {
	baseURL string
	email   string
	token   string
	http    *http.Client
	useADF  bool // send descriptions as Atlassian Document Format via REST v3
}

// User represents the authenticated Jira user.
type User struct {
	AccountID   string `json:"accountId"`
	DisplayName string `json:"displayName"`
	Email       string `json:"emailAddress"`
}

// Transition represents a Jira workflow transition.
type Transition struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Board is a Jira agile board with its resolved project key.
type Board struct {
	ID         int
	Name       string
	ProjectKey string
}

// CreateResult holds the outcome of a single ticket creation.
type CreateResult struct {
	Ticket       Ticket
	Key          string // e.g. "FINOPS-7"
	URL          string
	Skipped      bool
	Err          error
	AssigneeWarn string // non-empty when requested assignee could not be resolved
}

func newJiraClient(baseURL, email, token string) *JiraClient {
	return &JiraClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		email:   email,
		token:   token,
		http:    &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *JiraClient) get(path string) ([]byte, error) {
	req, err := http.NewRequest("GET", c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.email, c.token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

func (c *JiraClient) postJSON(path string, body string) ([]byte, error) {
	req, err := http.NewRequest("POST", c.baseURL+path, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.email, c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}
	return b, nil
}

// VerifyAuth checks credentials and returns the authenticated user.
func (c *JiraClient) VerifyAuth() (User, error) {
	data, err := c.get("/rest/api/2/myself")
	if err != nil {
		return User{}, err
	}
	var u User
	return u, json.Unmarshal(data, &u)
}

// GetBoards fetches every Jira agile board the token has access to,
// paging through results 50 at a time until the API reports isLast=true.
func (c *JiraClient) GetBoards() ([]Board, error) {
	type boardPage struct {
		IsLast bool `json:"isLast"`
		Values []struct {
			ID       int    `json:"id"`
			Name     string `json:"name"`
			Location struct {
				ProjectKey string `json:"projectKey"`
			} `json:"location"`
		} `json:"values"`
	}

	const pageSize = 50
	const maxPages = 40
	var boards []Board
	startAt := 0

	for page := 0; page < maxPages; page++ {
		path := fmt.Sprintf("/rest/agile/1.0/board?maxResults=%d&startAt=%d", pageSize, startAt)
		data, err := c.get(path)
		if err != nil {
			return nil, err
		}
		var boardPage boardPage
		if err := json.Unmarshal(data, &boardPage); err != nil {
			return nil, err
		}
		for _, v := range boardPage.Values {
			if v.Location.ProjectKey == "" {
				continue
			}
			boards = append(boards, Board{
				ID:         v.ID,
				Name:       v.Name,
				ProjectKey: v.Location.ProjectKey,
			})
		}
		if boardPage.IsLast || len(boardPage.Values) == 0 {
			break
		}
		startAt += len(boardPage.Values)
	}

	return boards, nil
}

// ResolveAccountID looks up a Jira accountId by email or display name.
// Returns "" (not an error) when no exact match is found.
func (c *JiraClient) ResolveAccountID(email string) (string, error) {
	if email == "" {
		return "", nil
	}
	data, err := c.get("/rest/api/3/user/search?query=" + url.QueryEscape(email))
	if err != nil {
		return "", err
	}
	var users []User
	if err := json.Unmarshal(data, &users); err != nil {
		return "", err
	}
	for _, u := range users {
		if u.Email == email || u.DisplayName == email {
			return u.AccountID, nil
		}
	}
	return "", nil
}

// toADFJSON wraps plain text in Atlassian Document Format (ADF) for REST v3 endpoints.
// Double newlines create new paragraphs; single newlines become hard breaks.
func toADFJSON(text string) (string, error) {
	type adfNode struct {
		Type    string    `json:"type"`
		Text    string    `json:"text,omitempty"`
		Content []adfNode `json:"content,omitempty"`
	}
	type adfDoc struct {
		Version int       `json:"version"`
		Type    string    `json:"type"`
		Content []adfNode `json:"content"`
	}

	paras := strings.Split(text, "\n\n")
	var docContent []adfNode
	for _, para := range paras {
		lines := strings.Split(para, "\n")
		var paraContent []adfNode
		for i, line := range lines {
			paraContent = append(paraContent, adfNode{Type: "text", Text: line})
			if i < len(lines)-1 {
				paraContent = append(paraContent, adfNode{Type: "hardBreak"})
			}
		}
		docContent = append(docContent, adfNode{Type: "paragraph", Content: paraContent})
	}
	if len(docContent) == 0 {
		docContent = []adfNode{{Type: "paragraph", Content: []adfNode{{Type: "text", Text: ""}}}}
	}
	b, err := json.Marshal(adfDoc{Version: 1, Type: "doc", Content: docContent})
	return string(b), err
}

// CreateIssue creates a single Jira issue and returns its key (e.g. "PROJ-42").
// parentKey, when non-empty, sets the parent field (linking a child to an epic or issue).
func (c *JiraClient) CreateIssue(projectKey, issueType string, t Ticket, assigneeID, parentKey string) (string, error) {
	labelParts := make([]string, 0, len(t.Labels))
	for _, l := range t.Labels {
		l = strings.TrimSpace(l)
		l = strings.ReplaceAll(l, " ", "-")
		if l == "" {
			continue
		}
		b, err := json.Marshal(l)
		if err != nil {
			return "", fmt.Errorf("marshal label %q: %w", l, err)
		}
		labelParts = append(labelParts, string(b))
	}
	labelsJSON := "[" + strings.Join(labelParts, ",") + "]"

	summaryJSON, err := json.Marshal(t.Title)
	if err != nil {
		return "", fmt.Errorf("marshal title: %w", err)
	}
	var descJSON string
	if c.useADF {
		descJSON, err = toADFJSON(t.Description)
	} else {
		var b []byte
		b, err = json.Marshal(t.Description)
		descJSON = string(b)
	}
	if err != nil {
		return "", fmt.Errorf("marshal description: %w", err)
	}
	projJSON, err := json.Marshal(projectKey)
	if err != nil {
		return "", fmt.Errorf("marshal project key: %w", err)
	}
	typeJSON, err := json.Marshal(issueType)
	if err != nil {
		return "", fmt.Errorf("marshal issue type: %w", err)
	}

	assigneeClause := ""
	if assigneeID != "" {
		aid, err := json.Marshal(assigneeID)
		if err != nil {
			return "", fmt.Errorf("marshal assignee: %w", err)
		}
		assigneeClause = fmt.Sprintf(`,"assignee":{"accountId":%s}`, aid)
	}

	parentClause := ""
	if parentKey != "" {
		pkJSON, err := json.Marshal(parentKey)
		if err != nil {
			return "", fmt.Errorf("marshal parent key: %w", err)
		}
		parentClause = fmt.Sprintf(`,"parent":{"key":%s}`, pkJSON)
	}

	endpoint := "/rest/api/2/issue"
	if c.useADF {
		endpoint = "/rest/api/3/issue"
	}
	payload := fmt.Sprintf(
		`{"fields":{"project":{"key":%s},"summary":%s,"description":%s,"issuetype":{"name":%s},"labels":%s%s%s}}`,
		projJSON, summaryJSON, descJSON, typeJSON, labelsJSON, assigneeClause, parentClause,
	)

	data, err := c.postJSON(endpoint, payload)
	if err != nil {
		return "", err
	}
	var result struct {
		Key string `json:"key"`
	}
	return result.Key, json.Unmarshal(data, &result)
}

// CreateEpic creates a Jira Epic and returns its key.
// requester, when non-empty, is prepended to the description for tracking.
// Attempts to set customfield_10011 (Epic Name); falls back without it if the
// field doesn't exist on the target instance.
func (c *JiraClient) CreateEpic(projectKey, title, desc, requester string) (string, error) {
	if requester != "" {
		if desc != "" {
			desc = "Requester: " + requester + "\n\n" + desc
		} else {
			desc = "Requester: " + requester
		}
	}

	summaryJSON, err := json.Marshal(title)
	if err != nil {
		return "", fmt.Errorf("marshal epic title: %w", err)
	}
	var descJSON string
	if c.useADF {
		descJSON, err = toADFJSON(desc)
	} else {
		var b []byte
		b, err = json.Marshal(desc)
		descJSON = string(b)
	}
	if err != nil {
		return "", fmt.Errorf("marshal epic description: %w", err)
	}
	projJSON, err := json.Marshal(projectKey)
	if err != nil {
		return "", fmt.Errorf("marshal project key: %w", err)
	}
	epicNameJSON, err := json.Marshal(title)
	if err != nil {
		return "", fmt.Errorf("marshal epic name: %w", err)
	}

	endpoint := "/rest/api/2/issue"
	if c.useADF {
		endpoint = "/rest/api/3/issue"
	}
	// Try with Epic Name custom field first (required on older Jira Cloud configs).
	payload := fmt.Sprintf(
		`{"fields":{"project":{"key":%s},"summary":%s,"description":%s,"issuetype":{"name":"Epic"},"customfield_10011":%s}}`,
		projJSON, summaryJSON, descJSON, epicNameJSON,
	)
	data, err := c.postJSON(endpoint, payload)
	if err != nil {
		// Only retry without customfield_10011 when Jira rejects the field itself
		// (field-validation 400). All other errors (network, 403, 404) are real failures.
		if !strings.Contains(err.Error(), "customfield_10011") {
			return "", err
		}
		payload = fmt.Sprintf(
			`{"fields":{"project":{"key":%s},"summary":%s,"description":%s,"issuetype":{"name":"Epic"}}}`,
			projJSON, summaryJSON, descJSON,
		)
		data, err = c.postJSON(endpoint, payload)
		if err != nil {
			return "", err
		}
	}

	var result struct {
		Key string `json:"key"`
	}
	return result.Key, json.Unmarshal(data, &result)
}

// DeleteIssue permanently deletes a Jira issue by key.
// Requires the "Delete Issues" permission on the project.
func (c *JiraClient) DeleteIssue(key string) error {
	path := c.baseURL + "/rest/api/2/issue/" + url.PathEscape(key)
	req, err := http.NewRequest("DELETE", path, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.email, c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// GetTransitions returns the available workflow transitions for a Jira issue.
func (c *JiraClient) GetTransitions(key string) ([]Transition, error) {
	data, err := c.get("/rest/api/2/issue/" + url.PathEscape(key) + "/transitions")
	if err != nil {
		return nil, err
	}
	var result struct {
		Transitions []Transition `json:"transitions"`
	}
	return result.Transitions, json.Unmarshal(data, &result)
}

// TransitionIssue moves a Jira issue to a new workflow state by transition ID.
func (c *JiraClient) TransitionIssue(key, transitionID string) error {
	path := c.baseURL + "/rest/api/2/issue/" + url.PathEscape(key) + "/transitions"
	payload, err := json.Marshal(map[string]any{
		"transition": map[string]string{"id": transitionID},
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.email, c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// GetIssue fetches a single Jira issue by key using the REST v2 API.
func (c *JiraClient) GetIssue(key string) (*JiraIssue, error) {
	path := "/rest/api/2/issue/" + url.PathEscape(key) +
		"?fields=summary,description,assignee,reporter,labels,issuetype,status"
	data, err := c.get(path)
	if err != nil {
		return nil, err
	}
	var issue JiraIssue
	return &issue, json.Unmarshal(data, &issue)
}

// GetEpicChildren fetches all child issues of an epic.
// It first tries the modern JQL (parent=) used by team-managed projects, then
// falls back to the classic "Epic Link" field for older project configurations.
func (c *JiraClient) GetEpicChildren(epicKey string) ([]JiraIssue, error) {
	fields := "summary,description,assignee,reporter,labels,issuetype,status"

	issues, err := c.searchIssues(fmt.Sprintf(`parent = "%s"`, epicKey), fields)
	if err != nil {
		return nil, err
	}
	if len(issues) == 0 {
		// Fallback: classic projects store the relationship in "Epic Link".
		classic, err2 := c.searchIssues(fmt.Sprintf(`"Epic Link" = "%s"`, epicKey), fields)
		if err2 == nil {
			issues = classic
		}
	}
	return issues, nil
}

// searchIssues runs a paginated JQL search via the v3 /search/jql POST endpoint
// and returns all matching issues.
func (c *JiraClient) searchIssues(jql, fields string) ([]JiraIssue, error) {
	type searchResult struct {
		Issues        []JiraIssue `json:"issues"`
		NextPageToken string      `json:"nextPageToken"`
	}
	type searchReq struct {
		JQL           string   `json:"jql"`
		Fields        []string `json:"fields"`
		MaxResults    int      `json:"maxResults"`
		NextPageToken string   `json:"nextPageToken,omitempty"`
	}

	const pageSize = 50
	const maxPages = 20
	var all []JiraIssue
	nextToken := ""

	for page := 0; page < maxPages; page++ {
		req := searchReq{
			JQL:           jql,
			Fields:        strings.Split(fields, ","),
			MaxResults:    pageSize,
			NextPageToken: nextToken,
		}
		body, err := json.Marshal(req)
		if err != nil {
			return nil, err
		}
		data, err := c.postJSON("/rest/api/3/search/jql", string(body))
		if err != nil {
			return nil, err
		}
		var result searchResult
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, err
		}
		all = append(all, result.Issues...)
		if result.NextPageToken == "" || len(result.Issues) == 0 {
			break
		}
		nextToken = result.NextPageToken
	}

	return all, nil
}

// IssueDescription extracts a plain-text description from a raw Jira description
// field, which may be a JSON string (REST v2 classic), an ADF document object
// (REST v2 next-gen / v3), or null.
func IssueDescription(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ""
	}
	return strings.TrimSpace(extractADFPlainText(doc))
}

// extractADFPlainText recursively extracts plain text from an ADF node tree.
func extractADFPlainText(node map[string]interface{}) string {
	var sb strings.Builder
	nodeType, _ := node["type"].(string)

	if nodeType == "text" {
		text, _ := node["text"].(string)
		return text
	}
	if nodeType == "hardBreak" {
		return "\n"
	}

	if content, ok := node["content"].([]interface{}); ok {
		for _, child := range content {
			if childMap, ok := child.(map[string]interface{}); ok {
				sb.WriteString(extractADFPlainText(childMap))
			}
		}
	}

	switch nodeType {
	case "paragraph", "heading", "bulletList", "orderedList",
		"listItem", "codeBlock", "blockquote":
		sb.WriteString("\n")
	}

	return sb.String()
}
