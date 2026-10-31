package channels_test

import (
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
)

// Intake decides on what a scope collected, so a type declares it only on a scope of its own that
// collects messages; anything else is refused when the server starts.
func TestAnIntakeScopeMustBeAScopeThatCollects(t *testing.T) {
	for name, c := range map[string]struct {
		intake []string
		gather bool
		ok     bool
	}{
		"collects":      {intake: []string{"group"}, gather: true, ok: true},
		"no gather":     {intake: []string{"group"}},
		"not its scope": {intake: []string{"room"}, gather: true},
		"none":          {ok: true},
	} {
		loop := channeltest.NewLoopback("loopchat", channels.Async)
		loop.IntakeScopes = c.intake
		if c.gather {
			loop.Defaults.Scopes = map[string]channels.Scope{"group": {Gather: &channels.Gather{QuietSeconds: 60, MaxSeconds: 600}}, "room": {}}
		}
		_, err := channels.NewRegistry(loop)
		if c.ok != (err == nil) || (err != nil && !strings.Contains(err.Error(), "intake scope")) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// A scope's key is stored with the conversations it starts and the AI levels set on it, as lower-case
// letters and underscores; a type with another key is refused when the server starts.
func TestAScopeKeyIsLowerCaseLettersAndUnderscores(t *testing.T) {
	for _, scope := range []string{"Group", "group-1", "", strings.Repeat("a", 41)} {
		loop := channeltest.NewLoopback("loopchat", channels.Async)
		loop.ExtraScopes = []string{scope}
		if _, err := channels.NewRegistry(loop); err == nil || !strings.Contains(err.Error(), "scope") {
			t.Errorf("scope %q: %v", scope, err)
		}
	}
}
