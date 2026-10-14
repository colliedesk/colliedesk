package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

const testOrigin = "https://desk.example.com"

func testAPI(t *testing.T) (http.Handler, *auth.Service) {
	t.Helper()
	api, service, _ := testAPIWithPool(t)
	return api, service
}

func testAPIWithPool(t *testing.T) (http.Handler, *auth.Service, *pgxpool.Pool) {
	t.Helper()
	api, service, pool, _ := testAPIWithStorage(t)
	return api, service, pool
}

func testAPIWithStorage(t *testing.T) (http.Handler, *auth.Service, *pgxpool.Pool, attachments.Storage) {
	t.Helper()
	pool := testPool(t)
	if err := database.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	storage, err := attachments.NewLocalDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	authService := auth.New(pool, testOrigin)
	return httpapi.New(pool, httpapi.Config{Origin: testOrigin, Attachments: storage}), authService, pool, storage
}

func seedAttachment(t *testing.T, pool *pgxpool.Pool, storage attachments.Storage, token string, api http.Handler, messageID, filename, content string) string {
	t.Helper()
	var me struct {
		Workspace struct {
			ID string `json:"id"`
		} `json:"workspace"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/auth/me", nil, token, nil), &me)
	object, err := storage.Put(context.Background(), me.Workspace.ID, strings.NewReader(content), int64(len(content)))
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.New(pool).CreateAttachment(context.Background(), me.Workspace.ID, store.NewAttachment{
		MessageID: messageID, Filename: filename, ContentType: "text/plain", Object: object,
	})
	if err != nil {
		t.Fatal(err)
	}
	return item.ID
}

func createSession(t *testing.T, api http.Handler, service *auth.Service, workspace, email string) string {
	t.Helper()
	_, pairingURL, err := service.CreateWorkspace(context.Background(), workspace, email)
	if err != nil {
		t.Fatal(err)
	}
	response := request(t, api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, pairingURL)}, "", nil)
	var body struct {
		Token string `json:"token"`
	}
	decode(t, response, &body)
	return body.Token
}

func create(t *testing.T, api http.Handler, token, path string, body any) string {
	t.Helper()
	response := request(t, api, http.MethodPost, path, body, token, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("POST %s status=%d body=%s", path, response.Code, response.Body.String())
	}
	var result struct {
		ID string `json:"id"`
	}
	decode(t, response, &result)
	return result.ID
}

func request(t *testing.T, handler http.Handler, method, path string, body any, token string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	return serve(handler, newRequest(t, method, path, body, token, cookie))
}

func newRequest(t *testing.T, method, path string, body any, token string, cookie *http.Cookie) *http.Request {
	t.Helper()
	var data bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&data).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &data)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	return req
}

func serve(handler http.Handler, request *http.Request) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func requestWithOrigin(t *testing.T, handler http.Handler, method, path string, cookie *http.Cookie, origin string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.AddCookie(cookie)
	req.Header.Set("Origin", origin)
	return serve(handler, req)
}

func decode(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
		t.Fatalf("decode response %q: %v", response.Body.String(), err)
	}
}

func linkToken(t *testing.T, raw string) string {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed.Query().Get("token")
}

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is unset; skipping real PostgreSQL API test")
	}
	schema := "test_" + regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(t.Name()), "_") + fmt.Sprint(time.Now().UnixNano())
	admin, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(context.Background(), "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
	})
	return pool
}
