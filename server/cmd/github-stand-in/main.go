// Command github-stand-in serves githubtest, the local stand-in for the GitHub endpoints the Colliedesk
// GitHub App calls (install and authorize pages, tokens, installations, repositories, issues, pull
// requests, GraphQL), so Autofix can be tried and tested without a GitHub account or App. Point an
// end-to-end build of collie at it with COLLIEDESK_E2E_GITHUB_URL and the same App settings
// (GITHUB_APP_PRIVATE_KEY from the -key file). A test arranges GitHub's side with POST /stand-in/*,
// and /stand-in/webhook (or /stand-in/webhook/pull_request and /stand-in/webhook/issues, which fill in GitHub's
// objects) delivers a signed event to the server; GET /stand-in/issues reads the issues fixes created.
//
//	go run ./cmd/github-stand-in -listen 127.0.0.1:8097 -key /tmp/app.pem \
//	  -callback-url http://127.0.0.1:8080/api/v1/autofix/github/callback -webhook-url http://127.0.0.1:8080/api/v1/webhooks/github
package main

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/github/githubtest"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8097", "listen address")
	keyFile := flag.String("key", "", "the App's private key (PEM); created there when the file does not exist")
	appID := flag.String("app-id", "4242", "GITHUB_APP_ID the server is configured with")
	slug := flag.String("slug", "colliedesk-test", "GITHUB_APP_SLUG")
	clientID := flag.String("client-id", "Iv1.stand-in", "GITHUB_APP_CLIENT_ID")
	clientSecret := flag.String("client-secret", "stand-in-secret", "GITHUB_APP_CLIENT_SECRET")
	webhookSecret := flag.String("webhook-secret", "stand-in-webhook-secret", "GITHUB_APP_WEBHOOK_SECRET")
	callbackURL := flag.String("callback-url", "http://127.0.0.1:8080/api/v1/autofix/github/callback", "the App's callback URL")
	webhookURL := flag.String("webhook-url", "http://127.0.0.1:8080/api/v1/webhooks/github", "where /stand-in/webhook delivers")
	flag.Parse()
	key, err := loadKey(*keyFile)
	if err != nil {
		log.Fatal(err)
	}
	fake := githubtest.NewWithKey(key)
	fake.AppID, fake.Slug, fake.ClientID, fake.ClientSecret, fake.WebhookSecret = *appID, *slug, *clientID, *clientSecret, *webhookSecret
	fake.CallbackURL = *callbackURL
	fake.URL = "http://" + *listen

	mux := http.NewServeMux()
	mux.Handle("/", fake)
	mux.HandleFunc("POST /stand-in/installations", decoded(func(in struct {
		ID          int64    `json:"id"`
		Account     string   `json:"account"`
		AccountType string   `json:"account_type"`
		Selection   string   `json:"repository_selection"`
		Suspended   bool     `json:"suspended"`
		Users       []string `json:"users"`
		Repos       []int64  `json:"repos"`
	}) {
		if in.AccountType == "" {
			in.AccountType = "Organization"
		}
		if in.Selection == "" {
			in.Selection = "selected"
		}
		fake.AddInstallation(githubtest.Installation{ID: in.ID, Account: in.Account, AccountType: in.AccountType, Selection: in.Selection,
			Suspended: in.Suspended, Users: in.Users, Repos: in.Repos})
	}))
	mux.HandleFunc("POST /stand-in/repos", decoded(func(in struct {
		InstallationID int64  `json:"installation_id"`
		ID             int64  `json:"id"`
		FullName       string `json:"full_name"`
		Visibility     string `json:"visibility"`
		DefaultBranch  string `json:"default_branch"`
	}) {
		if in.Visibility == "" {
			in.Visibility = "private"
		}
		fake.AddRepo(in.InstallationID, githubtest.Repo{ID: in.ID, FullName: in.FullName, Visibility: in.Visibility, DefaultBranch: in.DefaultBranch})
	}))
	mux.HandleFunc("POST /stand-in/visibility", decoded(func(in struct {
		RepoID     int64  `json:"repo_id"`
		Visibility string `json:"visibility"`
	}) {
		fake.SetVisibility(in.RepoID, in.Visibility)
	}))
	mux.HandleFunc("POST /stand-in/issues", decoded(func(in struct {
		RepoID      int64  `json:"repo_id"`
		Number      int    `json:"number"`
		State       string `json:"state"`
		StateReason string `json:"state_reason"`
		ClosedBy    []int  `json:"closed_by"`
	}) {
		if in.State == "" {
			in.State = "open"
		}
		fake.SetIssue(in.RepoID, githubtest.Issue{Number: in.Number, State: in.State, StateReason: in.StateReason, ClosedBy: in.ClosedBy})
	}))
	mux.HandleFunc("POST /stand-in/pulls", decoded(func(in struct {
		RepoID   int64  `json:"repo_id"`
		Number   int    `json:"number"`
		State    string `json:"state"`
		Merged   bool   `json:"merged"`
		MergeSHA string `json:"merge_sha"`
		MergedBy string `json:"merged_by"`
		HeadRef  string `json:"head_ref"`
		Closes   []int  `json:"closes"`
	}) {
		if in.State == "" {
			in.State = "open"
		}
		fake.SetPull(in.RepoID, githubtest.Pull{Number: in.Number, State: in.State, Merged: in.Merged, MergeSHA: in.MergeSHA, MergedBy: in.MergedBy,
			HeadRef: in.HeadRef, Closes: in.Closes})
	}))
	mux.HandleFunc("POST /stand-in/down", decoded(func(in struct {
		Down bool `json:"down"`
	}) {
		fake.SetDown(in.Down)
	}))
	mux.HandleFunc("POST /stand-in/copilot", decoded(func(in struct {
		RepoID int64 `json:"repo_id"`
		On     bool  `json:"on"`
	}) {
		fake.SetCopilot(in.RepoID, in.On)
	}))
	mux.HandleFunc("POST /stand-in/access", decoded(func(in struct {
		RepoID int64  `json:"repo_id"`
		Login  string `json:"login"`
		Access string `json:"access"`
	}) {
		fake.SetAccess(in.RepoID, in.Login, in.Access)
	}))
	// Makes the next call of one route answer a status: {"route": "POST /repos/o/r/issues/1/assignees", "status": 403}.
	mux.HandleFunc("POST /stand-in/fail", decoded(func(in struct {
		Route  string `json:"route"`
		Status int    `json:"status"`
	}) {
		fake.Fail(in.Route, in.Status)
	}))
	// The issues created on a repository (?repo_id=), with their comments and assignees.
	mux.HandleFunc("GET /stand-in/issues", func(response http.ResponseWriter, request *http.Request) {
		repoID, err := strconv.ParseInt(request.URL.Query().Get("repo_id"), 10, 64)
		if err != nil {
			http.Error(response, "want ?repo_id=", http.StatusBadRequest)
			return
		}
		type comment struct {
			Body   string `json:"body"`
			Author string `json:"author"`
		}
		type issue struct {
			Number    int       `json:"number"`
			Title     string    `json:"title"`
			Body      string    `json:"body"`
			Author    string    `json:"author"`
			State     string    `json:"state"`
			Comments  []comment `json:"comments"`
			Assignees []string  `json:"assignees"`
		}
		issues := []issue{}
		for _, created := range fake.CreatedIssues(repoID) {
			comments := []comment{}
			for _, c := range created.Comments {
				comments = append(comments, comment{Body: c.Body, Author: c.Author})
			}
			issues = append(issues, issue{Number: created.Number, Title: created.Title, Body: created.Body, Author: created.Author,
				State: created.State, Comments: comments, Assignees: append([]string{}, created.Assignees...)})
		}
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(issues)
	})
	deliver := func(response http.ResponseWriter, request *http.Request, event string, payload any) {
		body, header := fake.Webhook(event, payload)
		delivery, err := http.NewRequestWithContext(request.Context(), http.MethodPost, *webhookURL, bytes.NewReader(body))
		if err != nil {
			http.Error(response, err.Error(), http.StatusInternalServerError)
			return
		}
		delivery.Header = header
		answer, err := http.DefaultClient.Do(delivery)
		if err != nil {
			http.Error(response, err.Error(), http.StatusBadGateway)
			return
		}
		defer answer.Body.Close()
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(answer.StatusCode)
		_, _ = io.Copy(response, answer.Body)
	}
	// Delivers one signed event to the server, as GitHub would: {"event": "pull_request", "payload": {...}}.
	mux.HandleFunc("POST /stand-in/webhook", func(response http.ResponseWriter, request *http.Request) {
		var in struct {
			Event   string          `json:"event"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.NewDecoder(request.Body).Decode(&in); err != nil || in.Event == "" {
			http.Error(response, "want {event, payload}", http.StatusBadRequest)
			return
		}
		deliver(response, request, in.Event, in.Payload)
	})
	// Delivers pull_request for a pull request set with /stand-in/pulls, its repository and pull request objects
	// as GitHub writes them: {"action": "opened", "installation_id": 1, "repo_id": 2, "number": 7}.
	mux.HandleFunc("POST /stand-in/webhook/pull_request", func(response http.ResponseWriter, request *http.Request) {
		var in struct {
			Action         string `json:"action"`
			InstallationID int64  `json:"installation_id"`
			RepoID         int64  `json:"repo_id"`
			Number         int    `json:"number"`
		}
		if err := json.NewDecoder(request.Body).Decode(&in); err != nil || in.Action == "" {
			http.Error(response, "want {action, installation_id, repo_id, number}", http.StatusBadRequest)
			return
		}
		pull, repo := fake.PullPayload(in.RepoID, in.Number), fake.RepoPayload(in.RepoID)
		if pull == nil || repo == nil {
			http.Error(response, "no such pull request", http.StatusNotFound)
			return
		}
		deliver(response, request, "pull_request", map[string]any{"action": in.Action, "number": in.Number, "installation": installationPayload(in.InstallationID),
			"repository": repo, "pull_request": pull, "sender": map[string]any{"login": "octocat", "type": "User"}})
	})
	// Delivers issues for an issue of a repository: {"action": "assigned", "installation_id": 1, "repo_id": 2, "number": 3,
	// "assignee": "copilot-swe-agent[bot]"}.
	mux.HandleFunc("POST /stand-in/webhook/issues", func(response http.ResponseWriter, request *http.Request) {
		var in struct {
			Action         string `json:"action"`
			InstallationID int64  `json:"installation_id"`
			RepoID         int64  `json:"repo_id"`
			Number         int    `json:"number"`
			Assignee       string `json:"assignee"`
		}
		if err := json.NewDecoder(request.Body).Decode(&in); err != nil || in.Action == "" {
			http.Error(response, "want {action, installation_id, repo_id, number}", http.StatusBadRequest)
			return
		}
		repo := fake.RepoPayload(in.RepoID)
		if repo == nil {
			http.Error(response, "no such repository", http.StatusNotFound)
			return
		}
		payload := map[string]any{"action": in.Action, "installation": installationPayload(in.InstallationID), "repository": repo,
			"issue": map[string]any{"number": in.Number}, "sender": map[string]any{"login": "octocat", "type": "User"}}
		if in.Assignee != "" {
			payload["assignee"] = map[string]any{"login": in.Assignee, "type": "Bot"}
		}
		deliver(response, request, "issues", payload)
	})
	mux.HandleFunc("GET /stand-in/state", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]any{"user_tokens": len(fake.UserTokens()), "revoked_tokens": fake.RevokedTokens()})
	})
	server := &http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("GitHub stand-in on http://%s (App %s, slug %s)", *listen, *appID, *slug)
	log.Fatal(server.ListenAndServe())
}

// installationPayload is the installation object of a webhook.
func installationPayload(id int64) map[string]any {
	return map[string]any{"id": id, "repository_selection": "selected"}
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

// loadKey reads the App key from path, or creates one there (mode 0600) when the file does not exist;
// with no path the key lives only in memory.
func loadKey(path string) (*rsa.PrivateKey, error) {
	if path != "" {
		data, err := os.ReadFile(path)
		if err == nil {
			block, _ := pem.Decode(data)
			if block == nil {
				return nil, fmt.Errorf("%s: no PEM block", path)
			}
			return x509.ParsePKCS1PrivateKey(block.Bytes)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	if path != "" {
		data := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return nil, err
		}
	}
	return key, nil
}
