package channeltest_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
	"github.com/colliedesk/colliedesk/server/internal/channels/pipeline"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// mediaServer serves loopback media over https: ok, dead (404), big (over the limit) and flaky (500
// once, then ok).
func mediaServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var flaky, hits atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/ok.txt":
			_, _ = w.Write([]byte("photo bytes"))
		case "/big.bin":
			_, _ = w.Write([]byte(strings.Repeat("x", 2000)))
		case "/flaky.txt":
			if flaky.Add(1) == 1 {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			_, _ = w.Write([]byte("second time"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server, &hits
}

func guardedPipeline(d loopDesk, server *httptest.Server) *pipeline.Pipeline {
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	client := pipeline.FetchClient(map[string]bool{server.Listener.Addr().String(): true}, &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12})
	return d.pipeline.WithFetchClient(client)
}

// Files served by link are fetched once into storage before the message is parsed: a dead link or
// a file too large leaves a placeholder and the rest of the message, a failure that may pass is
// tried again.
func TestChannelFilesAreFetchedBeforeParsing(t *testing.T) {
	d := newLoopDesk(t, channeltest.NewLoopback("loopback", channels.Async))
	server, _ := mediaServer(t)
	d.pipeline = guardedPipeline(d, server)
	d.send(t, channeltest.LoopbackEvent{ID: "m1", From: "u1", Text: "see attached", Links: []channeltest.LoopbackLink{
		{URL: server.URL + "/ok.txt", Name: "ok.txt", Type: "text/plain"},
		{URL: server.URL + "/dead.png", Name: "dead.png", Type: "image/png"},
		{URL: server.URL + "/big.bin", Name: "big.bin"},
		{URL: server.URL + "/flaky.txt", Name: "flaky.txt"},
		{URL: strings.Replace(server.URL, "https", "http", 1) + "/ok.txt", Name: "plain.txt"},
	}})
	if got := d.one(t, `SELECT string_agg(name||':'||status||':'||attempts||':'||attached,',' ORDER BY idx) FROM channel_inbound_files WHERE workspace_id=$1`); got !=
		"ok.txt:stored:1:true,dead.png:failed:1:false,big.bin:failed:1:false,flaky.txt:stored:2:true,plain.txt:failed:1:false" {
		t.Fatalf("files=%s", got)
	}
	if got := d.one(t, `SELECT string_agg(filename,',' ORDER BY filename) FROM attachments WHERE workspace_id=$1`); got != "flaky.txt,ok.txt" {
		t.Fatalf("attachments=%s", got)
	}
	if got := d.one(t, `SELECT channel_meta->>'files_failed' FROM messages WHERE workspace_id=$1`); got != `["dead.png", "big.bin", "plain.txt"]` {
		t.Fatalf("placeholders=%s", got)
	}
	if got := d.one(t, `SELECT (counted_storage_bytes=(SELECT sum(size) FROM attachments WHERE workspace_id=$1))::text FROM workspaces WHERE id=$1`); got != "true" {
		t.Fatalf("a fetched file that became an attachment is counted twice or not at all")
	}
}

// An event held while its channel is off has its files already: replaying it days later, when the
// links are dead, fetches nothing and loses nothing. Discarding it frees what its files took.
func TestHeldEventKeepsItsFetchedFiles(t *testing.T) {
	d := newLoopDesk(t, channeltest.NewLoopback("loopback", channels.Async))
	server, hits := mediaServer(t)
	d.pipeline = guardedPipeline(d, server)
	ctx := context.Background()
	off := false
	if _, err := d.store.UpdateChannel(ctx, d.workspaceID, d.channel.ID, store.ChannelInput{Enabled: &off}, store.ChannelActor{AgentID: d.agentID}); err != nil {
		t.Fatal(err)
	}
	d.send(t, channeltest.LoopbackEvent{ID: "h1", From: "u1", Text: "photo", Links: []channeltest.LoopbackLink{{URL: server.URL + "/ok.txt", Name: "ok.txt"}}},
		channeltest.LoopbackEvent{ID: "h2", From: "u2", Text: "spam", Links: []channeltest.LoopbackLink{{URL: server.URL + "/flaky.txt", Name: "f.txt"}}})
	if got := d.one(t, `SELECT string_agg(e.state||':'||f.status,',' ORDER BY e.external_event_id) FROM channel_inbound_events e
		JOIN channel_inbound_files f ON f.event_id=e.id WHERE e.workspace_id=$1`); got != "held:stored,held:stored" {
		t.Fatalf("held events' files=%s, want fetched before holding", got)
	}
	if got := d.one(t, `SELECT counted_storage_bytes::text FROM workspaces WHERE id=$1`); got != "22" {
		t.Fatalf("counted=%s, want the held files' 22 bytes", got)
	}
	server.Close()
	before := hits.Load()
	held, err := d.store.HeldEvents(ctx, d.workspaceID, d.channel.ID, nil)
	if err != nil || len(held) != 2 {
		t.Fatalf("held=%v %v", held, err)
	}
	if _, err := d.pipeline.ReplayHeld(ctx, d.workspaceID, d.channel.ID, []string{held[0].ID}, "", store.ChannelActor{AgentID: d.agentID}); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != before {
		t.Fatal("the replay fetched the file again")
	}
	if got := d.one(t, `SELECT string_agg(filename,',') FROM attachments WHERE workspace_id=$1`); got != "ok.txt" {
		t.Fatalf("replayed attachments=%s", got)
	}
	if _, err := d.store.DiscardHeld(ctx, d.workspaceID, d.channel.ID, nil, store.ChannelActor{AgentID: d.agentID}); err != nil {
		t.Fatal(err)
	}
	if got := d.one(t, `SELECT counted_storage_bytes::text FROM workspaces WHERE id=$1`); got != "11" {
		t.Fatalf("counted=%s after discarding, want only the replayed attachment's 11 bytes", got)
	}
}

// The fetch never reaches the host's own network: a link to a local address fails as blocked.
func TestChannelFileLinkToALocalAddressIsBlocked(t *testing.T) {
	d := newLoopDesk(t, channeltest.NewLoopback("loopback", channels.Async))
	server, hits := mediaServer(t)
	d.send(t, channeltest.LoopbackEvent{ID: "b1", From: "u1", Text: "x", Links: []channeltest.LoopbackLink{{URL: server.URL + "/ok.txt", Name: "ok.txt"}}})
	if got := d.one(t, `SELECT status||':'||(error LIKE '%private, loopback or link-local%')::text FROM channel_inbound_files WHERE workspace_id=$1`); got != "failed:true" {
		t.Fatalf("file=%s, want blocked", got)
	}
	if hits.Load() != 0 {
		t.Fatal("the local server was reached")
	}
}
