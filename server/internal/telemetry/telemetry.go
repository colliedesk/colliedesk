// Package telemetry sends one usage report a day from a running server so HC Labs can count the
// installs, their versions and how much they are used. It carries counts only, never a name, an
// email address or anything a customer wrote. docs/user/telemetry.md lists every field.
package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"runtime"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
)

const (
	// startDelay keeps a container that crash-loops from reporting on every start.
	startDelay = time.Minute
	interval   = 24 * time.Hour
)

// Reporter collects the counts and sends them.
type Reporter struct {
	db     *database.DB
	config Config
	client *http.Client
}

// New returns a reporter. A nil client gets one with a 10 second timeout.
func New(db *database.DB, config Config, client *http.Client) *Reporter {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Reporter{db: db, config: config, client: client}
}

// Run says whether the report is on, then sends one a minute after start and one a day until ctx
// ends. A failed report is logged and never stops the server.
func (r *Reporter) Run(ctx context.Context, logger *slog.Logger) {
	if !r.config.Enabled() {
		reason := r.config.OffReason
		if reason == "" {
			reason = "this build has no telemetry key"
		}
		logger.Info("usage report is off", "reason", reason)
		return
	}
	logger.Info("usage report is on: once a day this server sends counts (no names, emails or ticket content) to PostHog; set DISABLE_TELEMETRY=true to turn it off",
		"fields", "https://github.com/colliedesk/colliedesk/blob/main/docs/user/telemetry.md")
	wait := startDelay
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if err := r.SendOnce(ctx); err != nil && ctx.Err() == nil {
			logger.Warn("usage report not sent", "error", err)
		}
		wait = interval
	}
}

// SendOnce collects the counts and sends one report. It does nothing when the report is off.
func (r *Reporter) SendOnce(ctx context.Context) error {
	if !r.config.Enabled() {
		return nil
	}
	u, err := collectUsage(ctx, r.db)
	if err != nil {
		return fmt.Errorf("collect usage: %w", err)
	}
	install := map[string]any{"version": r.config.Version, "realm": r.config.Realm, "domain": r.config.Domain}
	properties := map[string]any{
		"os":                runtime.GOOS,
		"arch":              runtime.GOARCH,
		"workspaces":        u.workspaces,
		"agents_active_30d": u.activeAgents,
		"replies_30d":       u.replies,
		"email_channel":     u.emailChannel,
		"web_form":          u.webForm,
		"freshdesk_import":  u.freshdeskImport,
		"$set":              install,
	}
	maps.Copy(properties, install)
	body, err := json.Marshal(map[string]any{
		"api_key":     r.config.ProjectKey,
		"event":       "instance heartbeat",
		"distinct_id": u.installID,
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
		"properties":  properties,
	})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, r.config.Endpoint+"/i/v0/e/", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := r.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	if response.StatusCode/100 != 2 {
		return fmt.Errorf("PostHog answered %d", response.StatusCode)
	}
	return nil
}
