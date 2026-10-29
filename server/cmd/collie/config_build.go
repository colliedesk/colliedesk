package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"strconv"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/cloudauth"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/github"
	"github.com/colliedesk/colliedesk/server/internal/google"
	"github.com/colliedesk/colliedesk/server/internal/helpcenter"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/microsoft"
	"github.com/colliedesk/colliedesk/server/internal/polar"
	"github.com/colliedesk/colliedesk/server/internal/serverconfig"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
	"github.com/jackc/pgx/v5/pgxpool"
)

// settings reads the server's configuration by the old variable names (serverconfig.Snapshot.Getenv).
// Commands that need it load it first (useServerConfig); until then only the bootstrap variables read.
var settings = serverconfig.NewSnapshot(0, nil, nil, "", os.Getenv, false).Getenv

// maxRetentionDeletionsPerDay is the hard cap of retention.max_deletions_per_day.
const maxRetentionDeletionsPerDay = 20

// builtConfig is every setting parsed, as serve starts with it. buildConfig is the one place that turns a
// snapshot into it: serve, the CLI's checks and the import all call it, so they agree on what is valid.
type builtConfig struct {
	Billing                polar.Config
	Email                  email.Config
	Google                 google.Config
	Microsoft              microsoft.Config
	GitHub                 github.Config
	HelpCenterDomain       string
	AIServer               *ai.ServerProvider
	AllowPrivateBaseURLs   bool
	TrustedProxies         []netip.Prefix
	InternalSignups        httpapi.InternalSignups
	CloudAuth              cloudauth.Config
	SignupHashKey          string
	MailImageCacheBytes    int64
	KBVideoMaxBytes        int64
	Retention              retentionSettings
	ProblemReportWorkspace string
	BillingAlertEmail      string
	RequireTOTP            bool
}

// buildConfig parses every setting of getenv and the rules that span them. Errors name settings, never
// values (redactSecrets makes sure of it).
func buildConfig(getenv func(string) string, retired map[string]string) (builtConfig, error) {
	var built builtConfig
	var errs []error
	keep := func(err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}
	var err error
	built.Billing, err = polar.ConfigFromEnv(getenv)
	keep(err)
	built.Billing.Products.Retired = retired
	built.Email, err = email.ConfigFromEnv(getenv)
	keep(err)
	built.Google, err = google.ConfigFromEnv(getenv)
	keep(err)
	built.Microsoft, err = microsoft.ConfigFromEnv(getenv, built.Google.Keys)
	keep(err)
	built.GitHub = github.ConfigFromEnv(getenv)
	keep(built.GitHub.Check())
	built.HelpCenterDomain, err = helpcenter.DomainFromEnv(getenv)
	keep(err)
	built.AIServer, err = ai.ServerProviderFromEnv(getenv)
	keep(err)
	switch value := strings.TrimSpace(getenv("AI_ALLOW_PRIVATE_BASE_URLS")); value {
	case "", "0", "false":
	case "1", "true":
		built.AllowPrivateBaseURLs = true
	default:
		keep(errors.New("ai.allow_private_base_urls must be true or false"))
	}
	built.TrustedProxies, err = httpapi.ParseTrustedProxies(getenv("TRUSTED_PROXIES"))
	keep(err)
	built.InternalSignups, err = httpapi.ParseInternalSignups(getenv("INTERNAL_SIGNUP_EMAILS"), getenv("INTERNAL_SIGNUP_NETWORKS"))
	keep(err)
	built.CloudAuth = cloudAuthConfig(getenv)
	built.SignupHashKey = getenv(serverconfig.SignupHashKeyEnv)
	built.MailImageCacheBytes, err = mailImageCacheBytesFromEnv(getenv)
	keep(err)
	built.KBVideoMaxBytes, err = kbVideoMaxBytesFromEnv(getenv)
	keep(err)
	built.Retention, err = retentionSettingsFromEnv(getenv)
	keep(err)
	if value := getenv("RETENTION_MAX_DELETIONS_PER_DAY"); value != "" && err == nil {
		if count, _ := strconv.Atoi(value); count < 1 || count > maxRetentionDeletionsPerDay {
			keep(fmt.Errorf("retention.max_deletions_per_day must be between 1 and %d", maxRetentionDeletionsPerDay))
		}
	}
	built.ProblemReportWorkspace = strings.TrimSpace(getenv("PROBLEM_REPORT_WORKSPACE"))
	built.RequireTOTP = strings.TrimSpace(getenv("operator.require_totp")) == "true"
	built.BillingAlertEmail = strings.TrimSpace(getenv("BILLING_ALERT_EMAIL"))
	if built.BillingAlertEmail != "" && !strings.Contains(built.BillingAlertEmail, "@") {
		keep(errors.New("billing.alert_email must be an email address"))
	}
	if value := strings.TrimSpace(getenv("SMTP_PORT")); value != "" {
		if port, err := strconv.Atoi(value); err != nil || port < 1 || port > 65535 {
			keep(errors.New("smtp.port must be a port number between 1 and 65535"))
		}
	}
	return built, errors.Join(errs...)
}

// checkSnapshot builds a snapshot and hides the value of any secret an error would repeat.
func checkSnapshot(snap serverconfig.Snapshot) (builtConfig, error) {
	built, err := buildConfig(snap.Getenv, snap.Retired())
	return built, redactSecrets(err, snap)
}

// redactSecrets replaces an error that repeats a secret's value with one that only names the secret: the
// parsers quote what they reject (%q), and a validation error goes to the log, the audit log and the API.
func redactSecrets(err error, snap serverconfig.Snapshot) error {
	if err == nil {
		return nil
	}
	text := err.Error()
	var leaked []string
	for _, key := range serverconfig.Keys() {
		if !key.Secret {
			continue
		}
		value, ok := snap.Get(key.Name)
		if ok && len(strings.TrimSpace(value)) >= 4 && strings.Contains(text, strings.TrimSpace(value)) {
			leaked = append(leaked, key.Name)
		}
	}
	if len(leaked) == 0 {
		return err
	}
	return fmt.Errorf("%s: not valid", strings.Join(leaked, ", "))
}

// importGroupCheck validates one import group the way serve parses it, from getenv alone.
func importGroupCheck(group string, getenv func(string) string) error {
	var err error
	switch group {
	case "ai-server":
		_, err = ai.ServerProviderFromEnv(getenv)
	case "email":
		_, err = email.ConfigFromEnv(getenv)
	case "google":
		_, err = google.ConfigFromEnv(getenv)
	case "microsoft":
		_, err = microsoft.ConfigFromEnv(getenv, tokenseal.Keys{})
	case "github":
		err = github.ConfigFromEnv(getenv).Check()
	case "polar":
		_, err = polar.ConfigFromEnv(getenv)
	case "retention":
		_, err = retentionSettingsFromEnv(getenv)
	case "internal-signup":
		_, err = httpapi.ParseInternalSignups(getenv("INTERNAL_SIGNUP_EMAILS"), getenv("INTERNAL_SIGNUP_NETWORKS"))
	case "trusted-proxies":
		_, err = httpapi.ParseTrustedProxies(getenv("TRUSTED_PROXIES"))
	case "help-center":
		_, err = helpcenter.DomainFromEnv(getenv)
	case "limits":
		if _, err = mailImageCacheBytesFromEnv(getenv); err == nil {
			_, err = kbVideoMaxBytesFromEnv(getenv)
		}
	}
	return err
}

// hostedCloud is cloud sign-in with billing: the Colliedesk Cloud. A self-hosted server may use sign-in alone.
func hostedCloud(getenv func(string) string) bool {
	billing, err := polar.ConfigFromEnv(getenv)
	return err == nil && billing.Enabled() && cloudAuthConfig(getenv).Enabled()
}

// openConfigStore opens the configuration with the key ring of the environment.
func openConfigStore(db *database.DB) (*serverconfig.Store, error) {
	keys, err := tokenseal.ParseKeys(os.Getenv(google.EnvTokenKeys))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", google.EnvTokenKeys, err)
	}
	return serverconfig.NewStore(db, keys, os.Getenv), nil
}

// useServerConfig loads the configuration a command reads (settings). A database with no version yet
// reads as every default.
func useServerConfig(ctx context.Context, pool *pgxpool.Pool) (serverconfig.Loaded, error) {
	store, err := openConfigStore(database.New(pool))
	if err != nil {
		return serverconfig.Loaded{}, err
	}
	loaded, err := store.Current(ctx)
	if err != nil {
		return serverconfig.Loaded{}, fmt.Errorf("read the server's configuration (run `collie migrate` first?): %w", err)
	}
	settings = loaded.Getenv
	retiredProducts = loaded.Retired()
	return loaded, nil
}

// retiredProducts are the products no longer on sale, for the commands that map a subscription to its plan.
var retiredProducts map[string]string

// billingConfig is polar.ConfigFromEnv on the loaded configuration, with the products no longer on sale.
func billingConfig() (polar.Config, error) {
	config, err := polar.ConfigFromEnv(settings)
	config.Products.Retired = retiredProducts
	return config, err
}

// startConfig imports what the environment still holds, then loads the version to run. It never stops the
// server over its configuration: a version that does not build is refused (recorded, logged) and the last
// one that ran is used, else every default (docs/internals/operator-console.md › Start).
func startConfig(ctx context.Context, store *serverconfig.Store, logger *slog.Logger) (builtConfig, serverconfig.Loaded, error) {
	by := serverconfig.Actor{Kind: "server", Name: "serve " + version + " (env)"}
	// A trial whose time ran out while no process ran is reverted before anything is served with it (D12).
	if reverted, err := store.RevertExpiredTrial(ctx); err != nil {
		logger.Error("revert an expired trial change", "error", err)
	} else if reverted != 0 {
		logger.Warn("a trial change was not confirmed in time and was reverted", "version", reverted)
	}
	imported, err := store.ImportEnv(ctx, os.Getenv, importGroupCheck, by, serverconfig.ImportOptions{Hosted: func(snap serverconfig.Snapshot) bool { return hostedCloud(snap.Getenv) }})
	if err != nil {
		return builtConfig{}, serverconfig.Loaded{}, fmt.Errorf("import settings from the environment: %w", err)
	}
	for _, group := range imported.Imported {
		logger.Info("imported settings from the environment into the database", "group", group)
	}
	for group, reason := range imported.Invalid {
		logger.Error("settings in the environment were not imported: they do not make a valid setting; set them with `collie config set` or the operator console", "group", group, "error", reason)
	}
	if len(imported.Secrets) > 0 {
		logger.Info("imported secrets kept in the environment until the key ring existed", "keys", strings.Join(imported.Secrets, ","))
	}
	if imported.SignupKey == "too-short" {
		logger.Error("SIGNUP_HASH_KEY is shorter than 32 characters and was not imported; cloud sign-up stays off until the key is fixed and the server restarts")
	}
	loaded, err := store.Current(ctx)
	if err != nil {
		return builtConfig{}, serverconfig.Loaded{}, err
	}
	if len(loaded.Unopened) > 0 {
		logger.Error("secrets are sealed with a key MAILBOX_TOKEN_KEYS no longer has: they read as unset; set them again", "keys", strings.Join(loaded.Unopened, ","))
	}
	if !store.Keys().Configured() {
		logger.Warn("MAILBOX_TOKEN_KEYS is not set: secrets are still read from the environment until a key ring is added (docs/self-host/operator-console.md)")
	}
	for _, leftover := range serverconfig.Leftovers(loaded.Snapshot, os.Getenv) {
		if leftover.Same {
			logger.Warn("a setting is still in the environment and equals the database; remove it from .env and compose.yaml", "variable", leftover.Env, "key", leftover.Key)
		} else {
			logger.Error("a setting in the environment differs from the database and is ignored; the database wins", "variable", leftover.Env, "key", leftover.Key)
		}
	}
	for _, name := range serverconfig.Dead(os.Getenv) {
		logger.Warn("a variable no release reads is set; it can go", "variable", name)
	}
	built, buildErr := checkSnapshot(loaded.Snapshot)
	if buildErr == nil {
		settings = loaded.Getenv
		return built, loaded, store.MarkGood(ctx, loaded.Version)
	}
	logger.Error("the configuration does not make a valid setting; running the last one that did", "version", loaded.Version, "error", buildErr)
	if err := store.Reject(ctx, loaded.Version, buildErr.Error(), by); err != nil {
		return builtConfig{}, serverconfig.Loaded{}, err
	}
	if good := loaded.State.LastGood; good != 0 && good != loaded.Version {
		previous, err := store.Load(ctx, good)
		if err != nil {
			return builtConfig{}, serverconfig.Loaded{}, err
		}
		if built, err := checkSnapshot(previous.Snapshot); err == nil {
			settings = previous.Getenv
			return built, previous, nil
		}
	}
	defaults, err := store.Load(ctx, 0)
	if err != nil {
		return builtConfig{}, serverconfig.Loaded{}, err
	}
	logger.Error("no earlier configuration runs either; running with every default")
	settings = defaults.Getenv
	built, err = checkSnapshot(defaults.Snapshot)
	return built, defaults, err
}
