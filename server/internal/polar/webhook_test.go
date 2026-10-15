package polar_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/polar"
)

// The vectors below were computed outside this code base with Python's hmac module following the
// Standard Webhooks signed content "<id>.<timestamp>.<body>" (HMAC-SHA256, base64).
const (
	vectorBody      = `{"type":"subscription.updated","data":{"id":"sub_1"}}`
	vectorID        = "msg_2xQ8vLrT"
	vectorTimestamp = "1760500000"
	// Secret whose key is the base64 payload after "whsec_" (Standard Webhooks).
	standardSecret = "whsec_Y29sbGllZGVzay10ZXN0LXNpZ25pbmcta2V5LTMyYnk="
	standardSig    = "By+yE5olfIYz5GYtOF4oTEB++L91exEG3v2Wz4bcKys="
	// Secret whose key is the whole string as UTF-8 (Polar secrets issued before Standard Webhooks).
	legacySecret = "whsec_legacyPolarSecretValue"
	legacySig    = "S59W9EVHM/0/cvrZfD6+htN20n+KaGkji/OaYeBKdvc="
)

var vectorNow = time.Unix(1760500000, 0)

func headers(id, timestamp, signature string) http.Header {
	header := http.Header{}
	if id != "" {
		header.Set("webhook-id", id)
	}
	if timestamp != "" {
		header.Set("webhook-timestamp", timestamp)
	}
	if signature != "" {
		header.Set("webhook-signature", signature)
	}
	return header
}

func TestVerifyWebhook(t *testing.T) {
	cases := []struct {
		name    string
		secret  string
		header  http.Header
		body    string
		now     time.Time
		wantErr bool
	}{
		{"standard secret", standardSecret, headers(vectorID, vectorTimestamp, "v1,"+standardSig), vectorBody, vectorNow, false},
		{"legacy secret", legacySecret, headers(vectorID, vectorTimestamp, "v1,"+legacySig), vectorBody, vectorNow, false},
		{"second of several signatures", standardSecret, headers(vectorID, vectorTimestamp, "v1,AAAA v1,"+standardSig), vectorBody, vectorNow, false},
		{"within tolerance", standardSecret, headers(vectorID, vectorTimestamp, "v1,"+standardSig), vectorBody, vectorNow.Add(4 * time.Minute), false},
		{"tampered body", standardSecret, headers(vectorID, vectorTimestamp, "v1,"+standardSig), vectorBody + " ", vectorNow, true},
		{"other message id", standardSecret, headers("msg_other", vectorTimestamp, "v1,"+standardSig), vectorBody, vectorNow, true},
		{"wrong secret", legacySecret, headers(vectorID, vectorTimestamp, "v1,"+standardSig), vectorBody, vectorNow, true},
		{"stale timestamp", standardSecret, headers(vectorID, vectorTimestamp, "v1,"+standardSig), vectorBody, vectorNow.Add(6 * time.Minute), true},
		{"timestamp in the future", standardSecret, headers(vectorID, vectorTimestamp, "v1,"+standardSig), vectorBody, vectorNow.Add(-6 * time.Minute), true},
		{"missing signature", standardSecret, headers(vectorID, vectorTimestamp, ""), vectorBody, vectorNow, true},
		{"missing id", standardSecret, headers("", vectorTimestamp, "v1,"+standardSig), vectorBody, vectorNow, true},
		{"missing timestamp", standardSecret, headers(vectorID, "", "v1,"+standardSig), vectorBody, vectorNow, true},
		{"timestamp not a number", standardSecret, headers(vectorID, "yesterday", "v1,"+standardSig), vectorBody, vectorNow, true},
		{"unversioned signature", standardSecret, headers(vectorID, vectorTimestamp, standardSig), vectorBody, vectorNow, true},
		{"unknown version", standardSecret, headers(vectorID, vectorTimestamp, "v2,"+standardSig), vectorBody, vectorNow, true},
		{"empty secret", "", headers(vectorID, vectorTimestamp, "v1,"+standardSig), vectorBody, vectorNow, true},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			err := polar.VerifyWebhook(item.secret, item.header, []byte(item.body), item.now)
			if (err != nil) != item.wantErr {
				t.Fatalf("err=%v, wantErr=%v", err, item.wantErr)
			}
		})
	}
}
