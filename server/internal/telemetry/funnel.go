package telemetry

import (
	"context"
	"log/slog"
	"maps"
	"net/http"
	"time"
)

// Funnel sends the sign-up and payment events of Colliedesk Cloud, so the team can see how many
// visitors of colliedesk.com start a trial and how many trials pay. It runs on the hosted cloud
// only: a self-hosted server has no trials or billing and sends nothing. Each event is keyed by the
// random workspace id and carries the plan, never a person's name, an email address or a workspace
// name. docs/user/telemetry.md lists every field.
type Funnel struct {
	config Config
	client *http.Client
}

// NewFunnel returns a funnel. A nil client gets one with a 10 second timeout.
func NewFunnel(config Config, client *http.Client) *Funnel {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Funnel{config: config, client: client}
}

func (f *Funnel) enabled() bool { return f.config.Enabled() && f.config.Realm == "cloud" }

// Send posts one event and waits for PostHog's answer. It does nothing off the cloud.
func (f *Funnel) Send(ctx context.Context, event, workspaceID string, properties map[string]any) error {
	if !f.enabled() {
		return nil
	}
	merged := map[string]any{"realm": f.config.Realm, "version": f.config.Version, "$process_person_profile": false}
	maps.Copy(merged, properties)
	return capture(ctx, f.client, f.config, event, workspaceID, merged)
}

// Record sends the event in the background so the request that caused it never waits on PostHog.
// A failure is logged and changes nothing else.
func (f *Funnel) Record(event, workspaceID string, properties map[string]any) {
	if !f.enabled() {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
		defer cancel()
		if err := f.Send(ctx, event, workspaceID, properties); err != nil {
			slog.Warn("funnel event not sent", "event", event, "workspace", workspaceID, "error", err)
		}
	}()
}
