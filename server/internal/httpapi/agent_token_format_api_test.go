package httpapi_test

import (
	"context"
	"crypto/sha256"
	"net/http"
	"strings"
	"testing"
)

// An image older than the scoped-token migration looks a bearer up by the plain SHA-256 of the raw
// value and ignores the tier. A token it cannot find answers 401 there, so a rollback never turns a
// read token into a full agent session.
func TestAgentTokenCannotBeFoundByThePlainHashAnOlderImageUses(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Token Format", "admin@format.example.com")
	issued := issueToken(t, api, admin, "Claude Code", "read")

	if !strings.HasPrefix(issued.Token, "cdk_agent_") {
		t.Fatalf("token %q lacks the cdk_agent_ prefix that secret scanners match", issued.Token)
	}
	plain := sha256.Sum256([]byte(issued.Token))
	var found int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM sessions WHERE token_hash=$1`, plain[:]).Scan(&found); err != nil {
		t.Fatal(err)
	}
	if found != 0 {
		t.Fatalf("%d session rows match the plain SHA-256 of the raw token: an older image would accept it", found)
	}

	sessionPlain := sha256.Sum256([]byte(admin))
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM sessions WHERE token_hash=$1`, sessionPlain[:]).Scan(&found); err != nil || found != 1 {
		t.Fatalf("a browser session keeps its plain SHA-256 hash: rows=%d err=%v", found, err)
	}
	if me := request(t, api, http.MethodGet, "/api/v1/auth/me", nil, admin, nil); me.Code != http.StatusOK {
		t.Fatalf("browser session status=%d, want 200", me.Code)
	}
}

func TestAgentTokenNeedsItsPrefixToAuthenticate(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Token Prefix", "admin@prefix.example.com")
	issued := issueToken(t, api, admin, "Claude Code", "send")

	if me := request(t, api, http.MethodGet, "/api/v1/auth/me", nil, issued.Token, nil); me.Code != http.StatusOK {
		t.Fatalf("token status=%d, want 200", me.Code)
	}
	bare := strings.TrimPrefix(issued.Token, "cdk_agent_")
	if bare == issued.Token {
		t.Fatalf("token %q has no prefix to remove", issued.Token)
	}
	if me := request(t, api, http.MethodGet, "/api/v1/auth/me", nil, bare, nil); me.Code != http.StatusUnauthorized {
		t.Fatalf("the random part without the prefix status=%d, want 401", me.Code)
	}
}
