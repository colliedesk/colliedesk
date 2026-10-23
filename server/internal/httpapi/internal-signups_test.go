package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

func TestParseInternalSignupsRefusesAnEntryWithoutAt(t *testing.T) {
	for _, value := range []string{"team.example.com", "@", "tester@"} {
		if _, err := httpapi.ParseInternalSignups(value, ""); err == nil {
			t.Errorf("ParseInternalSignups(%q) accepted it, want an error naming the entry", value)
		}
	}
	if _, err := httpapi.ParseInternalSignups(" , ", ""); err != nil {
		t.Fatalf("empty list: %v, want no error", err)
	}
}

func TestInternalSignupIsMarkedOnTrialStarted(t *testing.T) {
	internal, err := httpapi.ParseInternalSignups("@team.example.com, Tester@Mail.Example.net", "")
	if err != nil {
		t.Fatal(err)
	}
	funnel := &recordingFunnel{}
	f := newSignupFixture(t, func(config *httpapi.Config) {
		config.Funnel = funnel
		config.InternalSignups = internal
	})

	signups := []struct {
		email, name, peer string
		internal          bool
	}{
		{"Owner@TEAM.example.com", "Team Test", "203.0.113.11:40000", true},
		{"tester@mail.example.net", "Tester Test", "203.0.113.12:40000", true},
		{"owner@customer.example.com", "Customer", "203.0.113.13:40000", false},
		// A domain entry is that domain exactly, never its subdomains or another mailbox there.
		{"owner@sub.team.example.com", "Sub Team", "203.0.113.14:40000", false},
		{"someone@mail.example.net", "Mail Someone", "203.0.113.15:40000", false},
	}
	for _, s := range signups {
		wantResponse(t, f.signUp(t, s.email, s.name, s.peer), http.StatusCreated, "")
	}

	events := funnel.named("trial_started")
	if len(events) != len(signups) {
		t.Fatalf("events=%+v, want one trial_started per sign-up", events)
	}
	for i, s := range signups {
		value, marked := events[i].Properties["internal"]
		if s.internal && value != true {
			t.Errorf("%s: properties=%v, want internal=true", s.email, events[i].Properties)
		}
		if !s.internal && marked {
			t.Errorf("%s: properties=%v, want no internal property", s.email, events[i].Properties)
		}
	}
}

func TestParseInternalSignupsRefusesABadNetwork(t *testing.T) {
	for _, value := range []string{"team-office", "198.51.100.0/33", "198.51.100"} {
		if _, err := httpapi.ParseInternalSignups("", value); err == nil {
			t.Errorf("networks %q accepted, want an error naming the entry", value)
		}
	}
}

// A mailbox has more than one spelling: Gmail ignores dots and anything after "+" reaches the same
// inbox at most providers. Each spelling of a listed mailbox is the same teammate.
func TestInternalSignupMatchesEverySpellingOfAListedMailbox(t *testing.T) {
	internal, err := httpapi.ParseInternalSignups("first.last@gmail.com, owner@corp.example", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, email := range []string{"firstlast@gmail.com", "F.i.r.s.t.L.a.s.t@Gmail.com", "firstlast+audit@gmail.com",
		"first.last@googlemail.com", "owner+qa@corp.example"} {
		if !internal.Match(email, "203.0.113.20") {
			t.Errorf("Match(%q) = false, want true: it is a listed mailbox", email)
		}
	}
	for _, email := range []string{"firstlast2@gmail.com", "o.wner@corp.example", "owner@corp.example.net"} {
		if internal.Match(email, "203.0.113.20") {
			t.Errorf("Match(%q) = true, want false: another mailbox", email)
		}
	}
}

// The team's own machines (agent hosts) sign up with whatever address a test needs; a sign-up from
// one of their networks is internal whatever the address.
func TestInternalSignupFromATeamNetworkIsMarkedOnTrialStarted(t *testing.T) {
	internal, err := httpapi.ParseInternalSignups("@team.example.com", "198.51.100.0/24, 2001:db8:1:2::/64")
	if err != nil {
		t.Fatal(err)
	}
	funnel := &recordingFunnel{}
	f := newSignupFixture(t, func(config *httpapi.Config) {
		config.Funnel = funnel
		config.InternalSignups = internal
	})
	signups := []struct {
		email, name, peer string
		internal          bool
	}{
		{"agent@customer.example.com", "Agent Test", "198.51.100.7:40000", true},
		{"agent6@customer.example.com", "Agent Six", "[2001:db8:1:2::9]:40000", true},
		{"owner@customer.example.com", "Customer", "203.0.113.30:40000", false},
		{"owner6@customer.example.com", "Customer Six", "[2001:db8:1:3::9]:40000", false},
	}
	for _, s := range signups {
		wantResponse(t, f.signUp(t, s.email, s.name, s.peer), http.StatusCreated, "")
	}
	events := funnel.named("trial_started")
	if len(events) != len(signups) {
		t.Fatalf("events=%+v, want one trial_started per sign-up", events)
	}
	for i, s := range signups {
		value, marked := events[i].Properties["internal"]
		if s.internal && value != true {
			t.Errorf("%s from %s: properties=%v, want internal=true", s.email, s.peer, events[i].Properties)
		}
		if !s.internal && marked {
			t.Errorf("%s from %s: properties=%v, want no internal property", s.email, s.peer, events[i].Properties)
		}
	}
}
