// Package cursortest is a local stand-in for the Cursor Cloud Agents API v1 that Autofix calls: create
// an agent with a client id (a second create answers 409 agent_id_conflict), read the agent and its run,
// cancel the run (409 run_not_cancellable once it ended), the key's information and its repositories.
// A run follows a script the test sets, one step per read. Tests and the e2e build point the Cursor base
// at it; no real Cursor account or key is involved. Answers follow the shapes documented on 27 October
// 2026.
package cursortest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"
)

// Step is one state of a run, read in turn by each GET of the run; the last one stays.
type Step struct {
	Status string
	Result string
	// PRURL and Branch, when set, are the agent's pushed branch from this step on.
	Branch string
	PRURL  string
}

// Failure is a scripted answer for the next calls of one endpoint ("create", "run", "cancel", "me",
// "repositories").
type Failure struct {
	Status     int
	Code       string
	Message    string
	RetryAfter int
}

// Created is one create the stand-in received, as sent.
type Created struct {
	Key  string
	Body map[string]any
}

type agent struct {
	id, key, repoURL string
	runID            string
	steps            []Step
	read             int
	cancelled        bool
	branch           Branch
}

// Branch is the agent's pushed branch.
type Branch struct{ RepoURL, Branch, PRURL string }

// Server is the stand-in. Change its fields between calls with the methods; they lock.
type Server struct {
	mu       sync.Mutex
	keys     map[string]bool
	agents   map[string]*agent
	creates  []Created
	cancels  []string
	calls    map[string]int
	failures map[string][]Failure
	// script is the run every new agent follows.
	script []Step
	repos  []string
	server *httptest.Server
}

// New starts the stand-in with the keys it accepts; every other key answers 401.
func New(keys ...string) *Server {
	s := &Server{keys: map[string]bool{}, agents: map[string]*agent{}, calls: map[string]int{}, failures: map[string][]Failure{},
		script: []Step{{Status: "RUNNING"}}}
	for _, key := range keys {
		s.keys[key] = true
	}
	s.server = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

// URL is the base to point the client at.
func (s *Server) URL() string { return s.server.URL }

// Close stops it.
func (s *Server) Close() { s.server.Close() }

// Script sets the steps every agent created from now on follows.
func (s *Server) Script(steps ...Step) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.script = append([]Step{}, steps...)
}

// ScriptAgent sets the steps of an agent already created, from its next read on.
func (s *Server) ScriptAgent(id string, steps ...Step) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a := s.agents[id]; a != nil {
		a.steps, a.read = append([]Step{}, steps...), 0
	}
}

// Repositories sets what /v1/repositories lists (https URLs).
func (s *Server) Repositories(urls ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.repos = append([]string{}, urls...)
}

// Fail queues answers for the next calls of an endpoint.
func (s *Server) Fail(endpoint string, failures ...Failure) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failures[endpoint] = append(s.failures[endpoint], failures...)
}

// Creates are the creates received, in order (refused ones included).
func (s *Server) Creates() []Created {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Created{}, s.creates...)
}

// Cancels are the run ids cancelled.
func (s *Server) Cancels() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.cancels...)
}

// Calls counts the calls of an endpoint, refused ones included.
func (s *Server) Calls(endpoint string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[endpoint]
}

// Agents is how many agents exist.
func (s *Server) Agents() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.agents)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func fail(w http.ResponseWriter, f Failure) {
	if f.RetryAfter > 0 {
		w.Header().Set("Retry-After", fmt.Sprint(f.RetryAfter))
	}
	writeJSON(w, f.Status, map[string]any{"code": f.Code, "message": f.Message})
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	endpoint := ""
	switch {
	case r.Method == http.MethodPost && len(path) == 2 && path[1] == "agents":
		endpoint = "create"
	case r.Method == http.MethodGet && len(path) == 3 && path[1] == "agents":
		endpoint = "agent"
	case r.Method == http.MethodGet && len(path) == 5 && path[3] == "runs":
		endpoint = "run"
	case r.Method == http.MethodPost && len(path) == 6 && path[5] == "cancel":
		endpoint = "cancel"
	case r.Method == http.MethodGet && len(path) == 2 && path[1] == "me":
		endpoint = "me"
	case r.Method == http.MethodGet && len(path) == 2 && path[1] == "repositories":
		endpoint = "repositories"
	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"code": "not_found", "message": "No such endpoint."})
		return
	}
	if path[0] != "v1" {
		writeJSON(w, http.StatusNotFound, map[string]any{"code": "not_found", "message": "No such endpoint."})
		return
	}
	s.calls[endpoint]++
	key, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if endpoint == "create" {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.creates = append(s.creates, Created{Key: key, Body: body})
		if !ok || !s.keys[key] {
			fail(w, Failure{Status: http.StatusUnauthorized, Message: "Invalid API key"})
			return
		}
		if queued := s.failures[endpoint]; len(queued) > 0 {
			s.failures[endpoint] = queued[1:]
			fail(w, queued[0])
			return
		}
		s.create(w, key, body)
		return
	}
	if !ok || !s.keys[key] {
		fail(w, Failure{Status: http.StatusUnauthorized, Message: "Invalid API key"})
		return
	}
	if queued := s.failures[endpoint]; len(queued) > 0 {
		s.failures[endpoint] = queued[1:]
		fail(w, queued[0])
		return
	}
	switch endpoint {
	case "me":
		writeJSON(w, http.StatusOK, map[string]any{"apiKeyName": "Colliedesk", "createdAt": "2026-10-01T00:00:00Z"})
	case "repositories":
		items := []map[string]any{}
		for _, repo := range s.repos {
			items = append(items, map[string]any{"url": repo})
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	case "agent":
		a := s.agents[path[2]]
		if a == nil || a.key != key {
			writeJSON(w, http.StatusNotFound, map[string]any{"code": "not_found", "message": "Agent not found"})
			return
		}
		writeJSON(w, http.StatusOK, a.wire())
	case "run":
		a := s.agents[path[2]]
		if a == nil || a.key != key || a.runID != path[4] {
			writeJSON(w, http.StatusNotFound, map[string]any{"code": "not_found", "message": "Run not found"})
			return
		}
		writeJSON(w, http.StatusOK, a.next())
	case "cancel":
		a := s.agents[path[2]]
		if a == nil || a.key != key || a.runID != path[4] {
			writeJSON(w, http.StatusNotFound, map[string]any{"code": "not_found", "message": "Run not found"})
			return
		}
		if a.cancelled || !active(a.current().Status) {
			writeJSON(w, http.StatusConflict, map[string]any{"code": "run_not_cancellable", "message": "The run is not active."})
			return
		}
		a.cancelled = true
		s.cancels = append(s.cancels, a.runID)
		writeJSON(w, http.StatusOK, map[string]any{"id": a.runID})
	}
}

func (s *Server) create(w http.ResponseWriter, key string, body map[string]any) {
	id, _ := body["agentId"].(string)
	if _, withEnv := body["envVars"]; withEnv && id != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"code": "validation_error", "message": "envVars cannot be combined with agentId"})
		return
	}
	repos, _ := body["repos"].([]any)
	prompt, _ := body["prompt"].(map[string]any)
	if text, _ := prompt["text"].(string); text == "" || len(repos) != 1 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"code": "validation_error", "message": "prompt.text and one repository are required"})
		return
	}
	repo, _ := repos[0].(map[string]any)
	repoURL, _ := repo["url"].(string)
	if id == "" {
		id = fmt.Sprintf("bc-stand-in-%d", len(s.agents)+1)
	}
	if !strings.HasPrefix(id, "bc-") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"code": "validation_error", "message": "agentId must be bc-<uuid>"})
		return
	}
	if _, taken := s.agents[id]; taken {
		writeJSON(w, http.StatusConflict, map[string]any{"code": "agent_id_conflict", "message": "An agent with this id already exists."})
		return
	}
	a := &agent{id: id, key: key, repoURL: repoURL, runID: "run-" + strings.TrimPrefix(id, "bc-"), steps: append([]Step{}, s.script...)}
	s.agents[id] = a
	run := map[string]any{"id": a.runID, "agentId": id, "status": "CREATING", "createdAt": now(), "updatedAt": now()}
	writeJSON(w, http.StatusCreated, map[string]any{"agent": a.wire(), "run": run})
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

func active(status string) bool {
	return status == "CREATING" || status == "RUNNING" || status == "WAITING_FOR_BACKGROUND_WORK"
}

func (a *agent) wire() map[string]any {
	return map[string]any{"id": a.id, "name": "fix", "status": "ACTIVE", "url": "https://cursor.com/agents/" + a.id,
		"latestRunId": a.runID, "createdAt": now(), "updatedAt": now()}
}

func (a *agent) current() Step {
	if a.cancelled {
		return Step{Status: "CANCELLED"}
	}
	if len(a.steps) == 0 {
		return Step{Status: "CREATING"}
	}
	index := a.read - 1
	if index < 0 {
		index = 0
	}
	if index >= len(a.steps) {
		index = len(a.steps) - 1
	}
	return a.steps[index]
}

// next is the run as one read sees it, moving the script one step.
func (a *agent) next() map[string]any {
	if a.read < len(a.steps) {
		a.read++
	}
	step := a.current()
	if step.PRURL != "" || step.Branch != "" {
		a.branch = Branch{RepoURL: strings.TrimPrefix(a.repoURL, "https://"), Branch: step.Branch, PRURL: step.PRURL}
	}
	run := map[string]any{"id": a.runID, "agentId": a.id, "status": step.Status, "createdAt": now(), "updatedAt": now()}
	if !active(step.Status) {
		run["result"] = step.Result
		run["durationMs"] = 1000
	}
	if a.branch != (Branch{}) {
		entry := map[string]any{"repoUrl": a.branch.RepoURL}
		if a.branch.Branch != "" {
			entry["branch"] = a.branch.Branch
		}
		if a.branch.PRURL != "" {
			entry["prUrl"] = a.branch.PRURL
		}
		run["git"] = map[string]any{"branches": []any{entry}}
	}
	return run
}
