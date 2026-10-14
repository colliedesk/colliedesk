package httpapi_test

import (
	"bytes"
	"context"
	"net/http"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestConcurrentDoubleRedeemExactlyOnce(t *testing.T) {
	api, service := testAPI(t)
	_, pairingURL, err := service.CreateWorkspace(context.Background(), "Concurrent Desk", "admin@concurrent.example.com")
	if err != nil {
		t.Fatal(err)
	}
	token := linkToken(t, pairingURL)
	start := make(chan struct{})
	statuses := make(chan int, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			statuses <- request(t, api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": token}, "", nil).Code
		}()
	}
	close(start)
	workers.Wait()
	close(statuses)
	got := make([]int, 0, 2)
	for status := range statuses {
		got = append(got, status)
	}
	sort.Ints(got)
	if got[0] != http.StatusOK || got[1] != http.StatusUnauthorized {
		t.Fatalf("concurrent redeem statuses=%v, want [200 401]", got)
	}
}

func TestLinkExpiryBoundaryAndHashOnlyPersistence(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	_, pairingURL, err := service.CreateWorkspace(context.Background(), "Hash Desk", "admin@hash.example.com")
	if err != nil {
		t.Fatal(err)
	}
	rawLinkToken := linkToken(t, pairingURL)
	var storedHash []byte
	if err := pool.QueryRow(context.Background(), `SELECT token_hash FROM pairing_links`).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if len(storedHash) != 32 || bytes.Contains(storedHash, []byte(rawLinkToken)) {
		t.Fatalf("link token storage is not a SHA-256 digest: length=%d", len(storedHash))
	}
	boundary := time.Now().UTC()
	if _, err := pool.Exec(context.Background(), `UPDATE pairing_links SET expires_at=$1`, boundary); err != nil {
		t.Fatal(err)
	}
	expired := request(t, api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": rawLinkToken}, "", nil)
	if expired.Code != http.StatusUnauthorized {
		t.Fatalf("expiry boundary status=%d, want 401", expired.Code)
	}

	if _, err := pool.Exec(context.Background(), `UPDATE pairing_links SET expires_at=now()+interval '15 minutes'`); err != nil {
		t.Fatal(err)
	}
	redeemed := request(t, api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": rawLinkToken}, "", nil)
	var session struct {
		Token string `json:"token"`
	}
	decode(t, redeemed, &session)
	if err := pool.QueryRow(context.Background(), `SELECT token_hash FROM sessions`).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if len(storedHash) != 32 || bytes.Contains(storedHash, []byte(session.Token)) {
		t.Fatalf("session token storage is not a SHA-256 digest: length=%d", len(storedHash))
	}
}
