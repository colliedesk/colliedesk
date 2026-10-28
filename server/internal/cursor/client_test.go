package cursor_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/cursor"
)

const key = "crsr_canary-client-DO-NOT-PRINT-1a2b"

func TestCursorErrorsSayWhatCursorSaidButNeverTheKey(t *testing.T) {
	for _, item := range []struct {
		name, body string
		status     int
		code       string
	}{
		{"code and message", `{"code":"agent_id_conflict","message":"taken by ` + key + `"}`, 409, "agent_id_conflict"},
		{"error string", `{"error":"Unauthorized","message":"Invalid API key ` + key + `"}`, 401, ""},
		{"error object", `{"error":{"code":"repository_access","message":"no access with ` + key + `"}}`, 400, "repository_access"},
		{"not JSON", `<html>` + key + `</html>`, 502, ""},
	} {
		t.Run(item.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer "+key {
					t.Errorf("authorization: %q", r.Header.Get("Authorization"))
				}
				w.WriteHeader(item.status)
				_, _ = w.Write([]byte(item.body))
			}))
			defer server.Close()
			err := (cursor.Client{Base: server.URL, Key: key}).Me(context.Background())
			var cursorErr *cursor.Error
			if !errors.As(err, &cursorErr) || cursorErr.Status != item.status || cursorErr.Code != item.code {
				t.Fatalf("error: %#v", err)
			}
			if strings.Contains(err.Error(), key) {
				t.Fatalf("the error shows the key: %v", err)
			}
		})
	}
}

func TestTooManyRequestsCarriesRetryAfter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "42")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	err := (cursor.Client{Base: server.URL, Key: key}).Me(context.Background())
	var cursorErr *cursor.Error
	if !errors.As(err, &cursorErr) || cursorErr.RetryAfter != 42*time.Second {
		t.Fatalf("error: %#v", err)
	}
}
