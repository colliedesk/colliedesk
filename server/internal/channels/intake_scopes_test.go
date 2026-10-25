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
