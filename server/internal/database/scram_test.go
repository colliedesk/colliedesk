package database

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RFC 7677 section 3 exchange for user "user", password "pencil".
const (
	rfcSalt        = "W22ZaJ0SNY7soEsUEjb6gQ=="
	rfcAuthMessage = "n=user,r=rOprNGfwEbeRWgbNEkqO," +
		"r=rOprNGfwEbeRWgbNEkqO%hvYDpWUa2RaTCAfuxFIlj)hNlF$k0,s=W22ZaJ0SNY7soEsUEjb6gQ==,i=4096," +
		"c=biws,r=rOprNGfwEbeRWgbNEkqO%hvYDpWUa2RaTCAfuxFIlj)hNlF$k0"
	rfcClientProof     = "dHzbZapWIk4jUhN+Ute9ytag9zjfMHgsqmmiz7AndVQ="
	rfcServerSignature = "6rriTRBi23WpRR/wtup+mMhUZUn/dB5nLTJRsjl95G4="
)

func TestSCRAMVerifierMatchesRFC7677(t *testing.T) {
	salt, _ := base64.StdEncoding.DecodeString(rfcSalt)
	verifier, err := scramVerifier("pencil", salt, 4096)
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`^SCRAM-SHA-256\$4096:` + regexp.QuoteMeta(rfcSalt) + `\$([A-Za-z0-9+/=]{44}):([A-Za-z0-9+/=]{44})$`).FindStringSubmatch(verifier)
	if match == nil {
		t.Fatalf("verifier %q is not in PostgreSQL's SCRAM-SHA-256 format", verifier)
	}
	storedKey, _ := base64.StdEncoding.DecodeString(match[1])
	serverKey, _ := base64.StdEncoding.DecodeString(match[2])

	signature := hmac.New(sha256.New, serverKey)
	signature.Write([]byte(rfcAuthMessage))
	if got := base64.StdEncoding.EncodeToString(signature.Sum(nil)); got != rfcServerSignature {
		t.Fatalf("server signature = %s, want the RFC's %s", got, rfcServerSignature)
	}
	// ClientKey = ClientProof XOR HMAC(StoredKey, AuthMessage), and StoredKey = SHA-256(ClientKey).
	proof, _ := base64.StdEncoding.DecodeString(rfcClientProof)
	clientSignature := hmac.New(sha256.New, storedKey)
	clientSignature.Write([]byte(rfcAuthMessage))
	clientKey := clientSignature.Sum(nil)
	for i := range clientKey {
		clientKey[i] ^= proof[i]
	}
	if sum := sha256.Sum256(clientKey); !hmac.Equal(sum[:], storedKey) {
		t.Fatal("stored key does not match the RFC's client proof")
	}
}

func TestSCRAMVerifierRejectsPasswordsThatNeedSASLprep(t *testing.T) {
	if _, err := scramVerifier("pässword", make([]byte, 16), 4096); err == nil {
		t.Fatal("non-ASCII password accepted; PostgreSQL would normalize it and the login would fail")
	}
}

type statementRecorder struct {
	mu   sync.Mutex
	seen []string
}

func (r *statementRecorder) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, data.SQL)
	for _, arg := range data.Args {
		r.seen = append(r.seen, fmt.Sprint(arg))
	}
	return ctx
}

func (r *statementRecorder) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// TestEnsureRequestLoginNeverSendsThePlaintextPassword records every statement and argument sent to
// PostgreSQL: a failing or logged CREATE ROLE must only ever show a SCRAM verifier.
func TestEnsureRequestLoginNeverSendsThePlaintextPassword(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	recorder := &statementRecorder{}
	config := pool.Config().Copy()
	config.ConnConfig.Tracer = recorder
	traced, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(traced.Close)

	user := fmt.Sprintf("colliedesk_test_canary_%d", time.Now().UnixNano())
	canary := fmt.Sprintf("canary%dpassword", time.Now().UnixNano())
	requestURL := fmt.Sprintf("postgres://%s:%s@%s:%d/%s", user, canary, config.ConnConfig.Host, config.ConnConfig.Port, config.ConnConfig.Database)
	if err := EnsureRequestLogin(ctx, traced, requestURL); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DROP ROLE `+user); err != nil {
			t.Errorf("drop role %s: %v", user, err)
		}
	})
	recorder.mu.Lock()
	for _, text := range recorder.seen {
		if strings.Contains(text, canary) {
			t.Errorf("plaintext password sent to PostgreSQL in: %.120s", text)
		}
	}
	recorder.mu.Unlock()

	login := pool.Config().Copy()
	login.ConnConfig.User, login.ConnConfig.Password = user, canary
	conn, err := pgx.ConnectConfig(ctx, login.ConnConfig)
	if err != nil {
		t.Fatalf("log in with the plaintext password: %v", err)
	}
	var method string
	err = conn.QueryRow(ctx, `SELECT left(rolpassword, 14) FROM pg_authid WHERE rolname=$1`, user).Scan(&method)
	_ = conn.Close(ctx)
	if err == nil {
		t.Fatalf("request login can read pg_authid (%s)", method)
	}
}
