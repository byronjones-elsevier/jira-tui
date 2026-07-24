package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// JiraClient wraps Jira's REST API with basic-auth credentials.
type JiraClient struct {
	baseURL string
	email   string
	token   string
	http    *http.Client
}

// User represents the authenticated Jira user.
type User struct {
	AccountID   string `json:"accountId"`
	DisplayName string `json:"displayName"`
	Email       string `json:"emailAddress"`
}

// Board is a Jira agile board with its resolved project key.
type Board struct {
	ID         int
	Name       string
	ProjectKey string
}

// CreateResult holds the outcome of a single ticket creation.
type CreateResult struct {
	Ticket  Ticket
	Key     string // e.g. "FINOPS-7"
	URL     string
	Skipped bool
	Err     error
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
	body, _ := io.ReadAll(resp.Body)
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
	b, _ := io.ReadAll(resp.Body)
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
	var boards []Board
	startAt := 0

	for {
		path := fmt.Sprintf("/rest/agile/1.0/board?maxResults=%d&startAt=%d", pageSize, startAt)
		data, err := c.get(path)
		if err != nil {
			return nil, err
		}
		var page boardPage
		if err := json.Unmarshal(data, &page); err != nil {
			return nil, err
		}
		for _, v := range page.Values {
			boards = append(boards, Board{
				ID:         v.ID,
				Name:       v.Name,
				ProjectKey: v.Location.ProjectKey,
			})
		}
		if page.IsLast || len(page.Values) == 0 {
			break
		}
		startAt += len(page.Values)
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

// CreateIssue creates a single Jira issue and returns its key (e.g. "PROJ-42").
// parentKey, when non-empty, sets the parent field (linking a child to an epic or issue).
func (c *JiraClient) CreateIssue(projectKey, issueType string, t Ticket, assigneeID, parentKey string) (string, error) {
	labelParts := make([]string, 0, len(t.Labels))
	for _, l := range t.Labels {
		l = strings.TrimSpace(l)
		l = strings.ReplaceAll(l, " ", "-")
		if l != "" {
			b, _ := json.Marshal(l)
			labelParts = append(labelParts, string(b))
		}
	}
	labelsJSON := "[" + strings.Join(labelParts, ",") + "]"

	summaryJSON, _ := json.Marshal(t.Title)
	descJSON, _ := json.Marshal(t.Description)
	projJSON, _ := json.Marshal(projectKey)
	typeJSON, _ := json.Marshal(issueType)

	assigneeClause := ""
	if assigneeID != "" {
		aid, _ := json.Marshal(assigneeID)
		assigneeClause = fmt.Sprintf(`,"assignee":{"accountId":%s}`, aid)
	}

	parentClause := ""
	if parentKey != "" {
		pkJSON, _ := json.Marshal(parentKey)
		parentClause = fmt.Sprintf(`,"parent":{"key":%s}`, pkJSON)
	}

	payload := fmt.Sprintf(
		`{"fields":{"project":{"key":%s},"summary":%s,"description":%s,"issuetype":{"name":%s},"labels":%s%s%s}}`,
		projJSON, summaryJSON, descJSON, typeJSON, labelsJSON, assigneeClause, parentClause,
	)

	data, err := c.postJSON("/rest/api/2/issue", payload)
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

	summaryJSON, _ := json.Marshal(title)
	descJSON, _ := json.Marshal(desc)
	projJSON, _ := json.Marshal(projectKey)
	epicNameJSON, _ := json.Marshal(title)

	// Try with Epic Name custom field first (required on older Jira Cloud configs).
	payload := fmt.Sprintf(
		`{"fields":{"project":{"key":%s},"summary":%s,"description":%s,"issuetype":{"name":"Epic"},"customfield_10011":%s}}`,
		projJSON, summaryJSON, descJSON, epicNameJSON,
	)
	data, err := c.postJSON("/rest/api/2/issue", payload)
	if err != nil {
		// Retry without the custom field — some instances don't require it.
		payload = fmt.Sprintf(
			`{"fields":{"project":{"key":%s},"summary":%s,"description":%s,"issuetype":{"name":"Epic"}}}`,
			projJSON, summaryJSON, descJSON,
		)
		data, err = c.postJSON("/rest/api/2/issue", payload)
		if err != nil {
			return "", err
		}
	}

	var result struct {
		Key string `json:"key"`
	}
	return result.Key, json.Unmarshal(data, &result)
}
