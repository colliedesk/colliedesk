package autofix

import (
	"errors"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var (
	github = Actor{Kind: ActorGitHub, Name: "octocat"}
	system = Actor{Kind: ActorSystem}
	person = Actor{Kind: ActorPerson, ID: "a1", Name: "Ada"}
)

func TestSignalsOnlyMoveForwardAndNeverUndoAPersonsCancel(t *testing.T) {
	pr := "https://github.com/acme/shop/pull/7"
	for _, c := range []struct {
		name    string
		current Current
		obs     Observation
		want    Result
	}{
		{"the issue exists", Current{State: Sending}, Observation{State: Sent, IssueNumber: 3}, Move},
		{"the agent runs", Current{State: Sent}, Observation{State: Working}, Move},
		{"a pull request without a running sign", Current{State: Sent}, Observation{State: PROpen, PRURL: pr}, Move},
		{"a pull request", Current{State: Working}, Observation{State: PROpen, PRURL: pr}, Move},
		{"merged", Current{State: PROpen, HasPR: true}, Observation{State: Merged}, Move},
		{"issue closed by a merged pull request the webhook missed", Current{State: Working}, Observation{State: Merged, PRURL: pr}, Move},
		{"the same state again", Current{State: Working}, Observation{State: Working}, Ignore},
		{"an old signal after a newer one", Current{State: PROpen, HasPR: true}, Observation{State: Working}, Ignore},
		{"only a person releases", Current{State: Merged}, Observation{State: Deployed}, Note},
		{"merged stays merged", Current{State: Merged}, Observation{State: Failed, Reason: "pr_closed"}, Note},
		{"a pull request closed without merging", Current{State: PROpen, HasPR: true}, Observation{State: Failed, Reason: "pr_closed"}, Move},
		{"the pull request decides, not another failure", Current{State: PROpen, HasPR: true}, Observation{State: Failed, Reason: "agent_error"}, Note},
		{"the pull request reopened", Current{State: Failed, Reason: "pr_closed", HasPR: true}, Observation{State: PROpen}, Move},
		{"another failure does not reopen", Current{State: Failed, Reason: "agent_error"}, Observation{State: PROpen, PRURL: pr}, Note},
		{"issue closed by hand", Current{State: Sent}, Observation{State: Cancelled, Reason: "issue_closed"}, Move},
		{"issue closed after the person cancelled in Colliedesk", Current{State: Cancelled, Reason: "cancelled_by_person"}, Observation{State: Cancelled, Reason: "issue_closed"}, Note},
		{"issue closed while a pull request is open", Current{State: PROpen, HasPR: true}, Observation{State: Cancelled, Reason: "issue_closed"}, Note},
		{"issue reopened", Current{State: Cancelled, Reason: "issue_closed"}, Observation{State: Sent}, Move},
		{"a person's cancel never comes back", Current{State: Cancelled, Reason: "cancelled_by_person"}, Observation{State: Sent}, Note},
		{"authorization abandoned", Current{State: Sending}, Observation{State: Cancelled, Reason: "authorization_abandoned"}, Move},
		{"authorization abandoned once sent is late", Current{State: Sent}, Observation{State: Cancelled, Reason: "authorization_abandoned"}, Note},
		{"the process died while sending", Current{State: Sending}, Observation{State: Failed, Reason: "interrupted"}, Move},
		{"repository disconnected with a pull request open", Current{State: PROpen, HasPR: true}, Observation{State: Cancelled, Reason: "repo_disconnected"}, Move},
	} {
		got, err := Decide(c.current, c.obs, github)
		if err != nil || got != c.want {
			t.Errorf("%s: Decide = %v, %v; want %v", c.name, got, err, c.want)
		}
	}
}

func TestDecideRefusesWhatNoSourceMaySay(t *testing.T) {
	for _, c := range []struct {
		name  string
		obs   Observation
		actor Actor
		err   error
	}{
		{"a person goes through Act", Observation{State: Cancelled, Reason: "cancelled_by_person"}, person, ErrMove},
		{"failed without a reason", Observation{State: Failed}, system, ErrReason},
		{"failed with a cancel reason", Observation{State: Failed, Reason: "issue_closed"}, system, ErrReason},
		{"a reason on a moving state", Observation{State: Working, Reason: "timed_out"}, system, ErrReason},
		{"pr_open without the pull request", Observation{State: PROpen}, github, ErrMove},
		{"an unknown state", Observation{State: "done"}, github, ErrMove},
	} {
		if _, err := Decide(Current{State: Sent}, c.obs, c.actor); !errors.Is(err, c.err) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.err)
		}
	}
}

func TestEveryWayInHasAWayOut(t *testing.T) {
	for _, c := range []struct {
		action  Action
		current Current
		want    State
		reason  string
	}{
		{ActionCancel, Current{State: Sending}, Cancelled, "cancelled_by_person"},
		{ActionCancel, Current{State: Sent}, Cancelled, "cancelled_by_person"},
		{ActionCancel, Current{State: Working}, Cancelled, "cancelled_by_person"},
		{ActionCancel, Current{State: PROpen, HasPR: true}, Cancelled, "cancelled_by_person"},
		{ActionLinkPR, Current{State: Sent}, PROpen, ""},
		{ActionLinkPR, Current{State: Working}, PROpen, ""},
		{ActionLinkPR, Current{State: Failed, Reason: "pr_closed", HasPR: true}, PROpen, ""},
		{ActionUnlinkPR, Current{State: PROpen, HasPR: true, PRLinkedBy: "person"}, Sent, ""},
		{ActionDeployed, Current{State: Merged}, Deployed, ""},
		{ActionUndeployed, Current{State: Deployed}, Merged, ""},
	} {
		got, reason, err := Act(c.action, c.current)
		if err != nil || got != c.want || reason != c.reason {
			t.Errorf("%s from %s = %s %q %v, want %s %q", c.action, c.current.State, got, reason, err, c.want, c.reason)
		}
	}
	for _, c := range []struct {
		action  Action
		current Current
	}{
		{ActionCancel, Current{State: Merged}},
		{ActionCancel, Current{State: Cancelled, Reason: "cancelled_by_person"}},
		{ActionLinkPR, Current{State: Failed, Reason: "agent_error"}},
		{ActionLinkPR, Current{State: Sending}},
		{ActionUnlinkPR, Current{State: PROpen, HasPR: true, PRLinkedBy: "auto"}},
		{ActionDeployed, Current{State: PROpen, HasPR: true}},
		{ActionUndeployed, Current{State: Merged}},
		{"stop", Current{State: Working}},
	} {
		if _, _, err := Act(c.action, c.current); !errors.Is(err, ErrMove) {
			t.Errorf("%s from %s: err = %v, want ErrMove", c.action, c.current.State, err)
		}
	}
	for _, state := range States {
		if CanRetry(state) != (state == Failed || state == Cancelled) {
			t.Errorf("CanRetry(%s) = %v", state, CanRetry(state))
		}
	}
}

// The migration and the contract hold the same states, connectors and reasons as this package, so a
// branch that adds one cannot forget the others.
func TestTheDatabaseAndTheContractNameTheSameStatesAndReasons(t *testing.T) {
	migration := readFile(t, "../../migrations/20261027200000-autofix.up.sql")
	contract := readFile(t, "../../../contracts/openapi.yaml")
	quoted := func(text string) []string {
		var names []string
		for _, match := range regexp.MustCompile(`'([a-z_]+)'`).FindAllStringSubmatch(text, -1) {
			names = append(names, match[1])
		}
		return names
	}
	between := func(text, start, end string) string {
		i := strings.Index(text, start)
		if i < 0 {
			t.Fatalf("%q not found", start)
		}
		rest := text[i+len(start):]
		return rest[:strings.Index(rest, end)]
	}
	var states []string
	for _, s := range States {
		states = append(states, string(s))
	}
	var connectors []string
	for _, c := range Connectors {
		connectors = append(connectors, string(c))
	}
	equal := func(what string, got, want []string) {
		if !slices.Equal(got, want) {
			t.Errorf("%s = %v, want %v", what, got, want)
		}
	}
	equal("migration fix_attempts.state", quoted(between(migration, "state text NOT NULL CHECK (state IN (", "))")), states)
	equal("migration fix_attempts.connector", quoted(between(migration, "connector text NOT NULL CHECK (connector IN (", "))")), connectors)
	equal("migration failed reasons", quoted(between(migration, "WHEN 'failed' THEN outcome_reason IN (", ")")), FailedReasons)
	equal("migration cancelled reasons", quoted(between(migration, "WHEN 'cancelled' THEN outcome_reason IN (", ")")), CancelledReasons)
	enum := func(schema string) []string {
		// The schema's own enum comes first after its name.
		block := between(contract[strings.Index(contract, "\n    "+schema+":\n"):], "enum: [", "]")
		var names []string
		for _, name := range strings.Split(block, ",") {
			names = append(names, strings.TrimSpace(name))
		}
		return names
	}
	equal("contract FixState", enum("FixState"), states)
	equal("contract FixConnector", enum("FixConnector"), connectors)
	equal("contract FixOutcomeReason", enum("FixOutcomeReason"), Reasons())
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
