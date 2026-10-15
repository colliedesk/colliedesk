package httpapi_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A 2,000-conversation workspace with 23,000 messages, one 3,000-message HTML thread and 4,000 attachments exports to tens of
// megabytes; the server must start answering at once and never hold the archive in memory.
func TestExportOfALargeWorkspaceStartsFastAndStaysInConstantMemory(t *testing.T) {
	if testing.Short() {
		t.Skip("seeds a large workspace")
	}
	api, service, pool, storage := testAPIWithStorage(t)
	admin := createSession(t, api, service, "Export Large", "admin@export-large.example.com")
	workspace := workspaceID(t, api, admin)
	ctx := context.Background()
	seedConversations(t, pool, workspace, 2000, 4)
	// One endless thread of HTML mails: held in memory it would cost tens of megabytes.
	if _, err := pool.Exec(ctx, `INSERT INTO messages(workspace_id,conversation_id,kind,body,body_html)
		SELECT $1,(SELECT id FROM conversations WHERE workspace_id=$1 ORDER BY id LIMIT 1),'incoming','long thread '||n,repeat('<p>quoted reply '||n||'</p>',400)
		FROM generate_series(1,3000) n`, workspace); err != nil {
		t.Fatal(err)
	}
	// 4,000 attachments of 4 KiB; storage is content-addressed, so they share one stored file.
	object, err := storage.Put(ctx, workspace, strings.NewReader(strings.Repeat("x", 4096)), 4096)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO attachments(workspace_id,message_id,filename,content_type,size,sha256,storage_key)
		SELECT $1,id,'file-'||row_number() OVER ()||'.bin','application/octet-stream',$2,$3,$4
		FROM (SELECT id FROM messages WHERE workspace_id=$1 ORDER BY id LIMIT 4000) m`, workspace, object.Size, object.SHA256, object.Key); err != nil {
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
	if total < 20<<20 {
		t.Fatalf("archive is only %d bytes; the fixture is too small to prove streaming", total)
	}
	if growth > 12<<20 {
		t.Errorf("heap grew %d bytes while exporting %d bytes: the archive is being buffered", growth, total)
	}
}
