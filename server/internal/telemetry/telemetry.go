// Package telemetry sends one usage report a day from a running server so the Colliedesk team can
// count the installs, their versions and how much they are used. It carries counts and the public
// host name of the install, never a person's name, an email address or anything a customer wrote.
// docs/user/telemetry.md lists every field.
package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
)

const (
	startDelay = time.Minute
	// checkEvery is how often a running server asks whether a day has passed since the install last
	// reported; the claim in the database makes it one report a day however often servers start.
	checkEvery  = time.Hour
	sendTimeout = 30 * time.Second
)

// Reporter collects the counts and sends them.
type Reporter struct {
	db     *database.DB
	config Config
	client *http.Client
	logger *slog.Logger
}

// New returns a reporter. A nil client gets one with a 10 second timeout.
func New(db *database.DB, config Config, client *http.Client) *Reporter {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Reporter{db: db, config: config, client: client, logger: slog.Default()}
}

// Run says whether the report is on, then from a minute after start checks every hour and sends
// the install's report when a day has passed, until ctx ends. A failed report is logged and never
// stops the server.
func (r *Reporter) Run(ctx context.Context, logger *slog.Logger) {
	r.logger = logger
	if !r.config.Enabled() {
		reason := r.config.OffReason
		if reason == "" {
			reason = "this build has no telemetry key"
		}
		logger.Info("usage report is off", "reason", reason)
		return
	}
	logger.Info("usage report is on: once a day this install sends counts and its public host name (no names, emails or ticket content) to PostHog; set DISABLE_TELEMETRY=true to turn it off",
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
		wait = checkEvery
	}
}

// SendOnce sends the install's report unless it is off or the install already reported in the
// last 23 hours, and logs what it sent.
func (r *Reporter) SendOnce(ctx context.Context) error {
	if !r.config.Enabled() {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	u, claimed, err := claimUsage(ctx, r.db)
	if err != nil {
		return fmt.Errorf("collect usage: %w", err)
	}
	if !claimed {
		return nil
	}
	properties := map[string]any{
		"version":           r.config.Version,
		"realm":             r.config.Realm,
		"domain":            r.config.Domain,
		"os":                runtime.GOOS,
		"arch":              runtime.GOARCH,
		"workspaces":        u.workspaces,
		"agents_active_30d": u.activeAgents,
		"replies_30d":       u.replies,
		"email_channel":     u.emailChannel,
		"web_form":          u.webForm,
		"freshdesk_import":  u.freshdeskImport,
		// An install is not a person; the PostHog project also counts the people who use the
		// website and the cloud app, so the report must not create a person profile.
		"$process_person_profile": false,
	}
	if err := capture(ctx, r.client, r.config, "instance heartbeat", u.installID, properties); err != nil {
		return err
	}
	r.logger.Info("usage report sent", "install_id", u.installID, "version", r.config.Version, "realm", r.config.Realm,
		"domain", r.config.Domain, "workspaces", u.workspaces, "agents_active_30d", u.activeAgents, "replies_30d", u.replies,
		"email_channel", u.emailChannel, "web_form", u.webForm, "freshdesk_import", u.freshdeskImport)
	return nil
}

// capture posts one event to PostHog's capture endpoint and fails unless PostHog accepts it.
func capture(ctx context.Context, client *http.Client, config Config, event, distinctID string, properties map[string]any) error {
	body, err := json.Marshal(map[string]any{
		"api_key":     config.ProjectKey,
		"event":       event,
		"distinct_id": distinctID,
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
		"properties":  properties,
	})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, config.Endpoint+"/i/v0/e/", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
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
