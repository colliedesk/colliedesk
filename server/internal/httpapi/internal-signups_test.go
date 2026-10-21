package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

func TestParseInternalSignupsRefusesAnEntryWithoutAt(t *testing.T) {
	for _, value := range []string{"team.example.com", "@", "tester@"} {
		if _, err := httpapi.ParseInternalSignups(value); err == nil {
			t.Errorf("ParseInternalSignups(%q) accepted it, want an error naming the entry", value)
		}
	}
	if _, err := httpapi.ParseInternalSignups(" , "); err != nil {
		t.Fatalf("empty list: %v, want no error", err)
	}
}

func TestInternalSignupIsMarkedOnTrialStarted(t *testing.T) {
	internal, err := httpapi.ParseInternalSignups("@team.example.com, Tester@Mail.Example.net")
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
