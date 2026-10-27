// Command cursor-stand-in serves cursortest, the local stand-in for the Cursor Cloud Agents API v1 that
// Autofix calls (create an agent, read its run, cancel it, the key and its repositories), so Autofix can
// be tried and tested without a Cursor account or key. Point an end-to-end build of collie at it with
// COLLIEDESK_E2E_CURSOR_URL. A test scripts the runs of the next agents with POST /stand-in/script and
// reads what Cursor received with GET /stand-in/state; POST /stand-in/fail makes its next calls fail.
//
//	go run ./cmd/cursor-stand-in -listen 127.0.0.1:8098 -key cursor-e2e-key
package main

import (
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"slices"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/cursor/cursortest"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8098", "listen address")
	key := flag.String("key", "cursor-stand-in-key", "the API key Cursor accepts; every other key answers 401")
	flag.Parse()
	fake := cursortest.NewHandler(*key)

	mux := http.NewServeMux()
	mux.Handle("/", fake)
	// The steps every agent created from now on follows, one per read of its run:
	// [{"status": "RUNNING"}, {"status": "FINISHED", "branch": "…", "pr_url": "https://github.com/o/r/pull/7"}].
	mux.HandleFunc("POST /stand-in/script", decoded(func(in []struct {
		Status string `json:"status"`
		Result string `json:"result"`
		Branch string `json:"branch"`
		PRURL  string `json:"pr_url"`
	}) {
		steps := make([]cursortest.Step, 0, len(in))
		for _, step := range in {
			steps = append(steps, cursortest.Step{Status: step.Status, Result: step.Result, Branch: step.Branch, PRURL: step.PRURL})
		}
		fake.Script(steps...)
	}))
	// Makes the next calls of one endpoint ("create", "run", "cancel", "me", "repositories") answer a status:
	// {"endpoint": "create", "status": 429, "retry_after": 30, "times": 2}.
	mux.HandleFunc("POST /stand-in/fail", func(response http.ResponseWriter, request *http.Request) {
		var in struct {
			Endpoint   string `json:"endpoint"`
			Status     int    `json:"status"`
			Code       string `json:"code"`
			Message    string `json:"message"`
			RetryAfter int    `json:"retry_after"`
			Times      int    `json:"times"`
		}
		if err := json.NewDecoder(io.LimitReader(request.Body, 1<<20)).Decode(&in); err != nil {
			http.Error(response, "bad JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		if in.Times < 1 {
			in.Times = 1
		}
		if !slices.Contains([]string{"create", "run", "cancel", "me", "repositories"}, in.Endpoint) || in.Status < 400 || in.Status > 599 || in.Times > 20 {
			http.Error(response, `want endpoint create|run|cancel|me|repositories, status 400..599, times 1..20`, http.StatusBadRequest)
			return
		}
		failures := make([]cursortest.Failure, in.Times)
		for i := range failures {
			failures[i] = cursortest.Failure{Status: in.Status, Code: in.Code, Message: in.Message, RetryAfter: in.RetryAfter}
		}
		fake.Fail(in.Endpoint, failures...)
		response.WriteHeader(http.StatusNoContent)
	})
	// What Cursor received: each create's agent id, repository and prompt (never the key), and the runs cancelled.
	mux.HandleFunc("GET /stand-in/state", func(response http.ResponseWriter, _ *http.Request) {
		type created struct {
			AgentID string `json:"agent_id"`
			Repo    string `json:"repo"`
			Prompt  string `json:"prompt"`
		}
		creates := []created{}
		for _, create := range fake.Creates() {
			id, _ := create.Body["agentId"].(string)
			prompt, _ := create.Body["prompt"].(map[string]any)
			text, _ := prompt["text"].(string)
			repo := ""
			if repos, _ := create.Body["repos"].([]any); len(repos) == 1 {
				first, _ := repos[0].(map[string]any)
				repo, _ = first["url"].(string)
			}
			creates = append(creates, created{AgentID: id, Repo: repo, Prompt: text})
		}
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]any{"creates": creates, "cancels": fake.Cancels(), "agents": fake.Agents()})
	})
	server := &http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("Cursor stand-in on http://%s", *listen)
	log.Fatal(server.ListenAndServe())
}

// decoded answers 204 after handing the JSON body to apply, 400 when it does not decode.
func decoded[T any](apply func(T)) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		var in T
		if err := json.NewDecoder(io.LimitReader(request.Body, 1<<20)).Decode(&in); err != nil {
			http.Error(response, "bad JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		apply(in)
		response.WriteHeader(http.StatusNoContent)
	}
}
