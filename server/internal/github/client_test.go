package github_test

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/github"
	"github.com/colliedesk/colliedesk/server/internal/github/githubtest"
)

func newApp(t *testing.T) (*github.App, *githubtest.Server) {
	t.Helper()
	fake := githubtest.New()
	server := fake.Start()
	t.Cleanup(server.Close)
	fake.CallbackURL = "https://desk.example.com/api/v1/autofix/github/callback"
	app, err := github.NewApp(fake.Config())
	if err != nil {
		t.Fatal(err)
	}
	return app, fake
}

func TestThePrivateKeyIsReadAsPEMEscapedOrBase64(t *testing.T) {
	fake := githubtest.New()
	pem := fake.PrivateKeyPEM()
	for name, value := range map[string]string{
		"pem": pem, "escaped": strings.ReplaceAll(pem, "\n", `\n`), "base64": base64.StdEncoding.EncodeToString([]byte(pem)),
	} {
		config := fake.Config()
		config.PrivateKey = value
		if err := config.Check(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	config := fake.Config()
	config.PrivateKey = "not a key"
	if err := config.Check(); err == nil || !strings.Contains(err.Error(), github.EnvPrivateKey) {
		t.Errorf("a broken key passes: %v", err)
	}
	if _, err := github.NewApp(config); err == nil {
		t.Error("NewApp takes a broken key")
	}
}

// The installation flow end to end against the stand-in: the person's token proves which installations
// are theirs, the App reads repositories with its own token, and the person's token is revoked.
func TestInstallationIsCheckedWithThePersonsTokenThenReadAsTheApp(t *testing.T) {
	app, fake := newApp(t)
	ctx := context.Background()
	fake.AddInstallation(githubtest.Installation{ID: 11, Account: "acme", AccountType: "Organization", Selection: "selected", Users: []string{"ada"}})
	fake.AddInstallation(githubtest.Installation{ID: 12, Account: "other", AccountType: "User", Selection: "selected", Users: []string{"bo"}})
	fake.AddRepo(11, githubtest.Repo{ID: 501, FullName: "acme/shop-web", Visibility: "private"})
	fake.AddRepo(11, githubtest.Repo{ID: 502, FullName: "acme/docs", Visibility: "public"})
	fake.AddRepo(12, githubtest.Repo{ID: 601, FullName: "other/secret", Visibility: "private"})

	target, err := fake.Install(app.InstallURL("state-1"), "ada", 11)
	if err != nil {
		t.Fatal(err)
	}
	callback, _ := url.Parse(target)
	if callback.Query().Get("state") != "state-1" || callback.Query().Get("installation_id") != "11" {
		t.Fatalf("callback = %s", target)
	}
	token, err := app.Exchange(ctx, callback.Query().Get("code"), "", fake.CallbackURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Exchange(ctx, callback.Query().Get("code"), "", fake.CallbackURL); !errors.Is(err, github.ErrCodeRefused) {
		t.Fatalf("a used code: %v", err)
	}
	installations, err := app.UserInstallations(ctx, token)
	if err != nil || len(installations) != 1 || installations[0].ID != 11 || installations[0].Account.Login != "acme" || installations[0].RepositorySelection != "selected" {
		t.Fatalf("ada's installations = %+v, %v", installations, err)
	}
	if err := app.Revoke(ctx, token); err != nil || !fake.Revoked(token) {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := app.UserInstallations(ctx, token); err == nil {
		t.Fatal("a revoked token still reads")
	}
	if err := app.Revoke(ctx, token); err != nil {
		t.Fatalf("revoking twice: %v", err)
	}

	repos, err := app.Repositories(ctx, 11)
	if err != nil || len(repos) != 2 {
		t.Fatalf("repositories = %+v, %v", repos, err)
	}
	repo, err := app.Repository(ctx, 11, 502)
	if err != nil || repo.Kind() != "public" || repo.FullName != "acme/docs" {
		t.Fatalf("repo 502 = %+v, %v", repo, err)
	}
	// Another account's repository is not the installation's to read.
	if _, err := app.Repository(ctx, 11, 601); !errors.Is(err, github.ErrNotFound) {
		t.Fatalf("another installation's repository: %v", err)
	}
	if calls := fake.Calls("POST /app/installations/11/access_tokens"); calls != 1 {
		t.Fatalf("installation token minted %d times, want once (cached)", calls)
	}
	fake.Update(11, func(i *githubtest.Installation) { i.Suspended = true })
	if installation, err := app.Installation(ctx, 11); err != nil || installation.SuspendedAt == nil {
		t.Fatalf("suspended installation = %+v, %v", installation, err)
	}
	if err := app.Uninstall(ctx, 11); err != nil || !fake.Uninstalled(11) {
		t.Fatalf("uninstall: %v", err)
	}
	if err := app.Uninstall(ctx, 11); err != nil {
		t.Fatalf("uninstalling twice: %v", err)
	}
	if _, err := app.Installation(ctx, 11); !errors.Is(err, github.ErrNotFound) {
		t.Fatalf("uninstalled installation: %v", err)
	}
}

func TestAuthorizationUsesPKCE(t *testing.T) {
	app, fake := newApp(t)
	verifier := strings.Repeat("v", 50)
	target, err := fake.Authorize(app.AuthorizeURL(fake.CallbackURL, "s", verifier), "ada")
	if err != nil {
		t.Fatal(err)
	}
	code, _ := url.Parse(target)
	if _, err := app.Exchange(context.Background(), code.Query().Get("code"), strings.Repeat("w", 50), fake.CallbackURL); !errors.Is(err, github.ErrCodeRefused) {
		t.Fatalf("a wrong verifier: %v", err)
	}
	target, _ = fake.Authorize(app.AuthorizeURL(fake.CallbackURL, "s", verifier), "ada")
	code, _ = url.Parse(target)
	if token, err := app.Exchange(context.Background(), code.Query().Get("code"), verifier, fake.CallbackURL); err != nil || !strings.HasPrefix(token, "ghu_") {
		t.Fatalf("exchange = %q, %v", token, err)
	}
}

func TestPullRequestsAndIssuesAreLinkedBothWays(t *testing.T) {
	app, fake := newApp(t)
	ctx := context.Background()
	fake.AddInstallation(githubtest.Installation{ID: 21, Account: "acme", AccountType: "Organization", Selection: "selected"})
	fake.AddRepo(21, githubtest.Repo{ID: 701, FullName: "acme/shop-web", Visibility: "private"})
	fake.SetPull(701, githubtest.Pull{Number: 9, State: "closed", Merged: true, MergeSHA: strings.Repeat("a", 40), MergedBy: "ada", HeadRef: "claude/issue-3-fix", Closes: []int{3}})
	fake.SetIssue(701, githubtest.Issue{Number: 3, State: "closed", StateReason: "completed", ClosedBy: []int{9}})
	closes, err := app.ClosingIssues(ctx, 21, "acme/shop-web", 9)
	if err != nil || len(closes) != 1 || closes[0] != 3 {
		t.Fatalf("closing issues = %v, %v", closes, err)
	}
	pulls, err := app.IssuePullRequests(ctx, 21, "acme/shop-web", 3)
	if err != nil || len(pulls) != 1 || pulls[0].State != "MERGED" || pulls[0].MergedBy != "ada" || pulls[0].URL != "https://github.com/acme/shop-web/pull/9" {
		t.Fatalf("issue pulls = %+v, %v", pulls, err)
	}
	pull, err := app.PullRequest(ctx, 21, "acme/shop-web", 9)
	if err != nil || !pull.Merged || pull.MergedByLogin() != "ada" || pull.Head.Ref != "claude/issue-3-fix" {
		t.Fatalf("pull = %+v, %v", pull, err)
	}
	if _, err := app.ClosingIssues(ctx, 21, "acme/shop-web", 99); !errors.Is(err, github.ErrNotFound) {
		t.Fatalf("missing pull request: %v", err)
	}
	fake.SetDown(true)
	var apiErr *github.APIError
	if _, err := app.Issue(ctx, 21, "acme/shop-web", 3); !errors.As(err, &apiErr) || !apiErr.Temporary() {
		t.Fatalf("GitHub down: %v", err)
	}
}

// A browser following the install address reaches the callback through the stand-in's own page.
func TestTheStandInServesTheInstallPageToABrowser(t *testing.T) {
	app, fake := newApp(t)
	fake.AddInstallation(githubtest.Installation{ID: 31, Account: "acme", AccountType: "Organization", Selection: "selected", Users: []string{"ada"}})
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	page, err := client.Get(app.InstallURL("s-1"))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(page.Body)
	page.Body.Close()
	if !strings.Contains(string(body), "Install on acme as ada") {
		t.Fatalf("install page = %s", body)
	}
	chosen, err := client.Get(app.InstallURL("s-1") + "&as=ada&installation_id=31")
	if err != nil {
		t.Fatal(err)
	}
	chosen.Body.Close()
	location, _ := url.Parse(chosen.Header.Get("Location"))
	if chosen.StatusCode != http.StatusFound || location.Query().Get("state") != "s-1" || location.Query().Get("installation_id") != "31" || location.Query().Get("code") == "" {
		t.Fatalf("install redirect = %d %s", chosen.StatusCode, location)
	}
	authorized, err := client.Get(app.AuthorizeURL(fake.CallbackURL, "s-2", strings.Repeat("v", 50)))
	if err != nil {
		t.Fatal(err)
	}
	authorized.Body.Close()
	if back, _ := url.Parse(authorized.Header.Get("Location")); authorized.StatusCode != http.StatusFound || back.Query().Get("state") != "s-2" {
		t.Fatalf("authorize redirect = %d %s", authorized.StatusCode, back)
	}
}

// A list cut short is an error, never a shorter list: the reconcile pass would read the missing
// repositories as removed from the App.
func TestARepositoryListTooLongToReadIsAnError(t *testing.T) {
	app, fake := newApp(t)
	fake.AddInstallation(githubtest.Installation{ID: 41, Account: "big", AccountType: "Organization", Selection: "selected"})
	for id := int64(1); id <= 2001; id++ {
		fake.AddRepo(41, githubtest.Repo{ID: id, FullName: "big/r" + strconv.FormatInt(id, 10), Visibility: "private"})
	}
	if repos, err := app.Repositories(context.Background(), 41); err == nil {
		t.Fatalf("read %d of 2001 repositories without an error", len(repos))
	}
	fake.AddInstallation(githubtest.Installation{ID: 42, Account: "small", AccountType: "Organization", Selection: "selected"})
	for id := int64(5001); id <= 5150; id++ {
		fake.AddRepo(42, githubtest.Repo{ID: id, FullName: "small/r" + strconv.FormatInt(id, 10), Visibility: "private"})
	}
	if repos, err := app.Repositories(context.Background(), 42); err != nil || len(repos) != 150 {
		t.Fatalf("150 repositories over two pages = %d, %v", len(repos), err)
	}
}
