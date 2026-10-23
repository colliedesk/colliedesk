package httpapi_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/mailimage"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// pngOf is a PNG-signed body of the given size; each size has different bytes, so each is its own stored object.
func pngOf(size int, fill byte) []byte {
	body := make([]byte, size)
	copy(body, tinyPNG)
	for i := len(tinyPNG); i < size; i++ {
		body[i] = fill
	}
	return body
}

type limitDesk struct {
	api     http.Handler
	service *auth.Service
	pool    *pgxpool.Pool
	storage attachments.Storage
	dir     string
}

func newLimitDesk(t *testing.T, options mailimage.Options, mutate func(*httpapi.Config)) *limitDesk {
	t.Helper()
	dir := t.TempDir()
	storage, err := attachments.NewLocalDisk(dir)
	if err != nil {
		t.Fatal(err)
	}
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		config.Attachments = storage
		config.MailImages = mailimage.NewFetcher(options)
		if mutate != nil {
			mutate(config)
		}
	})
	return &limitDesk{api: api, service: service, pool: pool, storage: storage, dir: dir}
}

// workspace signs up a team with an email channel and returns its admin session and inbound secret.
func (d *limitDesk) workspace(t *testing.T, name, domain string) (admin, secret string) {
	t.Helper()
	admin = createSession(t, d.api, d.service, name, "admin@"+domain)
	return admin, connectEmail(t, d.api, admin, "support@"+domain)
}

func TestMailImageProxyQueuesInsteadOfRefusingWhenFetchesAreBusy(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 4)
	host := newImageHost(t, func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-release
		w.Write(tinyPNG)
	})
	desk := newLimitDesk(t, allowing(host), func(config *httpapi.Config) { config.MailImageConcurrency = 1 })
	admin, secret := desk.workspace(t, "Queue Desk", "queue-desk.example.com")
	paths := proxyPaths(t, desk.api, admin, secret, `<img src="`+host.URL+`/a.png"><img src="`+host.URL+`/b.png">`)
	codes := make(chan int, 2)
	for _, path := range paths {
		go func(path string) { codes <- request(t, desk.api, http.MethodGet, path, nil, admin, nil).Code }(path)
	}
	<-started
	select {
	case code := <-codes:
		t.Fatalf("a request was answered (%d) while every slot was busy and the queue wait had not run out", code)
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	for range paths {
		if code := <-codes; code != http.StatusOK {
			t.Fatalf("a queued image got status %d, want 200 once a slot freed", code)
		}
	}
}

func TestMailImageProxyGivesUpAfterTheQueueWait(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 2)
	host := newImageHost(t, func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-release
		w.Write(tinyPNG)
	})
	desk := newLimitDesk(t, allowing(host), func(config *httpapi.Config) {
		config.MailImageConcurrency = 1
		config.MailImageQueueWait = 200 * time.Millisecond
	})
	admin, secret := desk.workspace(t, "Wait Desk", "wait-desk.example.com")
	paths := proxyPaths(t, desk.api, admin, secret, `<img src="`+host.URL+`/a.png"><img src="`+host.URL+`/b.png">`)
	done := make(chan int, 1)
	go func() { done <- request(t, desk.api, http.MethodGet, paths[0], nil, admin, nil).Code }()
	<-started
	begun := time.Now()
	response := request(t, desk.api, http.MethodGet, paths[1], nil, admin, nil)
	if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") == "" || time.Since(begun) < 150*time.Millisecond {
		t.Fatalf("status=%d after %v, want 503 with Retry-After once the wait ran out", response.Code, time.Since(begun))
	}
	close(release)
	if code := <-done; code != http.StatusOK {
		t.Fatalf("first fetch status=%d", code)
	}
}

func TestOneWorkspaceCannotTakeEveryFetchSlot(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 8)
	host := newImageHost(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/slow") {
			started <- struct{}{}
			<-release
		}
		w.Write(tinyPNG)
	})
	desk := newLimitDesk(t, allowing(host), func(config *httpapi.Config) {
		config.MailImageConcurrency = 2
		config.MailImageWorkspaceConcurrency = 1
	})
	adminA, secretA := desk.workspace(t, "Hog Desk", "hog-desk.example.com")
	adminB, secretB := desk.workspace(t, "Fair Desk", "fair-desk.example.com")
	pathsA := proxyPaths(t, desk.api, adminA, secretA, `<img src="`+host.URL+`/slow1.png"><img src="`+host.URL+`/slow2.png">`)
	pathsB := proxyPaths(t, desk.api, adminB, secretB, `<img src="`+host.URL+`/fast.png">`)
	codes := make(chan int, 2)
	for _, path := range pathsA {
		go func(path string) { codes <- request(t, desk.api, http.MethodGet, path, nil, adminA, nil).Code }(path)
	}
	<-started
	begun := time.Now()
	if response := request(t, desk.api, http.MethodGet, pathsB[0], nil, adminB, nil); response.Code != http.StatusOK || time.Since(begun) > 2*time.Second {
		t.Fatalf("another workspace's image: status=%d after %v while a workspace held its share of slots", response.Code, time.Since(begun))
	}
	select {
	case <-started:
		t.Fatal("the same workspace ran two fetches at once, over its own limit")
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	for range pathsA {
		if code := <-codes; code != http.StatusOK {
			t.Fatalf("workspace A image status=%d", code)
		}
	}
}

func TestTwoServerProcessesOnOneDatabaseAcceptEachOthersImageLinks(t *testing.T) {
	host := newImageHost(t, serveBytes("image/png", tinyPNG))
	pool := testPool(t)
	storage, err := attachments.NewLocalDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mutate := func(config *httpapi.Config) {
		config.Attachments = storage
		config.MailImages = mailimage.NewFetcher(allowing(host))
	}
	first, service, _, _ := testAPIOnPool(t, pool, mutate)
	second, _, _, _ := testAPIOnPool(t, pool, mutate) // a replica, or the next deploy while the first still runs
	admin := createSession(t, first, service, "Replica Desk", "admin@replica-desk.example.com")
	secret := connectEmail(t, first, admin, "support@replica-desk.example.com")
	paths := proxyPaths(t, first, admin, secret, `<img src="`+host.URL+`/logo.png">`)
	if response := request(t, second, http.MethodGet, paths[0], nil, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("a link signed by one process was refused by another: status=%d", response.Code)
	}
	third, _, _, _ := testAPIOnPool(t, pool, mutate) // a restart
	if response := request(t, third, http.MethodGet, paths[0], nil, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("a link signed before a restart was refused after it: status=%d", response.Code)
	}
}

func TestMailImageSigningKeyIsOneRowOf32BytesCreatedOnceForAllProcesses(t *testing.T) {
	pool := testPool(t)
	_, _, _, _ = testAPIOnPool(t, pool, nil)
	_, _, _, _ = testAPIOnPool(t, pool, nil)
	var rows, length int
	if err := pool.QueryRow(context.Background(), `SELECT count(*), COALESCE(max(octet_length(value)),0) FROM server_secrets WHERE name='mail_image_key'`).Scan(&rows, &length); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || length != 32 {
		t.Fatalf("%d key rows of %d bytes, want one of 32", rows, length)
	}
}

// The request connection runs as the app role; it must not be able to read the key, whatever a query asks.
func TestAppRoleCannotReadTheServerSecrets(t *testing.T) {
	pool := testPool(t)
	_, _, _, _ = testAPIOnPool(t, pool, nil)
	ctx := context.Background()
	var role string
	if err := pool.QueryRow(ctx, `SELECT roles[1]::text FROM pg_policies WHERE schemaname=current_schema() AND tablename='workspaces' AND policyname='workspace_isolation'`).Scan(&role); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `SELECT set_config('role',$1,true)`, role); err != nil {
		t.Fatal(err)
	}
	var count int
	err = tx.QueryRow(ctx, `SELECT count(*) FROM server_secrets`).Scan(&count)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("as the app role, reading server_secrets gave %v (count %d), want permission denied (42501)", err, count)
	}
}

// With no key in the database, a process that signed with a key of its own would answer 403 to images signed
// by its replicas without a sign. It stops at start instead.
func TestServerRefusesToStartWhenItCannotReadTheImageSigningKey(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), databasetest.EmptySchema(t)) // never migrated: no server_secrets table
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	defer func() {
		recovered := recover()
		if recovered == nil || !strings.Contains(fmt.Sprint(recovered), "image signing key") {
			t.Fatalf("recovered %v, want a panic that names the image signing key", recovered)
		}
	}()
	httpapi.New(pool, httpapi.Config{Origin: testOrigin})
}

// cacheRows lists the stored object key of every cached image, by original URL hash order of insertion.
func cacheKeys(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT storage_key FROM mail_image_cache ORDER BY fetched_at`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, key)
	}
	return keys
}

func stored(t *testing.T, storage attachments.Storage, key string) bool {
	t.Helper()
	body, err := storage.Open(context.Background(), key)
	if err != nil {
		return false
	}
	body.Close()
	return true
}

func TestMailImageCacheSweepDropsOldImagesThenTheLeastRecentlyServedOverTheCap(t *testing.T) {
	host := newImageHost(t, func(w http.ResponseWriter, r *http.Request) {
		size := map[string]int{"/old.png": 1000, "/idle.png": 1100, "/busy.png": 1200}[r.URL.Path]
		w.Write(pngOf(size, byte(size)))
	})
	desk := newLimitDesk(t, allowing(host), nil)
	admin, secret := desk.workspace(t, "Sweep Desk", "sweep-desk.example.com")
	paths := proxyPaths(t, desk.api, admin, secret, `<img src="`+host.URL+`/old.png"><img src="`+host.URL+`/idle.png"><img src="`+host.URL+`/busy.png">`)
	for _, path := range paths {
		if response := request(t, desk.api, http.MethodGet, path, nil, admin, nil); response.Code != http.StatusOK {
			t.Fatalf("status=%d", response.Code)
		}
	}
	keys := cacheKeys(t, desk.pool)
	if len(keys) != 3 {
		t.Fatalf("cache holds %d images", len(keys))
	}
	oldKey, idleKey, busyKey := keys[0], keys[1], keys[2]
	exec := func(sql string, args ...any) {
		if _, err := desk.pool.Exec(context.Background(), sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE mail_image_cache SET fetched_at=now()-interval '40 days', last_served_at=now()-interval '2 days' WHERE storage_key=$1`, oldKey)
	exec(`UPDATE mail_image_cache SET last_served_at=now()-interval '10 days' WHERE storage_key=$1`, idleKey)

	data := store.New(desk.pool).WithObjects(desk.storage)
	// Pass 1: the 40-day-old image goes by age alone, the cap is not reached (1100 + 1200 <= 2500).
	if removed, err := data.SweepMailImageCache(context.Background(), 30*24*time.Hour, 2500); err != nil || removed != 1 {
		t.Fatalf("removed %d, %v; want 1 (the expired image)", removed, err)
	}
	if left := cacheKeys(t, desk.pool); len(left) != 2 || stored(t, desk.storage, oldKey) || !stored(t, desk.storage, idleKey) {
		t.Fatalf("after the age sweep: rows %v, old stored=%v", left, stored(t, desk.storage, oldKey))
	}
	// Pass 2: over a 1500 byte cap the image nobody has looked at for the longest goes, the busy one stays.
	if removed, err := data.SweepMailImageCache(context.Background(), 30*24*time.Hour, 1500); err != nil || removed != 1 {
		t.Fatalf("removed %d, %v; want 1 (the least recently served)", removed, err)
	}
	left := cacheKeys(t, desk.pool)
	if len(left) != 1 || left[0] != busyKey || stored(t, desk.storage, idleKey) || !stored(t, desk.storage, busyKey) {
		t.Fatalf("after the cap sweep: rows %v, idle stored=%v, busy stored=%v", left, stored(t, desk.storage, idleKey), stored(t, desk.storage, busyKey))
	}
}

func TestMailImageCacheSweepKeepsAFileAnAttachmentStillUses(t *testing.T) {
	host := newImageHost(t, serveBytes("image/png", pngOf(900, 7)))
	desk := newLimitDesk(t, allowing(host), nil)
	admin, secret := desk.workspace(t, "Shared Desk", "shared-desk.example.com")
	paths := proxyPaths(t, desk.api, admin, secret, `<img src="`+host.URL+`/a.png">`)
	if request(t, desk.api, http.MethodGet, paths[0], nil, admin, nil).Code != http.StatusOK {
		t.Fatal("fetch failed")
	}
	key := cacheKeys(t, desk.pool)[0]
	if _, err := desk.pool.Exec(context.Background(), `INSERT INTO attachments(workspace_id,message_id,filename,content_type,size,sha256,storage_key)
		SELECT m.workspace_id,m.id,'same.png','image/png',900,repeat('a',64),$1 FROM messages m LIMIT 1`, key); err != nil {
		t.Fatal(err)
	}
	if _, err := desk.pool.Exec(context.Background(), `UPDATE mail_image_cache SET fetched_at=now()-interval '90 days'`); err != nil {
		t.Fatal(err)
	}
	if removed, err := store.New(desk.pool).WithObjects(desk.storage).SweepMailImageCache(context.Background(), 30*24*time.Hour, 1<<30); err != nil || removed != 1 {
		t.Fatalf("removed %d, %v", removed, err)
	}
	if !stored(t, desk.storage, key) {
		t.Fatal("the sweep deleted bytes an attachment still points at")
	}
}

func TestMailImageCacheRefusesToGrowPastItsCapButStillServesTheImage(t *testing.T) {
	host := newImageHost(t, func(w http.ResponseWriter, r *http.Request) { w.Write(pngOf(1000, byte(len(r.URL.Path)))) })
	desk := newLimitDesk(t, allowing(host), func(config *httpapi.Config) { config.MailImageCacheBytes = 1500 })
	admin, secret := desk.workspace(t, "Cap Desk", "cap-desk.example.com")
	paths := proxyPaths(t, desk.api, admin, secret, `<img src="`+host.URL+`/a.png"><img src="`+host.URL+`/bb.png">`)
	for _, path := range paths {
		if response := request(t, desk.api, http.MethodGet, path, nil, admin, nil); response.Code != http.StatusOK || response.Body.Len() != 1000 {
			t.Fatalf("status=%d, %d bytes", response.Code, response.Body.Len())
		}
	}
	if keys := cacheKeys(t, desk.pool); len(keys) != 1 {
		t.Fatalf("cache holds %d images, want 1: the second would pass the 1500 byte cap", len(keys))
	}
	if entries, _ := os.ReadDir(desk.dir); len(entries) == 0 {
		t.Fatal("storage is empty")
	}
}

func TestServingACachedImageNotesTheUseAtMostOncePerDay(t *testing.T) {
	host := newImageHost(t, serveBytes("image/png", pngOf(800, 3)))
	desk := newLimitDesk(t, allowing(host), nil)
	admin, secret := desk.workspace(t, "Use Desk", "use-desk.example.com")
	paths := proxyPaths(t, desk.api, admin, secret, `<img src="`+host.URL+`/a.png">`)
	get := func() { request(t, desk.api, http.MethodGet, paths[0], nil, admin, nil) }
	get()
	age := func() time.Duration {
		var seconds float64
		if err := desk.pool.QueryRow(context.Background(), `SELECT extract(epoch from now()-last_served_at) FROM mail_image_cache`).Scan(&seconds); err != nil {
			t.Fatal(err)
		}
		return time.Duration(seconds * float64(time.Second))
	}
	if _, err := desk.pool.Exec(context.Background(), `UPDATE mail_image_cache SET last_served_at=now()-interval '3 days'`); err != nil {
		t.Fatal(err)
	}
	get()
	if age() > time.Minute {
		t.Fatalf("a view three days after the last one left last_served_at %v old", age())
	}
	if _, err := desk.pool.Exec(context.Background(), `UPDATE mail_image_cache SET last_served_at=now()-interval '2 hours'`); err != nil {
		t.Fatal(err)
	}
	get()
	if age() < time.Hour {
		t.Fatalf("a view two hours after the last one rewrote last_served_at (now %v old)", age())
	}
	_ = httptest.NewRecorder
}

// The sweep's cap is per workspace, like the cap on writing: a workspace over its share loses its own oldest
// images and no one else's.
func TestCacheSweepNeverEvictsOneWorkspaceBecauseAnotherIsOverItsCap(t *testing.T) {
	host := newImageHost(t, func(w http.ResponseWriter, r *http.Request) {
		size := map[string]int{"/big1.png": 1000, "/big2.png": 1100, "/small.png": 300}[r.URL.Path]
		w.Write(pngOf(size, byte(size)))
	})
	desk := newLimitDesk(t, allowing(host), nil)
	adminA, secretA := desk.workspace(t, "Big Desk", "big-desk.example.com")
	adminB, secretB := desk.workspace(t, "Small Desk", "small-desk.example.com")
	for _, item := range []struct{ admin, secret, markup string }{
		{adminA, secretA, `<img src="` + host.URL + `/big1.png"><img src="` + host.URL + `/big2.png">`},
		{adminB, secretB, `<img src="` + host.URL + `/small.png">`},
	} {
		for _, path := range proxyPaths(t, desk.api, item.admin, item.secret, item.markup) {
			if request(t, desk.api, http.MethodGet, path, nil, item.admin, nil).Code != http.StatusOK {
				t.Fatal("fetch failed")
			}
		}
	}
	// The small workspace's image is the least recently served of all three.
	if _, err := desk.pool.Exec(context.Background(), `UPDATE mail_image_cache SET last_served_at=now()-interval '3 days' WHERE size=300`); err != nil {
		t.Fatal(err)
	}
	removed, err := store.New(desk.pool).WithObjects(desk.storage).SweepMailImageCache(context.Background(), 30*24*time.Hour, 1500)
	if err != nil || removed != 1 {
		t.Fatalf("removed %d, %v; want 1 (only the big workspace's own oldest image)", removed, err)
	}
	var small, big int
	if err := desk.pool.QueryRow(context.Background(), `SELECT count(*) FILTER (WHERE size=300), count(*) FILTER (WHERE size<>300) FROM mail_image_cache`).Scan(&small, &big); err != nil {
		t.Fatal(err)
	}
	if small != 1 || big != 1 {
		t.Fatalf("small workspace kept %d images, big workspace kept %d; want 1 and 1", small, big)
	}
}

// stallingWriter is a client that has stopped reading: its Write waits until released.
type stallingWriter struct {
	header  http.Header
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *stallingWriter) Header() http.Header { return w.header }
func (w *stallingWriter) WriteHeader(int)     {}
func (w *stallingWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.started) })
	<-w.release
	return len(p), nil
}

// The bytes are in memory when the response starts, so a client that reads slowly must not keep a fetch slot.
func TestASlowReadingClientDoesNotHoldAFetchSlot(t *testing.T) {
	host := newImageHost(t, serveBytes("image/png", tinyPNG))
	desk := newLimitDesk(t, allowing(host), func(config *httpapi.Config) {
		config.MailImageConcurrency = 1
		config.MailImageQueueWait = time.Second
	})
	admin, secret := desk.workspace(t, "Slow Desk", "slow-desk.example.com")
	paths := proxyPaths(t, desk.api, admin, secret, `<img src="`+host.URL+`/a.png"><img src="`+host.URL+`/b.png">`)
	stalled := &stallingWriter{header: http.Header{}, started: make(chan struct{}), release: make(chan struct{})}
	finished := make(chan struct{})
	go func() {
		desk.api.ServeHTTP(stalled, newRequest(t, http.MethodGet, paths[0], nil, admin, nil))
		close(finished)
	}()
	select {
	case <-stalled.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the first response never started")
	}
	if response := request(t, desk.api, http.MethodGet, paths[1], nil, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("a second image got %d while a client was slow to read the first", response.Code)
	}
	close(stalled.release)
	<-finished
}
