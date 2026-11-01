package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/autofix/githubsync"
	"github.com/colliedesk/colliedesk/server/internal/autofixworker"
	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/cloudauth"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/importjobs"
	"github.com/colliedesk/colliedesk/server/internal/polar"
	"github.com/colliedesk/colliedesk/server/internal/serverconfig"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// importJobNetwork changes the importer options of in-app imports; only end-to-end builds set it,
// to reach a local fake Freshdesk.
var importJobNetwork func(*freshdesk.Options)

// googleBaseURL replaces Google's addresses with a local stand-in; only end-to-end builds set it.
var googleBaseURL = ""

// githubBaseURL replaces GitHub's addresses with a local stand-in; only end-to-end builds set it.
var githubBaseURL = ""

// cursorBaseURL replaces Cursor's API with a local stand-in (cursortest); only end-to-end builds set it.
var cursorBaseURL = ""

// microsoftBaseURL replaces Microsoft's sign-in and Graph addresses with a local stand-in; only
// end-to-end builds set it.
var microsoftBaseURL = ""

// e2eChannelTypes are channel types run after the shipped ones; only end-to-end builds set them (a
// loopback chat type whose groups collect messages for intake).
var e2eChannelTypes []channels.Adapter

func serve(ctx context.Context, pool *pgxpool.Pool, origin string, args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	defaultAddress := os.Getenv("LISTEN_ADDR")
	if defaultAddress == "" {
		defaultAddress = ":8080"
	}
	address := flags.String("listen", defaultAddress, "listen address")
	if err := flags.Parse(args); err != nil {
		return err
	}
	requestPool := pool
	if requestURL := os.Getenv("DATABASE_REQUEST_URL"); requestURL != "" {
		opened, err := openPool(ctx, requestURL)
		if err != nil {
			return fmt.Errorf("open DATABASE_REQUEST_URL: %w", err)
		}
		defer opened.Close()
		requestPool = opened
	}
	db := database.NewSplit(pool, requestPool)
	if err := db.Verify(ctx); err != nil {
		return err
	}
	configStore, err := openConfigStore(db)
	if err != nil {
		return err
	}
	config, loaded, err := startConfig(ctx, configStore, slog.Default())
	if err != nil {
		return err
	}
	storage, err := attachmentStorage()
	if err != nil {
		return err
	}
	// What lives as long as the process: in-app import keys (sealed with MAILBOX_TOKEN_KEYS, or only in
	// memory without it), one Cursor client so the API and the Autofix worker keep one call budget, and
	// the API state a new configuration must not reset.
	hot := httpapi.NewHot(hotSettings(config))
	carry := httpapi.NewCarry(httpapi.Config{})
	carry.Hot = hot
	p := &process{pool: pool, requestPool: requestPool, db: db, origin: origin, storage: storage, carry: carry, configStore: configStore,
		importKeys: importjobs.NewKeys(configStore.Keys()),
		cursor:     &autofixworker.Cursor{Store: store.NewWithDB(db).WithSealKeys(configStore.Keys()), Base: cursorBaseURL}}
	p.operatorOrigin, err = operatorOrigin(origin)
	if err != nil {
		slog.Error("the operator console stays at APP_ORIGIN/operator/", "error", err)
	}
	p.operators = auth.NewOperatorService(db, configStore.Keys(), consoleBase(origin), func() bool { return hot.Load().RequireTOTP })
	processCtx, stopProcess := context.WithCancel(ctx)
	defer stopProcess()
	p.Go(processCtx, func(ctx context.Context) { sweepDaily(ctx, store.NewWithDB(db).WithObjects(storage)) })
	p.Go(processCtx, func(ctx context.Context) {
		sweepMailImageCache(ctx, store.NewWithDB(db).WithObjects(storage), func() int64 { return hot.Load().MailImageCacheBytes })
	})
	// What AI features may quote is built from a queue the database fills (docs/internals/ai-retrieval.md).
	p.Go(processCtx, func(ctx context.Context) {
		store.NewWithDB(db).RunAIRetrieval(ctx, 2*time.Second, func(err error) { slog.Error("build AI retrieval", "error", err) })
	})
	first := newGeneration(p, loaded, config, slog.Default())
	first.Start(processCtx)
	handler := &swapHandler{}
	handler.Store(first.Handler())
	hostname, _ := os.Hostname()
	reload := newReloader(configStore, func(next serverconfig.Loaded, built builtConfig) (served, error) {
		return newGeneration(p, next, built, slog.Default()), nil
	}, hot, handler, first, loaded, serverconfig.Process{InstanceID: newInstanceID(), Hostname: hostname, BinaryVersion: version, StartedAt: time.Now()}, slog.Default())
	p.Go(processCtx, reload.Run)
	server := &http.Server{Addr: *address, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	select {
	case err := <-done:
		if err != http.ErrServerClosed {
			return fmt.Errorf("serve HTTP: %w", err)
		}
		return nil
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			return fmt.Errorf("shutdown HTTP server: %w", err)
		}
		reload.Stop(stopWorkersWithin)
		_ = configStore.Leave(context.WithoutCancel(ctx), reload.process.InstanceID)
		// Fixes being sent to GitHub finish (or time out) and revoke the person's token before the process ends.
		sent := make(chan struct{})
		go func() { carry.WaitForAutofixSends(); close(sent) }()
		select {
		case <-sent:
		case <-time.After(githubsync.DispatchTimeout):
		}
		return nil
	}
}

// newInstanceID names this process in server_processes.
func newInstanceID() string {
	raw := make([]byte, 16)
	_, _ = rand.Read(raw)
	return hex.EncodeToString(raw)
}

// refreshFoundingAtStart counts the Founding slots once so the state survives a restart. A failure
// leaves the state unknown, which keeps Founding on offer; the next Founding webhook counts again.
func refreshFoundingAtStart(ctx context.Context, api *httpapi.Server) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := api.RefreshFounding(ctx); err != nil {
		slog.Error("founding slot count at start failed", "error", err)
	}
}

// minSignupKeyBytes is the shortest SIGNUP_HASH_KEY accepted.
const minSignupKeyBytes = 32

// signupProblem explains why public sign-up must stay off on the hosted cloud (cloud sign-in and
// billing both on, which is when sign-up exists): it needs the key of the sign-up record hashes and
// the trusted proxies that reveal the client address. Without the second, every client shares the
// proxy's address and the fourth sign-up of the day is refused for everyone. A missing setting turns
// sign-up off with a logged reason instead of stopping the server, so the agents already working in
// their inboxes are never taken down by it. The text names variables, never values.
func signupProblem(cloudSignIn, billing bool, trustedProxies []netip.Prefix, key string) string {
	if !cloudSignIn || !billing {
		return ""
	}
	if len(key) < minSignupKeyBytes {
		return fmt.Sprintf("cloud sign-up is turned off: set SIGNUP_HASH_KEY to at least %d random characters (keeps the trial records pseudonymous; changing it later forgets who had a trial)", minSignupKeyBytes)
	}
	if len(trustedProxies) == 0 {
		return "cloud sign-up is turned off: set TRUSTED_PROXIES to the address of the reverse proxy as the server sees it (for example the Docker gateway, 172.18.0.1/32); without it every sign-up shares one client address"
	}
	return ""
}

// sweepSignupAddresses clears old client-address hashes at start and then every hour until ctx ends.
func sweepSignupAddresses(ctx context.Context, service *auth.Service) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		if _, err := service.ClearSignupAddresses(ctx); err != nil && ctx.Err() == nil {
			slog.Error("clear old sign-up addresses", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// lookupCNAME and lookupTXT answer the DNS questions of domain checks; nil uses the system resolver.
// End-to-end builds replace them with a local stand-in (e2e_dns.go).
var (
	lookupCNAME func(ctx context.Context, host string) (string, error)
	lookupTXT   func(ctx context.Context, name string) ([]string, error)
)

// recheckHelpCenterDomainsDaily looks up the DNS of every active custom help center domain at start
// and then daily until ctx ends, and records what it finds: a failure warns on the domain, and a
// domain no check has proved for 7 days goes back to pending.
func recheckHelpCenterDomainsDaily(ctx context.Context, api *httpapi.Server) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		checked, failed, released, err := api.RecheckHelpCenterDomains(ctx)
		if err != nil && ctx.Err() == nil {
			slog.Error("re-check help center domains", "error", err)
		}
		if failed > 0 {
			slog.Warn("help center domains whose DNS no longer proves their workspace", "checked", checked, "failed", failed, "released", released)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// sweepDaily runs the daily clean-up at start and then daily until ctx ends: it clears the call and
// answer of agent requests decided more than 90 days ago (the rows stay as the record of who asked and
// who decided), and deletes for good the saved replies and knowledge base articles and collections that
// have been in the trash for 30 days, and knowledge base images no article has linked for 30 days.
func sweepDaily(ctx context.Context, data *store.Store) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		if pruned, err := data.PruneAgentRequests(ctx); err != nil && ctx.Err() == nil {
			slog.Error("prune old agent requests", "error", err)
		} else if pruned > 0 {
			slog.Info("pruned old agent requests", "count", pruned)
		}
		if purged, err := data.PurgeSavedReplyTrash(ctx); err != nil && ctx.Err() == nil {
			slog.Error("purge saved reply trash", "error", err)
		} else if purged > 0 {
			slog.Info("purged saved replies deleted more than 30 days ago", "count", purged)
		}
		if purged, err := data.PurgeKBTrash(ctx); err != nil && ctx.Err() == nil {
			slog.Error("purge knowledge base trash", "error", err)
		} else if purged != (store.KBPurge{}) {
			slog.Info("purged knowledge base articles and collections deleted more than 30 days ago, and unused images",
				"articles", purged.Articles, "collections", purged.Collections, "images", purged.Images)
		}
		backfillKBMedia(ctx, data)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// backfillKBMedia gives the library images stored before the server read their size and made a
// thumbnail at upload (and imported ones) both, a batch at a time until none is left.
func backfillKBMedia(ctx context.Context, data *store.Store) {
	total := 0
	for {
		done, err := data.BackfillKBMedia(ctx, store.KBBackfillBatch)
		if err != nil {
			if ctx.Err() == nil {
				slog.Error("backfill knowledge base images", "error", err)
			}
			return
		}
		total += done
		if done < store.KBBackfillBatch {
			break
		}
	}
	if total > 0 {
		slog.Info("read the size and made the thumbnail of older knowledge base images", "count", total)
	}
}

// attachmentStorage opens the local-disk attachment store at ATTACHMENTS_DIR
// (default ./data/attachments). serve and import must point at the same place.
func attachmentStorage() (*attachments.LocalDisk, error) {
	dir := os.Getenv("ATTACHMENTS_DIR")
	if dir == "" {
		dir = "data/attachments"
	}
	return attachments.NewLocalDisk(dir)
}

func cloudAuthConfig(getenv func(string) string) cloudauth.Config {
	return cloudauth.ConfigFromEnv(getenv)
}

// lockedScreenShipped says the web app shows a locked workspace (trial over, nothing paid) instead
// of a broken inbox. Billing locks workspaces, so serve keeps it off (and says so) until this is true.
const lockedScreenShipped = true

// billingAtStart keeps billing off while the web app cannot show the lock, and says why. The server
// still starts: an image older than its configuration must never take the inboxes down.
func billingAtStart(billing polar.Config, shipped bool) (polar.Config, string) {
	if billing.Enabled() && !shipped {
		return polar.Config{}, "billing is configured but turned off: this build has no locked screen yet, so workspaces would lock with no explanation in the web app"
	}
	return billing, ""
}

// defaultPoolConns is the pool size when the database URL has no pool_max_conns. pgx would pick
// max(4, CPU count), which a handful of slow workspace exports could exhaust; this stays well above
// the number of exports the server allows at once.
const defaultPoolConns = 20

// openPool connects with an explicit pool size. Put pool_max_conns=N in the URL to change it.
func openPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	if parsed, err := url.Parse(databaseURL); err != nil || !parsed.Query().Has("pool_max_conns") {
		config.MaxConns = defaultPoolConns
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	return pool, nil
}
