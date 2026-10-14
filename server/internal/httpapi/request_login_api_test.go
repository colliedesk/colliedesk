package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestAPIServesRequestsThroughASeparateRequestLogin runs the request path as the hardened layout
// does: DATABASE_URL is the owner, DATABASE_REQUEST_URL a login that can only become the app role.
func TestAPIServesRequestsThroughASeparateRequestLogin(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	user := fmt.Sprintf("colliedesk_test_api_request_%d", time.Now().UnixNano())
	config := pool.Config().Copy()
	config.ConnConfig.User, config.ConnConfig.Password = user, "request-secret"
	requestURL := fmt.Sprintf("postgres://%s:%s@%s:%d/%s", user, config.ConnConfig.Password,
		config.ConnConfig.Host, config.ConnConfig.Port, config.ConnConfig.Database)
	if err := database.EnsureRequestLogin(ctx, pool, requestURL); err != nil {
		t.Fatal(err)
	}
	requestPool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		requestPool.Close()
		if _, err := pool.Exec(context.Background(), `DROP ROLE `+user); err != nil {
			t.Errorf("drop role %s: %v", user, err)
		}
	})

	api := httpapi.New(pool, httpapi.Config{Origin: testOrigin, RequestPool: requestPool})
	service := auth.New(pool, testOrigin)
	alpha := createSession(t, api, service, "Alpha Desk", "admin@alpha.example.com")
	beta := createSession(t, api, service, "Beta Desk", "admin@beta.example.com")
	contact := create(t, api, alpha, "/api/v1/contacts", map[string]any{"name": "Ada", "email": "ada@example.com"})
	conversation := create(t, api, alpha, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Printer"})

	var page struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/conversations", nil, alpha, nil), &page)
	if len(page.Items) != 1 || page.Items[0].ID != conversation {
		t.Fatalf("alpha inbox = %+v", page.Items)
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/conversations", nil, beta, nil), &page)
	if len(page.Items) != 0 {
		t.Fatalf("beta inbox shows alpha's conversations: %+v", page.Items)
	}
	if response := request(t, api, http.MethodGet, "/api/v1/conversations/"+conversation, nil, beta, nil); response.Code != http.StatusNotFound {
		t.Fatalf("beta reads alpha's conversation: status %d", response.Code)
	}
}
