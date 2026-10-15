package httpapi_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"runtime/debug"
	"sync/atomic"
	"testing"
	"time"
)

// A 5,000-conversation workspace with 20,000 messages of about 3 KiB each exports to tens of
// megabytes; the server must start answering at once and never hold the archive in memory.
func TestExportOfALargeWorkspaceStartsFastAndStaysInConstantMemory(t *testing.T) {
	if testing.Short() {
		t.Skip("seeds a large workspace")
	}
	api, service, pool, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "Export Large", "admin@export-large.example.com")
	workspace := workspaceID(t, api, admin)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO contacts(workspace_id,name,email) SELECT $1,'Customer '||n,'c'||n||'@large.example.com' FROM generate_series(1,5000) n`, workspace); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO conversations(workspace_id,contact_id,subject)
		SELECT $1,id,'Ticket '||name FROM contacts WHERE workspace_id=$1`, workspace); err != nil {
		t.Fatal(err)
	}
	// Distinct pseudo-random text per message so ZIP compression cannot hide the volume.
	if _, err := pool.Exec(ctx, `INSERT INTO messages(workspace_id,conversation_id,kind,body)
		SELECT $1,c.id,'note',(SELECT string_agg(md5(c.id::text||n||k),'') FROM generate_series(1,96) k)
		FROM conversations c CROSS JOIN generate_series(1,4) n WHERE c.workspace_id=$1`, workspace); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api)
	defer server.Close()

	// A tight GC target makes HeapAlloc follow live data instead of uncollected garbage.
	defer debug.SetGCPercent(debug.SetGCPercent(20))
	runtime.GC()
	var baseline runtime.MemStats
	runtime.ReadMemStats(&baseline)
	var peak atomic.Uint64
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			var stats runtime.MemStats
			runtime.ReadMemStats(&stats)
			if stats.HeapAlloc > peak.Load() {
				peak.Store(stats.HeapAlloc)
			}
			select {
			case <-stop:
				return
			case <-time.After(5 * time.Millisecond):
			}
		}
	}()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/export", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+admin)
	started := time.Now()
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if _, err := response.Body.Read(make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	firstByte := time.Since(started)
	copied, err := io.Copy(io.Discard, response.Body)
	close(stop)
	<-done
	if err != nil {
		t.Fatal(err)
	}
	total := copied + 1
	growth := int64(peak.Load()) - int64(baseline.HeapAlloc)
	t.Logf("first byte %v, archive %d MiB, heap growth %d MiB", firstByte, total>>20, growth>>20)
	if firstByte > 200*time.Millisecond {
		t.Errorf("first byte after %v, budget 200ms", firstByte)
	}
	if total < 24<<20 {
		t.Fatalf("archive is only %d bytes; the fixture is too small to prove streaming", total)
	}
	if growth > 12<<20 {
		t.Errorf("heap grew %d bytes while exporting %d bytes: the archive is being buffered", growth, total)
	}
}
