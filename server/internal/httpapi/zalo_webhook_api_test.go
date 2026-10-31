package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels/pipeline"
	"github.com/colliedesk/colliedesk/server/internal/channels/registry"
	"github.com/colliedesk/colliedesk/server/internal/channels/zalo"
	"github.com/colliedesk/colliedesk/server/internal/google"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
	"github.com/jackc/pgx/v5/pgxpool"
)

// zaloDesk is two workspaces with an OA each, set up through the API as an admin does.
type zaloDesk struct {
	api            http.Handler
	pool           *pgxpool.Pool
	adminA, adminB string
	channelA       string
	oaA, oaB       string
}

func newZaloDesk(t *testing.T) zaloDesk {
	t.Helper()
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		keys, _ := tokenseal.ParseKeys("k1:AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM=")
		config.Google = google.Config{Keys: keys}
	})
	d := zaloDesk{api: api, pool: pool, oaA: fmt.Sprint(800000 + time.Now().UnixNano()%100000), oaB: fmt.Sprint(700000 + time.Now().UnixNano()%100000)}
	d.adminA = createSession(t, api, service, "Zalo A", "admin@zalo-a.example.com")
	d.adminB = createSession(t, api, service, "Zalo B", "admin@zalo-b.example.com")
	d.channelA = d.connect(t, d.adminA, d.oaA, "oa-secret-a")
	d.connect(t, d.adminB, d.oaB, "oa-secret-b")
	return d
}

func (d zaloDesk) connect(t *testing.T, admin, oa, secret string) string {
	t.Helper()
	id := create(t, d.api, admin, "/api/v1/channels", map[string]any{"type": "zalo", "config": map[string]any{"oa_id": oa, "app_id": "700001"}})
	if response := request(t, d.api, http.MethodPut, "/api/v1/channels/"+id+"/credentials", map[string]any{"name": zalo.CredentialOASecretKey, "value": secret}, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("set the OA secret key: %d %s", response.Code, response.Body.String())
	}
	return id
}

// deliver posts a delivery to the app's webhook signed with secret, as Zalo does.
func (d zaloDesk) deliver(t *testing.T, oa, id, text, secret string) *httptest.ResponseRecorder {
	t.Helper()
	timestamp := fmt.Sprint(time.Now().UnixMilli())
	body, _ := json.Marshal(map[string]any{"app_id": "700001", "event_name": "user_send_text", "timestamp": timestamp,
		"sender": map[string]string{"id": "1110001"}, "recipient": map[string]string{"id": oa}, "message": map[string]string{"msg_id": id, "text": text}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/channels/zalo/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(zalo.SignatureHeader, "mac="+zalo.Sign("700001", body, timestamp, secret))
	return serve(d.api, req)
}

// The app's one webhook takes each OA's deliveries into that OA's workspace only, signed with its own
// key; an OA no channel is answers 200 and leaves nothing; a wrong signature is refused, kept out, and the
// channel says so.
func TestTheZaloWebhookRoutesByOAAndChecksTheSignature(t *testing.T) {
	d := newZaloDesk(t)
	if response := d.deliver(t, d.oaA, "m-1", "Hello A", "oa-secret-a"); response.Code != http.StatusOK {
		t.Fatalf("A's delivery: %d %s", response.Code, response.Body.String())
	}
	if got := queryString(t, d.pool, `SELECT count(*)::text FROM channel_inbound_events e JOIN channels c ON c.id=e.channel_id WHERE c.external_account=$1`, d.oaA); got != "1" {
		t.Fatalf("A's journal = %s", got)
	}
	if response := d.deliver(t, "123456789", "m-2", "Someone else's OA", "x"); response.Code != http.StatusOK {
		t.Fatalf("an OA no channel is: %d %s", response.Code, response.Body.String())
	}
	if got := queryString(t, d.pool, `SELECT count(*)::text FROM channel_inbound_events WHERE external_event_id='m-2'`); got != "0" {
		t.Fatalf("an unknown OA's delivery was kept: %s", got)
	}
	// B's secret signs nothing of A's. A refused delivery answers as an OA no channel is, so nobody learns
	// which OAs are here; it is noted once a minute at most.
	unknown := d.deliver(t, "123456780", "m-4", "Someone else's OA", "x")
	for i := 0; i < 2; i++ {
		if response := d.deliver(t, d.oaA, fmt.Sprintf("m-3-%d", i), "Forged", "oa-secret-b"); response.Code != http.StatusOK || response.Body.String() != unknown.Body.String() {
			t.Fatalf("a delivery signed with another OA's key: %d %s", response.Code, response.Body.String())
		}
	}
	noted := queryString(t, d.pool, `SELECT signature_failed_at::text FROM zalo_channels WHERE channel_id=$1`, d.channelA)
	if _, err := d.pool.Exec(context.Background(), `UPDATE zalo_channels SET signature_failed_at=signature_failed_at-interval '30 seconds' WHERE channel_id=$1`, d.channelA); err != nil {
		t.Fatal(err)
	}
	d.deliver(t, d.oaA, "m-3-2", "Forged", "oa-secret-b")
	if again := queryString(t, d.pool, `SELECT (signature_failed_at+interval '30 seconds')::text FROM zalo_channels WHERE channel_id=$1`, d.channelA); again != noted {
		t.Fatalf("noted again within the minute: %s, then %s", noted, again)
	}
	if got := queryString(t, d.pool, `SELECT count(*)::text FROM channel_inbound_events WHERE external_event_id LIKE 'm-3%'`); got != "0" {
		t.Fatalf("a refused delivery was kept: %s", got)
	}
	var channel struct {
		Status struct {
			State      string `json:"state"`
			ReasonCode string `json:"reason_code"`
		} `json:"status"`
		Summary string `json:"summary"`
		Config  struct {
			OAID        string `json:"oa_id"`
			WebhookPath string `json:"webhook_path"`
			Credentials []struct {
				Name  string  `json:"name"`
				Hint  *string `json:"hint"`
				SetAt *string `json:"set_at"`
			} `json:"credentials"`
		} `json:"config"`
	}
	response := request(t, d.api, http.MethodGet, "/api/v1/channels/"+d.channelA, nil, d.adminA, nil)
	decode(t, response, &channel)
	if channel.Status.ReasonCode != zalo.ReasonSignatureFailed || channel.Config.OAID != d.oaA || channel.Config.WebhookPath != "/api/v1/channels/zalo/webhook" ||
		!strings.HasPrefix(channel.Summary, "OA ") || strings.Contains(response.Body.String(), "oa-secret-a") {
		t.Fatalf("A's channel = %s", response.Body.String())
	}
	// The secrets it shows are whether each is set, never a value.
	if len(channel.Config.Credentials) != 4 || channel.Config.Credentials[1].Name != zalo.CredentialOASecretKey || channel.Config.Credentials[1].Hint == nil ||
		*channel.Config.Credentials[1].Hint != "et-a" || channel.Config.Credentials[1].SetAt == nil || channel.Config.Credentials[0].Hint != nil || channel.Config.Credentials[0].SetAt != nil {
		t.Fatalf("credentials = %+v", channel.Config.Credentials)
	}
	// A delivery signed right again (the key fixed) clears it.
	if response := d.deliver(t, d.oaA, "m-5", "Fixed", "oa-secret-a"); response.Code != http.StatusOK {
		t.Fatalf("a delivery after the fix: %d", response.Code)
	}
	var fixed struct {
		Status struct {
			ReasonCode string `json:"reason_code"`
		} `json:"status"`
	}
	decode(t, request(t, d.api, http.MethodGet, "/api/v1/channels/"+d.channelA, nil, d.adminA, nil), &fixed)
	if fixed.Status.ReasonCode == zalo.ReasonSignatureFailed {
		t.Fatalf("still %s after a delivery signed right", fixed.Status.ReasonCode)
	}
	// Nothing of A's reaches B.
	if got := queryString(t, d.pool, `SELECT count(*)::text FROM channel_inbound_events e JOIN channels c ON c.id=e.channel_id
		WHERE c.external_account=$1`, d.oaB); got != "0" {
		t.Fatalf("B's journal = %s", got)
	}
	if other := request(t, d.api, http.MethodGet, "/api/v1/channels/"+d.channelA, nil, d.adminB, nil); other.Code != http.StatusNotFound {
		t.Fatalf("B reads A's channel: %d", other.Code)
	}
	// One OA is one channel on the server.
	taken := request(t, d.api, http.MethodPost, "/api/v1/channels", map[string]any{"type": "zalo", "config": map[string]any{"oa_id": d.oaA, "app_id": "700001"}}, d.adminB, nil)
	if taken.Code == http.StatusCreated {
		t.Fatalf("a second workspace took A's OA: %s", taken.Body.String())
	}
}

// A sender is named by their id until their name is read from Zalo, once their message is taken: the
// follower's profile, or a group member's name in the group's list. Then the contact and the chat show it.
func TestAZaloSendersNameIsReadOnceTheirMessageIsTaken(t *testing.T) {
	var calls []string
	followed := false
	zaloServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path+"?"+r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Header.Get("access_token") != "access-a":
			_, _ = w.Write([]byte(`{"error":-216}`))
		case r.URL.Path == "/v3.0/oa/user/detail" && strings.Contains(r.URL.RawQuery, "1110007") && followed:
			_, _ = w.Write([]byte(`{"error":0,"data":{"display_name":"Hoa"}}`))
		case r.URL.Path == "/v3.0/oa/user/detail" && strings.Contains(r.URL.RawQuery, "1110001"):
			_, _ = w.Write([]byte(`{"error":0,"data":{"display_name":"Minh Anh"}}`))
		case r.URL.Path == "/v3.0/oa/user/detail":
			_, _ = w.Write([]byte(`{"error":-213,"message":"user has not followed the OA"}`))
		case r.URL.Path == "/v3.0/oa/group/listmember":
			_, _ = w.Write([]byte(`{"error":0,"data":{"offset":0,"count":50,"total":2,"members":[{"user_id":"1110009","name":"Lan"},{"user_id":"1110002","name":"Bao"}]}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(zaloServer.Close)
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		keys, _ := tokenseal.ParseKeys("k1:AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM=")
		config.Google = google.Config{Keys: keys}
		config.Zalo = zalo.Config{MessageBase: zaloServer.URL, OAuthBase: zaloServer.URL, HTTP: zaloServer.Client(), Background: func(f func()) { f() }}
	})
	d := zaloDesk{api: api, pool: pool, oaA: fmt.Sprint(600000 + time.Now().UnixNano()%100000)}
	d.adminA = createSession(t, api, service, "Zalo Names", "admin@zalo-names.example.com")
	d.channelA = d.connect(t, d.adminA, d.oaA, "oa-secret-a")
	if response := request(t, api, http.MethodPut, "/api/v1/channels/"+d.channelA+"/credentials", map[string]any{"name": zalo.CredentialAccessToken, "value": "access-a"}, d.adminA, nil); response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	journal := pipeline.New(store.New(pool).WithChannels(registry.New(registry.Config{Zalo: zalo.Config{MessageBase: zaloServer.URL}})), nil)
	process := func() {
		t.Helper()
		if _, err := journal.ProcessDue(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	d.deliver(t, d.oaA, "n-1", "Hello", "oa-secret-a")
	process()
	if got := queryString(t, pool, `SELECT ct.name||'/'||COALESCE(i.display_name,'') FROM contact_identities i JOIN contacts ct ON ct.id=i.contact_id WHERE i.value='1110001'`); got != "Minh Anh/Minh Anh" {
		t.Fatalf("follower = %s", got)
	}
	if got := queryString(t, pool, `SELECT COALESCE(sender_name,'') FROM channel_thread_items WHERE external_id='n-1'`); got != "Minh Anh" {
		t.Fatalf("follower's item = %q", got)
	}
	// Known now: their next message asks Zalo nothing.
	before := len(calls)
	d.deliver(t, d.oaA, "n-2", "Again", "oa-secret-a")
	process()
	if len(calls) != before {
		t.Fatalf("asked again for a known name: %v", calls[before:])
	}
	// A group member who does not follow the OA is found in the group's list.
	timestamp := fmt.Sprint(time.Now().UnixMilli())
	body, _ := json.Marshal(map[string]any{"app_id": "700001", "oa_id": d.oaA, "event_name": "user_send_group_text", "timestamp": timestamp,
		"sender": map[string]string{"id": "1110002"}, "recipient": map[string]string{"id": "g-9"}, "message": map[string]string{"msg_id": "n-3", "text": "hi all"}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/channels/zalo/webhook", bytes.NewReader(body))
	req.Header.Set(zalo.SignatureHeader, "mac="+zalo.Sign("700001", body, timestamp, "oa-secret-a"))
	if response := serve(api, req); response.Code != http.StatusOK {
		t.Fatalf("group delivery: %d", response.Code)
	}
	process()
	if got := queryString(t, pool, `SELECT COALESCE(sender_name,'') FROM channel_thread_items WHERE external_id='n-3'`); got != "Bao" {
		t.Fatalf("group member's item = %q (calls %v)", got, calls)
	}
	// Zalo had no name for a customer at first: they are called by their id, and named once Zalo gives
	// it (an hour on), with what was stored of them.
	timestamp = fmt.Sprint(time.Now().UnixMilli())
	direct := func(id string) {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"app_id": "700001", "event_name": "user_send_text", "timestamp": timestamp,
			"sender": map[string]string{"id": "1110007"}, "recipient": map[string]string{"id": d.oaA}, "message": map[string]string{"msg_id": id, "text": "hello"}})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/channels/zalo/webhook", bytes.NewReader(body))
		req.Header.Set(zalo.SignatureHeader, "mac="+zalo.Sign("700001", body, timestamp, "oa-secret-a"))
		if response := serve(api, req); response.Code != http.StatusOK {
			t.Fatalf("delivery %s: %d", id, response.Code)
		}
		process()
	}
	direct("n-4")
	if got := queryString(t, pool, `SELECT ct.name FROM contact_identities i JOIN contacts ct ON ct.id=i.contact_id WHERE i.value='1110007'`); got != "1110007" {
		t.Fatalf("unnamed customer = %s", got)
	}
	followed = true
	if _, err := pool.Exec(context.Background(), `UPDATE zalo_users SET looked_up_at=looked_up_at-interval '2 hours' WHERE user_id='1110007'`); err != nil {
		t.Fatal(err)
	}
	direct("n-5")
	if got := queryString(t, pool, `SELECT ct.name||'/'||(SELECT string_agg(COALESCE(sender_name,'-'),',' ORDER BY external_id) FROM channel_thread_items WHERE sender_value='1110007')
		FROM contact_identities i JOIN contacts ct ON ct.id=i.contact_id WHERE i.value='1110007'`); got != "Hoa/Hoa,Hoa" {
		t.Fatalf("named later = %s", got)
	}
}

// A lookup that failed for a reason of the moment (a refused token, the network) is tried again with the
// next message; one Zalo answered otherwise waits its hour. A group's member list names every member at
// once. A contact made before its name was kept takes it with the next message. Removing the channel
// forgets the names Zalo gave.
func TestAZaloNameLookupRetriesWhatFailedForNowAndKeepsAGroupsNames(t *testing.T) {
	token := "access-a"
	var calls []string
	zaloServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path+"?"+r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Header.Get("access_token") != token:
			_, _ = w.Write([]byte(`{"error":-216}`))
		case r.URL.Path == "/v3.0/oa/user/detail" && strings.Contains(r.URL.RawQuery, "1110001"):
			_, _ = w.Write([]byte(`{"error":0,"data":{"display_name":"Minh Anh"}}`))
		case r.URL.Path == "/v3.0/oa/user/detail" && (strings.Contains(r.URL.RawQuery, "1110008") || strings.Contains(r.URL.RawQuery, "1110011")):
			_, _ = w.Write([]byte(`{"error":-999,"message":"something else"}`))
		case r.URL.Path == "/v3.0/oa/user/detail" && strings.Contains(r.URL.RawQuery, "1110012"):
			_, _ = w.Write([]byte(`{"error":0,"data":{"display_name":""}}`))
		case r.URL.Path == "/v3.0/oa/user/detail":
			_, _ = w.Write([]byte(`{"error":-213,"message":"user has not followed the OA"}`))
		case r.URL.Path == "/v3.0/oa/group/listmember" && r.URL.Query().Get("group_id") == "g-8":
			_, _ = w.Write([]byte(`{"error":0,"data":{"total":1,"members":[{"user_id":"1110011","name":"Khoa"}]}}`))
		case r.URL.Path == "/v3.0/oa/group/listmember" && r.URL.Query().Get("group_id") == "g-9":
			_, _ = w.Write([]byte(`{"error":0,"data":{"total":1,"members":[{"user_id":"1110012","name":"Vy"}]}}`))
		case r.URL.Path == "/v3.0/oa/group/listmember":
			_, _ = w.Write([]byte(`{"error":0,"data":{"total":2,"members":[{"user_id":"1110009","name":"Lan"},{"user_id":"1110002","name":"Bao"}]}}`))
		}
	}))
	t.Cleanup(zaloServer.Close)
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		keys, _ := tokenseal.ParseKeys("k1:AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM=")
		config.Google = google.Config{Keys: keys}
		config.Zalo = zalo.Config{MessageBase: zaloServer.URL, OAuthBase: zaloServer.URL, HTTP: zaloServer.Client(), Background: func(f func()) { f() }}
	})
	d := zaloDesk{api: api, pool: pool, oaA: fmt.Sprint(500000 + time.Now().UnixNano()%100000)}
	d.adminA = createSession(t, api, service, "Zalo Retry", "admin@zalo-retry.example.com")
	d.channelA = d.connect(t, d.adminA, d.oaA, "oa-secret-a")
	if response := request(t, api, http.MethodPut, "/api/v1/channels/"+d.channelA+"/credentials", map[string]any{"name": zalo.CredentialAccessToken, "value": "access-a"}, d.adminA, nil); response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	journal := pipeline.New(store.New(pool).WithChannels(registry.New(registry.Config{Zalo: zalo.Config{MessageBase: zaloServer.URL}})), nil)
	send := func(event, sender, recipient, id string) {
		t.Helper()
		timestamp := fmt.Sprint(time.Now().UnixMilli())
		envelope := map[string]any{"app_id": "700001", "event_name": event, "timestamp": timestamp,
			"sender": map[string]string{"id": sender}, "recipient": map[string]string{"id": recipient}, "message": map[string]string{"msg_id": id, "text": "hi"}}
		if strings.Contains(event, "group") {
			envelope["oa_id"] = d.oaA
		}
		body, _ := json.Marshal(envelope)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/channels/zalo/webhook", bytes.NewReader(body))
		req.Header.Set(zalo.SignatureHeader, "mac="+zalo.Sign("700001", body, timestamp, "oa-secret-a"))
		if response := serve(api, req); response.Code != http.StatusOK {
			t.Fatalf("delivery %s: %d", id, response.Code)
		}
		if _, err := journal.ProcessDue(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	name := func(user string) string {
		return queryString(t, pool, `SELECT COALESCE((SELECT name FROM zalo_users WHERE user_id=$1),'-')`, user)
	}
	// A refused token: the next message asks again.
	token = "rotated"
	send("user_send_text", "1110001", d.oaA, "r-1")
	token = "access-a"
	send("user_send_text", "1110001", d.oaA, "r-2")
	if got := name("1110001"); got != "Minh Anh" {
		t.Fatalf("after a refused token = %s", got)
	}
	// Another answer waits its hour.
	send("user_send_text", "1110008", d.oaA, "r-3")
	before := len(calls)
	send("user_send_text", "1110008", d.oaA, "r-4")
	if len(calls) != before {
		t.Fatalf("asked again within the hour: %v", calls[before:])
	}
	// One read of a group names every member: the second asks Zalo nothing.
	send("user_send_group_text", "1110002", "g-7", "r-5")
	if got := name("1110002") + "," + name("1110009"); got != "Bao,Lan" {
		t.Fatalf("group names = %s", got)
	}
	before = len(calls)
	send("user_send_group_text", "1110009", "g-7", "r-6")
	if len(calls) != before {
		t.Fatalf("a member read with the list was asked about: %v", calls[before:])
	}
	if got := queryString(t, pool, `SELECT COALESCE(sender_name,'') FROM channel_thread_items WHERE external_id='r-6'`); got != "Lan" {
		t.Fatalf("the second member's item = %q", got)
	}
	// In a group, a profile Zalo answers otherwise, or answers with no name, sends us to the member list.
	send("user_send_group_text", "1110011", "g-8", "r-8")
	send("user_send_group_text", "1110012", "g-9", "r-9")
	if got := name("1110011") + "," + name("1110012"); got != "Khoa,Vy" {
		t.Fatalf("named from the member list = %s", got)
	}
	// The race: the name was kept after the worker read the names and before it made the contact.
	if _, err := pool.Exec(context.Background(), `INSERT INTO zalo_users(workspace_id,channel_id,user_id,name,looked_up_at)
		SELECT workspace_id,id,'1110010','Thu',now() FROM channels WHERE id=$1`, d.channelA); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `WITH c AS (INSERT INTO contacts(workspace_id,name) SELECT workspace_id,'1110010' FROM channels WHERE id=$1 RETURNING id,workspace_id)
		INSERT INTO contact_identities(workspace_id,contact_id,channel_id,kind,value) SELECT c.workspace_id,c.id,$1,'zalo_user','1110010' FROM c`, d.channelA); err != nil {
		t.Fatal(err)
	}
	send("user_send_text", "1110010", d.oaA, "r-7")
	if got := queryString(t, pool, `SELECT ct.name FROM contact_identities i JOIN contacts ct ON ct.id=i.contact_id WHERE i.value='1110010'`); got != "Thu" {
		t.Fatalf("a contact made before its name = %s", got)
	}
	// Removed, the channel forgets them.
	if response := request(t, api, http.MethodDelete, "/api/v1/channels/"+d.channelA, nil, d.adminA, nil); response.Code != http.StatusNoContent {
		t.Fatalf("remove: %d %s", response.Code, response.Body.String())
	}
	if got := queryString(t, pool, `SELECT count(*)::text FROM zalo_users WHERE channel_id=$1`, d.channelA); got != "0" {
		t.Fatalf("names kept after removal = %s", got)
	}
}

// A Zalo channel's processing is paused and resumed through the API, and says so; a channel that takes each
// message as it comes cannot be paused, and another workspace cannot pause this one.
func TestAZaloChannelsProcessingIsPausedAndResumedThroughTheAPI(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		keys, _ := tokenseal.ParseKeys("k1:AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM=")
		config.Google = google.Config{Keys: keys}
	})
	d := zaloDesk{api: api, pool: pool, oaA: fmt.Sprint(600000 + time.Now().UnixNano()%100000)}
	d.adminA = createSession(t, api, service, "Zalo Pause", "admin@zalo-pause.example.com")
	other := createSession(t, api, service, "Zalo Pause Other", "admin@zalo-pause-other.example.com")
	d.channelA = d.connect(t, d.adminA, d.oaA, "oa-secret-a")
	patch := func(session, id string, paused bool) (int, map[string]any) {
		response := request(t, api, http.MethodPatch, "/api/v1/channels/"+id, map[string]any{"processing_paused": paused}, session, nil)
		var body map[string]any
		_ = json.Unmarshal(response.Body.Bytes(), &body)
		return response.Code, body
	}
	if code, body := patch(d.adminA, d.channelA, true); code != http.StatusOK || body["processing_paused_at"] == nil {
		t.Fatalf("pause: %d %v", code, body)
	}
	// What arrives while it is paused is counted as kept.
	for _, id := range []string{"pz-1", "pz-2"} {
		timestamp := fmt.Sprint(time.Now().UnixMilli())
		body, _ := json.Marshal(map[string]any{"app_id": "700001", "event_name": "user_send_text", "timestamp": timestamp,
			"sender": map[string]string{"id": "1110001"}, "recipient": map[string]string{"id": d.oaA}, "message": map[string]string{"msg_id": id, "text": "hi"}})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/channels/zalo/webhook", bytes.NewReader(body))
		req.Header.Set(zalo.SignatureHeader, "mac="+zalo.Sign("700001", body, timestamp, "oa-secret-a"))
		if response := serve(api, req); response.Code != http.StatusOK {
			t.Fatalf("delivery %s: %d", id, response.Code)
		}
	}
	for _, path := range []string{"/api/v1/channels/" + d.channelA, "/api/v1/channels"} {
		response := request(t, api, http.MethodGet, path, nil, d.adminA, nil)
		if !strings.Contains(response.Body.String(), `"paused_count":2`) {
			t.Fatalf("%s while paused: %s", path, response.Body.String())
		}
	}
	if code, _ := patch(other, d.channelA, false); code != http.StatusNotFound {
		t.Fatalf("another workspace resumed it: %d", code)
	}
	if code, body := patch(d.adminA, d.channelA, false); code != http.StatusOK || body["processing_paused_at"] != nil || body["paused_count"] != float64(0) {
		t.Fatalf("resume: %d %v", code, body)
	}
	email := create(t, api, d.adminA, "/api/v1/channels", map[string]any{"type": "email", "config": map[string]any{"support_address": "help@zalo-pause.example.com"}})
	if code, body := patch(d.adminA, email, true); code != http.StatusBadRequest {
		t.Fatalf("an email channel paused: %d %v", code, body)
	}
	if got := queryString(t, pool, `SELECT string_agg(kind,',' ORDER BY seq) FROM channel_events WHERE channel_id=$1 AND kind IN ('paused','resumed')`, d.channelA); got != "paused,resumed" {
		t.Fatalf("history = %s", got)
	}
}
