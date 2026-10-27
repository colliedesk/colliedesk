// Command github-stand-in serves githubtest, the local stand-in for the GitHub endpoints the Colliedesk
// GitHub App calls (install and authorize pages, tokens, installations, repositories, issues, pull
// requests, GraphQL), so Autofix can be tried and tested without a GitHub account or App. Point an
// end-to-end build of collie at it with COLLIEDESK_E2E_GITHUB_URL and the same App settings
// (GITHUB_APP_PRIVATE_KEY from the -key file). A test arranges GitHub's side with POST /stand-in/*,
// and /stand-in/webhook delivers a signed event to the server.
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
		body, header := fake.Webhook(in.Event, in.Payload)
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
	})
	mux.HandleFunc("GET /stand-in/state", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]any{"user_tokens": len(fake.UserTokens()), "revoked_tokens": fake.RevokedTokens()})
	})
	server := &http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("GitHub stand-in on http://%s (App %s, slug %s)", *listen, *appID, *slug)
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
