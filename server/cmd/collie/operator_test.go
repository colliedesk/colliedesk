package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOperatorPairListRevokeAndTokens(t *testing.T) {
	databaseURL := cliDatabase(t)
	t.Setenv("MAILBOX_TOKEN_KEYS", cliTestRing)
	t.Setenv("OPERATOR_ORIGIN", "")
	t.Setenv("USER", "tests")
	runOperator := func(args ...string) (string, error) {
		var output strings.Builder
		err := run(context.Background(), append([]string{"operator"}, args...), &output, databaseURL, "https://desk.example.com")
		return output.String(), err
	}
	link, err := runOperator("pair", "--email", "Ops@Example.com")
	if err != nil || !strings.HasPrefix(link, "https://desk.example.com/operator/sign-in?token=") {
		t.Fatalf("pair = %q, %v", link, err)
	}
	t.Setenv("OPERATOR_ORIGIN", "https://ops.example.com")
	link, err = runOperator("pair", "--email", "ops@example.com")
	if err != nil || !strings.HasPrefix(link, "https://ops.example.com/sign-in?token=") {
		t.Fatalf("pair with OPERATOR_ORIGIN = %q, %v", link, err)
	}
	list, _ := runOperator("list")
	if !strings.Contains(list, "ops@example.com\tactive\tno authenticator") {
		t.Fatalf("list = %q", list)
	}
	previous := stderr
	stderr = io.Discard
	token, err := runOperator("token", "create", "--email", "ops@example.com", "--label", "nightly", "--permission", "operator_config:write")
	stderr = previous
	if err != nil || !strings.HasPrefix(token, "cdop_") {
		t.Fatalf("token = %q, %v", token, err)
	}
	sessions, _ := runOperator("sessions")
	if !strings.Contains(sessions, "token\tnightly\toperator:read,operator_config:write") || strings.Contains(sessions, "cdop_") {
		t.Fatalf("sessions = %q", sessions)
	}
	if _, err := runOperator("revoke", "ops@example.com"); err != nil {
		t.Fatal(err)
	}
	if sessions, _ := runOperator("sessions"); strings.TrimSpace(sessions) != "" {
		t.Fatalf("after revoke, sessions = %q", sessions)
	}
	if _, err := runOperator("revoke", "ops@example.com"); err == nil {
		t.Fatal("revoked twice")
	}
	// Pairing again lets a revoked operator back in.
	if _, err := runOperator("pair", "--email", "ops@example.com"); err != nil {
		t.Fatal(err)
	}
	if list, _ := runOperator("list"); !strings.Contains(list, "ops@example.com\tactive") {
		t.Fatalf("list after a new pair = %q", list)
	}
}

func TestOperatorOriginMustBeItsOwnHttpsHost(t *testing.T) {
	for value, ok := range map[string]bool{
		"":                          true,
		"https://ops.example.com":   true,
		"https://ops.example.com/":  true,
		"http://ops.localhost:8080": true,
		"http://ops.example.com":    false,
		"https://desk.example.com":  false,
		"https://ops.example.com/x": false,
		"ops.example.com":           false,
	} {
		t.Setenv("OPERATOR_ORIGIN", value)
		if _, err := operatorOrigin("https://desk.example.com"); (err == nil) != ok {
			t.Errorf("OPERATOR_ORIGIN=%q: err %v", value, err)
		}
	}
}

// With OPERATOR_ORIGIN each host serves only its own routes, before the help center can take a host.
func TestOperatorHostServesOnlyItsRoutes(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "inner") })
	handler := operatorHost("https://desk.example.com", "https://ops.example.com", inner)
	for _, c := range []struct {
		host, path string
		status     int
		code       string
	}{
		{"ops.example.com", "/api/v1/operator/auth/me", 200, ""},
		{"ops.example.com", "/mcp/operator", 200, ""},
		{"ops.example.com", "/api/v1/conversations", 404, "not_found"},
		{"ops.example.com", "/mcp", 404, "not_found"},
		{"desk.example.com", "/api/v1/operator/auth/me", 404, "operator_console_elsewhere"},
		{"desk.example.com", "/mcp/operator", 404, "operator_console_elsewhere"},
		{"desk.example.com", "/operator/sign-in", 404, "operator_console_elsewhere"},
		{"desk.example.com", "/api/v1/conversations", 200, ""},
		{"acme.help.example.com", "/", 200, ""},
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, c.path, nil)
		request.Host = c.host
		handler.ServeHTTP(recorder, request)
		if recorder.Code != c.status || (c.code != "" && !strings.Contains(recorder.Body.String(), c.code)) {
			t.Errorf("%s%s: %d %s", c.host, c.path, recorder.Code, recorder.Body.String())
		}
	}
	if same := operatorHost("https://desk.example.com", "", inner); same == nil {
		t.Fatal("no handler without OPERATOR_ORIGIN")
	}
}
