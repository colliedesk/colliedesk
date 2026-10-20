package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

const inboundPath = "/api/v1/channels/email/inbound"

// mail joins header lines and a body into a CRLF RFC 5322 message.
func mail(headers []string, body string) string {
	return strings.Join(headers, "\r\n") + "\r\n\r\n" + strings.ReplaceAll(body, "\n", "\r\n")
}

func inboundRequest(t *testing.T, raw, secret string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, inboundPath, strings.NewReader(raw))
	req.Header.Set("Content-Type", "message/rfc822")
	if secret != "" {
		req.Header.Set("X-Colliedesk-Inbound-Secret", secret)
	}
	return req
}

func inbound(t *testing.T, api http.Handler, secret, raw string) *httptest.ResponseRecorder {
	t.Helper()
	return serve(api, inboundRequest(t, raw, secret))
}

type inboundResult struct {
	Status         string  `json:"status"`
	Reason         *string `json:"reason"`
	ConversationID *string `json:"conversation_id"`
	MessageID      *string `json:"message_id"`
	Authentication string  `json:"authentication"`
}

func inboundOK(t *testing.T, api http.Handler, secret, raw string) inboundResult {
	t.Helper()
	response := inbound(t, api, secret, raw)
	if response.Code != http.StatusOK {
		t.Fatalf("inbound status=%d body=%s", response.Code, response.Body.String())
	}
	var result inboundResult
	decode(t, response, &result)
	return result
}

// connectEmail configures the email channel for the admin's workspace and returns a fresh inbound secret.
func connectEmail(t *testing.T, api http.Handler, admin, address string) string {
	t.Helper()
	response := request(t, api, http.MethodPut, "/api/v1/channels/email", map[string]any{"support_address": address}, admin, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("PUT email channel status=%d body=%s", response.Code, response.Body.String())
	}
	return rotateSecret(t, api, admin)
}

func rotateSecret(t *testing.T, api http.Handler, admin string) string {
	t.Helper()
	response := request(t, api, http.MethodPost, "/api/v1/channels/email/secret", nil, admin, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("rotate secret status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Secret string `json:"inbound_secret"`
	}
	decode(t, response, &body)
	if body.Secret == "" {
		t.Fatal("rotate returned an empty secret")
	}
	return body.Secret
}

type timelineMessage struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Body     string `json:"body"`
	Delivery *struct {
		Status   string `json:"status"`
		Attempts int    `json:"attempts"`
	} `json:"delivery"`
	Email *struct {
		From           string `json:"from"`
		Authentication string `json:"authentication"`
		Attachments    []struct {
			Filename    string `json:"filename"`
			ContentType string `json:"content_type"`
		} `json:"attachments"`
	} `json:"email"`
}

func timeline(t *testing.T, api http.Handler, token, conversationID string) []timelineMessage {
	t.Helper()
	response := request(t, api, http.MethodGet, "/api/v1/conversations/"+conversationID+"/messages", nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("timeline status=%d body=%s", response.Code, response.Body.String())
	}
	var list struct {
		Items []timelineMessage `json:"items"`
	}
	decode(t, response, &list)
	return list.Items
}

type conversationSummary struct {
	ID      string `json:"id"`
	Subject string `json:"subject"`
	Status  string `json:"status"`
	Channel string `json:"channel"`
}

func conversationList(t *testing.T, api http.Handler, token string) []conversationSummary {
	t.Helper()
	response := request(t, api, http.MethodGet, "/api/v1/conversations", nil, token, nil)
	var page struct {
		Items []conversationSummary `json:"items"`
	}
	decode(t, response, &page)
	return page.Items
}

func queryString(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) string {
	t.Helper()
	var value string
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&value); err != nil {
		t.Fatalf("query %q: %v", sql, err)
	}
	return value
}
