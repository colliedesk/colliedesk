package polar

import (
	"strings"
	"testing"
)

// FuzzParseEvent checks that any webhook body either fails to parse or yields an event the
// billing code can act on: a type, and for subscription events the fields it keys on.
func FuzzParseEvent(f *testing.F) {
	f.Add([]byte(`{"type":"subscription.updated","timestamp":"2026-10-15T00:00:00Z","data":{"id":"sub_1","status":"active","product_id":"prod_1","customer":{"external_id":"ws_1"}}}`))
	f.Add([]byte(`{"type":"order.created","data":{}}`))
	f.Add([]byte(`{"type":"subscription.canceled","data":{"id":"s","status":"canceled","product_id":"p","modified_at":"2026-10-15T00:00:00Z","ended_at":null}}`))
	f.Fuzz(func(t *testing.T, body []byte) {
		event, err := ParseEvent(body)
		if err != nil {
			return
		}
		if event.Type == "" {
			t.Fatal("event without a type")
		}
		isSubscription := strings.HasPrefix(event.Type, "subscription.")
		if isSubscription != (event.Subscription != nil) {
			t.Fatalf("type %q with subscription %v", event.Type, event.Subscription)
		}
		if s := event.Subscription; s != nil {
			if s.ID == "" || s.Status == "" || s.ProductID == "" || s.ModifiedAt.IsZero() {
				t.Fatalf("incomplete subscription %+v", s)
			}
		}
	})
}
