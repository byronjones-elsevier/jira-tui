package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureServer returns an httptest.Server that records the last request body
// and responds with the provided JSON payload and status code.
func captureServer(t *testing.T, statusCode int, responseJSON string) (*httptest.Server, *string) {
	t.Helper()
	var captured string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		fmt.Fprint(w, responseJSON)
	}))
	t.Cleanup(srv.Close)
	return srv, &captured
}

// ── CreateIssue ───────────────────────────────────────────────────────────────

func TestCreateIssue_BasicPayload(t *testing.T) {
	srv, body := captureServer(t, 201, `{"key":"PROJ-42"}`)
	client := newJiraClient(srv.URL, "user@example.com", "token")

	key, err := client.CreateIssue("PROJ", "Task", Ticket{
		Title:       "My Ticket",
		Description: "A description",
		Labels:      []string{"label1"},
	}, "", "")

	require.NoError(t, err)
	assert.Equal(t, "PROJ-42", key)
	assert.Contains(t, *body, `"summary":"My Ticket"`)
	assert.Contains(t, *body, `"description":"A description"`)
	assert.Contains(t, *body, `"label1"`)
	assert.Contains(t, *body, `"issuetype":{"name":"Task"}`)
	assert.Contains(t, *body, `"project":{"key":"PROJ"}`)
	assert.NotContains(t, *body, `"parent"`, "no parent key should be set in normal mode")
	assert.NotContains(t, *body, `"assignee"`, "no assignee when assigneeID is empty")
}

func TestCreateIssue_WithParentKey(t *testing.T) {
	srv, body := captureServer(t, 201, `{"key":"PROJ-43"}`)
	client := newJiraClient(srv.URL, "user@example.com", "token")

	key, err := client.CreateIssue("PROJ", "Story", Ticket{Title: "Child Ticket"}, "", "EPIC-1")

	require.NoError(t, err)
	assert.Equal(t, "PROJ-43", key)
	assert.Contains(t, *body, `"parent":{"key":"EPIC-1"}`)
}

func TestCreateIssue_WithAssignee(t *testing.T) {
	srv, body := captureServer(t, 201, `{"key":"PROJ-44"}`)
	client := newJiraClient(srv.URL, "user@example.com", "token")

	_, err := client.CreateIssue("PROJ", "Task", Ticket{Title: "Assigned"}, "account-id-abc", "")

	require.NoError(t, err)
	assert.Contains(t, *body, `"assignee":{"accountId":"account-id-abc"}`)
}

func TestCreateIssue_LabelsSpacesConvertedToDashes(t *testing.T) {
	srv, body := captureServer(t, 201, `{"key":"PROJ-45"}`)
	client := newJiraClient(srv.URL, "user@example.com", "token")

	_, err := client.CreateIssue("PROJ", "Task", Ticket{
		Title:  "Labeled",
		Labels: []string{"cost optimization", "tech-debt"},
	}, "", "")

	require.NoError(t, err)
	assert.Contains(t, *body, `"cost-optimization"`)
	assert.Contains(t, *body, `"tech-debt"`)
}

func TestCreateIssue_APIError(t *testing.T) {
	srv, _ := captureServer(t, 400, `{"errorMessages":["project is required"]}`)
	client := newJiraClient(srv.URL, "user@example.com", "token")

	_, err := client.CreateIssue("", "Task", Ticket{Title: "Bad"}, "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 400")
}

func TestCreateIssue_JSONEscaping(t *testing.T) {
	srv, body := captureServer(t, 201, `{"key":"PROJ-46"}`)
	client := newJiraClient(srv.URL, "user@example.com", "token")

	// Quotes and backslashes in title/description must be JSON-escaped.
	_, err := client.CreateIssue("PROJ", "Task", Ticket{
		Title:       `Say "hello" \world`,
		Description: "Line1\nLine2",
	}, "", "")

	require.NoError(t, err)
	assert.Contains(t, *body, `"summary":"Say \"hello\" \\world"`)
	assert.Contains(t, *body, `"description":"Line1\nLine2"`)
}

// ── CreateEpic ────────────────────────────────────────────────────────────────

func TestCreateEpic_BasicPayload(t *testing.T) {
	srv, body := captureServer(t, 201, `{"key":"PROJ-100"}`)
	client := newJiraClient(srv.URL, "user@example.com", "token")

	key, err := client.CreateEpic("PROJ", "My Epic", "Epic description", "")

	require.NoError(t, err)
	assert.Equal(t, "PROJ-100", key)
	assert.Contains(t, *body, `"issuetype":{"name":"Epic"}`)
	assert.Contains(t, *body, `"summary":"My Epic"`)
	assert.Contains(t, *body, `"description":"Epic description"`)
}

func TestCreateEpic_RequesterPrependedToDescription(t *testing.T) {
	srv, body := captureServer(t, 201, `{"key":"PROJ-101"}`)
	client := newJiraClient(srv.URL, "user@example.com", "token")

	_, err := client.CreateEpic("PROJ", "Epic", "Existing desc", "alice@example.com")

	require.NoError(t, err)
	// description should start with "Requester: alice@example.com"
	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(*body), &payload))
	fields := payload["fields"].(map[string]interface{})
	desc := fields["description"].(string)
	assert.True(t, strings.HasPrefix(desc, "Requester: alice@example.com"),
		"got description: %s", desc)
	assert.Contains(t, desc, "Existing desc")
}

func TestCreateEpic_RequesterOnly_NoOriginalDesc(t *testing.T) {
	srv, body := captureServer(t, 201, `{"key":"PROJ-102"}`)
	client := newJiraClient(srv.URL, "user@example.com", "token")

	_, err := client.CreateEpic("PROJ", "Epic", "", "bob@example.com")

	require.NoError(t, err)
	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(*body), &payload))
	fields := payload["fields"].(map[string]interface{})
	desc := fields["description"].(string)
	assert.Equal(t, "Requester: bob@example.com", desc)
}

func TestCreateEpic_FallbackWithoutCustomField(t *testing.T) {
	// First call (with customfield_10011) returns 400; second call (without) succeeds.
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(string(body), "customfield_10011") {
			w.WriteHeader(400)
			fmt.Fprint(w, `{"errorMessages":[],"errors":{"customfield_10011":"Field not found"}}`)
			return
		}
		w.WriteHeader(201)
		fmt.Fprint(w, `{"key":"PROJ-103"}`)
	}))
	t.Cleanup(srv.Close)

	client := newJiraClient(srv.URL, "user@example.com", "token")
	key, err := client.CreateEpic("PROJ", "My Epic", "desc", "")

	require.NoError(t, err)
	assert.Equal(t, "PROJ-103", key)
	assert.Equal(t, 2, callCount, "should have retried once without the custom field")
}

func TestCreateEpic_BothCallsFail(t *testing.T) {
	srv, _ := captureServer(t, 403, `{"errorMessages":["Unauthorized"]}`)
	client := newJiraClient(srv.URL, "user@example.com", "token")

	_, err := client.CreateEpic("PROJ", "Epic", "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 403")
}

// ── VerifyAuth ────────────────────────────────────────────────────────────────

func TestVerifyAuth_Success(t *testing.T) {
	srv, _ := captureServer(t, 200, `{"accountId":"abc123","displayName":"Alice","emailAddress":"alice@example.com"}`)
	client := newJiraClient(srv.URL, "alice@example.com", "token")

	user, err := client.VerifyAuth()
	require.NoError(t, err)
	assert.Equal(t, "abc123", user.AccountID)
	assert.Equal(t, "Alice", user.DisplayName)
}

func TestVerifyAuth_Failure(t *testing.T) {
	srv, _ := captureServer(t, 401, `{"errorMessages":["You are not authenticated"]}`)
	client := newJiraClient(srv.URL, "bad@example.com", "bad-token")

	_, err := client.VerifyAuth()
	require.Error(t, err)
}

// ── ResolveAccountID ──────────────────────────────────────────────────────────

func TestResolveAccountID_Found(t *testing.T) {
	srv, _ := captureServer(t, 200, `[{"accountId":"id-123","emailAddress":"alice@example.com","displayName":"Alice"}]`)
	client := newJiraClient(srv.URL, "user@example.com", "token")

	id, err := client.ResolveAccountID("alice@example.com")
	require.NoError(t, err)
	assert.Equal(t, "id-123", id)
}

func TestResolveAccountID_NotFound(t *testing.T) {
	srv, _ := captureServer(t, 200, `[]`)
	client := newJiraClient(srv.URL, "user@example.com", "token")

	id, err := client.ResolveAccountID("nobody@example.com")
	require.NoError(t, err)
	assert.Empty(t, id, "empty string expected when no match")
}

func TestResolveAccountID_EmptyInput(t *testing.T) {
	// Empty email should short-circuit without any HTTP call.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("HTTP call should not have been made for empty email")
	}))
	t.Cleanup(srv.Close)

	client := newJiraClient(srv.URL, "user@example.com", "token")
	id, err := client.ResolveAccountID("")
	require.NoError(t, err)
	assert.Empty(t, id)
}

// ── GetBoards ─────────────────────────────────────────────────────────────────

func TestGetBoards_SinglePage(t *testing.T) {
	resp := `{
		"isLast": true,
		"values": [
			{"id":1,"name":"Finance Board","location":{"projectKey":"FIN"}},
			{"id":2,"name":"Engineering","location":{"projectKey":"ENG"}}
		]
	}`
	srv, _ := captureServer(t, 200, resp)
	client := newJiraClient(srv.URL, "user@example.com", "token")

	boards, err := client.GetBoards()
	require.NoError(t, err)
	require.Len(t, boards, 2)
	assert.Equal(t, "FIN", boards[0].ProjectKey)
	assert.Equal(t, "ENG", boards[1].ProjectKey)
}

func TestGetBoards_Pagination(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		if callCount == 1 {
			fmt.Fprint(w, `{"isLast":false,"values":[{"id":1,"name":"Board A","location":{"projectKey":"A"}}]}`)
		} else {
			fmt.Fprint(w, `{"isLast":true,"values":[{"id":2,"name":"Board B","location":{"projectKey":"B"}}]}`)
		}
	}))
	t.Cleanup(srv.Close)

	client := newJiraClient(srv.URL, "user@example.com", "token")
	boards, err := client.GetBoards()
	require.NoError(t, err)
	assert.Len(t, boards, 2)
	assert.Equal(t, 2, callCount, "should have fetched both pages")
}

// ── GetIssue ──────────────────────────────────────────────────────────────────

func TestGetIssue_ReturnsIssue(t *testing.T) {
	resp := `{
		"key": "PROJ-123",
		"fields": {
			"summary": "My Epic",
			"description": "An epic description",
			"issuetype": {"name": "Epic"},
			"status": {"name": "In Progress"},
			"assignee": {"accountId": "acc1", "displayName": "Alice", "emailAddress": "alice@example.com"},
			"reporter": {"accountId": "acc2", "displayName": "Bob", "emailAddress": "bob@example.com"},
			"labels": ["finops", "budget"]
		}
	}`
	srv, _ := captureServer(t, 200, resp)
	client := newJiraClient(srv.URL, "user@example.com", "token")

	issue, err := client.GetIssue("PROJ-123")
	require.NoError(t, err)
	assert.Equal(t, "PROJ-123", issue.Key)
	assert.Equal(t, "My Epic", issue.Fields.Summary)
	assert.Equal(t, "Epic", issue.Fields.IssueType.Name)
	assert.Equal(t, "alice@example.com", issue.Fields.Assignee.EmailAddress)
	assert.Equal(t, "bob@example.com", issue.Fields.Reporter.EmailAddress)
	assert.Equal(t, []string{"finops", "budget"}, issue.Fields.Labels)
}

func TestGetIssue_HTTPError(t *testing.T) {
	srv, _ := captureServer(t, 404, `{"errorMessages":["Issue does not exist"]}`)
	client := newJiraClient(srv.URL, "user@example.com", "token")

	_, err := client.GetIssue("PROJ-999")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 404")
}

// ── GetEpicChildren ───────────────────────────────────────────────────────────

func TestGetEpicChildren_ReturnsChildren(t *testing.T) {
	resp := `{
		"total": 2,
		"issues": [
			{"key":"PROJ-2","fields":{"summary":"Child 1","issuetype":{"name":"Task"},"labels":["a"]}},
			{"key":"PROJ-3","fields":{"summary":"Child 2","issuetype":{"name":"Story"},"labels":[]}}
		]
	}`
	srv, _ := captureServer(t, 200, resp)
	client := newJiraClient(srv.URL, "user@example.com", "token")

	children, err := client.GetEpicChildren("PROJ-1")
	require.NoError(t, err)
	require.Len(t, children, 2)
	assert.Equal(t, "PROJ-2", children[0].Key)
	assert.Equal(t, "Child 1", children[0].Fields.Summary)
}

func TestGetEpicChildren_FallbackToEpicLink(t *testing.T) {
	// parent= returns 0, Epic Link returns 1 result
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		q := r.URL.Query().Get("jql")
		if strings.Contains(q, "parent") {
			fmt.Fprint(w, `{"total":0,"issues":[]}`)
		} else {
			fmt.Fprint(w, `{"total":1,"issues":[{"key":"PROJ-5","fields":{"summary":"Classic Child","issuetype":{"name":"Task"},"labels":[]}}]}`)
		}
	}))
	t.Cleanup(srv.Close)

	client := newJiraClient(srv.URL, "user@example.com", "token")
	children, err := client.GetEpicChildren("PROJ-1")
	require.NoError(t, err)
	require.Len(t, children, 1)
	assert.Equal(t, "PROJ-5", children[0].Key)
	assert.Equal(t, 2, callCount, "should have tried both JQL approaches")
}

// ── IssueDescription ─────────────────────────────────────────────────────────

func TestIssueDescription_PlainString(t *testing.T) {
	raw := json.RawMessage(`"This is a plain text description"`)
	assert.Equal(t, "This is a plain text description", IssueDescription(raw))
}

func TestIssueDescription_Null(t *testing.T) {
	assert.Equal(t, "", IssueDescription(json.RawMessage(`null`)))
	assert.Equal(t, "", IssueDescription(nil))
}

func TestIssueDescription_ADFDocument(t *testing.T) {
	adf := json.RawMessage(`{
		"version": 1,
		"type": "doc",
		"content": [
			{
				"type": "paragraph",
				"content": [
					{"type": "text", "text": "Hello"},
					{"type": "hardBreak"},
					{"type": "text", "text": "World"}
				]
			}
		]
	}`)
	desc := IssueDescription(adf)
	assert.Contains(t, desc, "Hello")
	assert.Contains(t, desc, "World")
}
