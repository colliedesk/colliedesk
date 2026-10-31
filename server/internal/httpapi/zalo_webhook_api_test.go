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

	"github.com/colliedesk/colliedesk/server/internal/channels/zalo"
	"github.com/colliedesk/colliedesk/server/internal/google"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
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
