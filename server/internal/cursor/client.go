// Package cursor is the Cursor Cloud Agents API v1 (public beta) that Autofix sends fixes to with the
// workspace's key: create an agent with a client id (idempotent), read its run, cancel the run, and the
// two metadata calls the settings check uses. Plain net/http against the endpoints documented at
// https://cursor.com/docs/cloud-agent/api/endpoints (read 27 October 2026); no SDK. The API is in beta,
// so everything Colliedesk knows of it lives in this file, and cursortest stands in for it in tests.
package cursor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// DefaultBase is Cursor's API.
const DefaultBase = "https://api.cursor.com"

// Run statuses. A run is active while CREATING, RUNNING or WAITING_FOR_BACKGROUND_WORK.
const (
	RunCreating   = "CREATING"
	RunRunning    = "RUNNING"
	RunWaiting    = "WAITING_FOR_BACKGROUND_WORK"
	RunFinished   = "FINISHED"
	RunError      = "ERROR"
	RunCancelled  = "CANCELLED"
	RunExpired    = "EXPIRED"
	maxBody       = 1 << 20
	maxMessage    = 300
	defaultClient = 30 * time.Second
)

// Client calls the API with one key. The key is sent as a bearer token and never appears in an error.
type Client struct {
	Base       string
	Key        string
	HTTPClient *http.Client
}

// Error is an answer other than 2xx. Code is Cursor's error code when it gave one (agent_id_conflict,
// run_not_cancellable, repository_access…); Message is its message, cut, never the key; RetryAfter is
// what a 429 asked for.
type Error struct {
	Status     int
	Code       string
	Message    string
	RetryAfter time.Duration
}

func (e *Error) Error() string {
	text := fmt.Sprintf("cursor answered %d", e.Status)
	if e.Code != "" {
		text += " " + e.Code
	}
	if e.Message != "" {
		text += ": " + e.Message
	}
	return text
}

// StatusOf is the HTTP status of a Cursor error, 0 for any other error (the network).
func StatusOf(err error) int {
	var cursorErr *Error
	if errors.As(err, &cursorErr) {
		return cursorErr.Status
	}
	return 0
}

// Agent is the durable agent.
type Agent struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	URL         string `json:"url"`
	LatestRunID string `json:"latestRunId"`
}

// Branch is a branch the agent pushed, with its pull request once opened. RepoURL has no scheme
// (github.com/owner/repo).
type Branch struct {
	RepoURL string `json:"repoUrl"`
	Branch  string `json:"branch"`
	PRURL   string `json:"prUrl"`
}

// Run is one run of an agent. Result is the final reply of a terminal run; Branches are the agent's,
// not only this run's.
type Run struct {
	ID       string `json:"id"`
	AgentID  string `json:"agentId"`
	Status   string `json:"status"`
	Result   string `json:"result"`
	Branches []Branch
}

// Active reports whether the run still works.
func (r Run) Active() bool {
	return r.Status == RunCreating || r.Status == RunRunning || r.Status == RunWaiting
}

// NewAgent is what Autofix creates: one prompt on one repository, a pull request at the end. ID is the
// client id (bc-<uuid>) that makes a second create answer 409 agent_id_conflict instead of a duplicate.
type NewAgent struct {
	ID          string
	Name        string
	Prompt      string
	RepoURL     string
	StartingRef string
}

// Created is the agent and its first run.
type Created struct {
	Agent Agent
	Run   Run
}

// CreateAgent creates the agent and enqueues its first run. No envVars: Cursor refuses them with a
// client id.
func (c Client) CreateAgent(ctx context.Context, in NewAgent) (Created, error) {
	repo := map[string]any{"url": in.RepoURL}
	if in.StartingRef != "" {
		repo["startingRef"] = in.StartingRef
	}
	body := map[string]any{
		"agentId": in.ID, "prompt": map[string]any{"text": in.Prompt}, "repos": []any{repo},
		"autoCreatePR": true, "name": cut(in.Name, 100),
	}
	var out struct {
		Agent Agent   `json:"agent"`
		Run   wireRun `json:"run"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/agents", body, &out); err != nil {
		return Created{}, err
	}
	return Created{Agent: out.Agent, Run: out.Run.run()}, nil
}

// GetAgent reads an agent.
func (c Client) GetAgent(ctx context.Context, id string) (Agent, error) {
	var agent Agent
	err := c.do(ctx, http.MethodGet, "/v1/agents/"+url.PathEscape(id), nil, &agent)
	return agent, err
}

// GetRun reads a run.
func (c Client) GetRun(ctx context.Context, agentID, runID string) (Run, error) {
	var run wireRun
	err := c.do(ctx, http.MethodGet, "/v1/agents/"+url.PathEscape(agentID)+"/runs/"+url.PathEscape(runID), nil, &run)
	return run.run(), err
}

// CancelRun cancels the active run; a run already ended answers 409 run_not_cancellable.
func (c Client) CancelRun(ctx context.Context, agentID, runID string) error {
	return c.do(ctx, http.MethodPost, "/v1/agents/"+url.PathEscape(agentID)+"/runs/"+url.PathEscape(runID)+"/cancel", map[string]any{}, nil)
}

// Me reads the key's own information; it answers 401 for a key Cursor does not know.
func (c Client) Me(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/v1/me", nil, nil)
}

// Repositories lists the GitHub repositories the key's owner reaches through Cursor's GitHub App. It
// is empty for a service account key, and Cursor limits it to one call a minute.
func (c Client) Repositories(ctx context.Context) ([]string, error) {
	var out struct {
		Items []struct {
			URL string `json:"url"`
		} `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/repositories", nil, &out); err != nil {
		return nil, err
	}
	urls := make([]string, 0, len(out.Items))
	for _, item := range out.Items {
		urls = append(urls, item.URL)
	}
	return urls, nil
}

type wireRun struct {
	ID      string `json:"id"`
	AgentID string `json:"agentId"`
	Status  string `json:"status"`
	Result  string `json:"result"`
	Git     struct {
		Branches []Branch `json:"branches"`
	} `json:"git"`
}

func (w wireRun) run() Run {
	return Run{ID: w.ID, AgentID: w.AgentID, Status: w.Status, Result: w.Result, Branches: w.Git.Branches}
}

func (c Client) do(ctx context.Context, method, path string, body, out any) error {
	base := strings.TrimRight(c.Base, "/")
	if base == "" {
		base = DefaultBase
	}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, base+path, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.Key)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultClient}
	}
	response, err := client.Do(request)
	if err != nil {
		// The URL holds no secret; the key is only in a header, which net/http never prints.
		return fmt.Errorf("call cursor: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBody))
	if err != nil {
		return fmt.Errorf("read cursor's answer: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return errorOf(response, data, c.Key)
	}
	if out == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode cursor's answer: %w", err)
	}
	return nil
}

// errorOf reads Cursor's error, whichever of its documented shapes it has ({"code","message"} or
// {"error","message"}, error a string or an object), and removes the key should Cursor echo it.
func errorOf(response *http.Response, data []byte, key string) *Error {
	out := &Error{Status: response.StatusCode}
	var body struct {
		Code    string          `json:"code"`
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
	}
	if json.Unmarshal(data, &body) == nil {
		out.Code, out.Message = body.Code, body.Message
		var text string
		var nested struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		switch {
		case json.Unmarshal(body.Error, &text) == nil:
			if out.Message == "" {
				out.Message = text
			}
		case json.Unmarshal(body.Error, &nested) == nil:
			if out.Code == "" {
				out.Code = nested.Code
			}
			if out.Message == "" {
				out.Message = nested.Message
			}
		}
	}
	if !codePattern(out.Code) {
		out.Code = ""
	}
	if key != "" {
		out.Message = strings.ReplaceAll(out.Message, key, "[key]")
	}
	out.Message = cut(out.Message, maxMessage)
	if response.StatusCode == http.StatusTooManyRequests {
		out.RetryAfter = time.Minute
		if seconds, err := strconv.Atoi(strings.TrimSpace(response.Header.Get("Retry-After"))); err == nil && seconds > 0 && seconds <= 3600 {
			out.RetryAfter = time.Duration(seconds) * time.Second
		}
	}
	return out
}

func codePattern(code string) bool {
	if len(code) > 60 {
		return false
	}
	for _, r := range code {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}

// cut shortens text to at most n characters, on a character boundary.
func cut(text string, n int) string {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) <= n {
		return text
	}
	return string([]rune(text)[:n-1]) + "…"
}
