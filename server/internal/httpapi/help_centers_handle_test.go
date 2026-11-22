package httpapi_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// The database refuses, on its own, a help center handle the site would read as one of its routes or
// as a language, so no writer that skips the domain check can shadow a page of the primary.
func TestTheDatabaseRefusesAHelpCenterHandleThatIsARouteOrALanguage(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "Handle Desk", "admin@handle-desk.example.com")
	workspace := workspaceID(t, api, admin)
	enableHelpCenter(t, api, admin, "Handle answers", true)
	insert := func(handle string) error {
		_, err := pool.Exec(context.Background(), `INSERT INTO help_centers
			(workspace_id, handle, primary_help_center, enabled, title, locale) VALUES ($1, $2, false, true, 'Other', 'en')`,
			workspace, handle)
		return err
	}
	for _, handle := range []string{"a", "b", "c", "search", "images", "media", "files", "support", "chat",
		"vi", "pt-br", "fra", "zh-hant"} {
		var pgErr *pgconn.PgError
		if err := insert(handle); !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Errorf("insert handle %q: %v, want a check violation (23514)", handle, err)
		}
	}
	if err := insert("hardware2"); err != nil {
		t.Errorf("insert handle hardware2: %v, want it stored", err)
	}
}

// The channel types serve their routes below the primary help center's root (the help chat at
// /chat), so the first segment of each is refused as a handle, or a help center could take it away.
func TestEveryChannelRouteBelowTheHelpCenterIsRefusedAsAHandle(t *testing.T) {
	api, _ := testAPI(t)
	routes := api.(*httpapi.Server).HelpCenterRoutes()
	if len(routes) == 0 {
		t.Fatal("no channel type serves a route below the help center; the help chat should")
	}
	for _, route := range routes {
		_, path, _ := strings.Cut(route, " ")
		segment, _, _ := strings.Cut(strings.TrimPrefix(path, "/"), "/")
		if domain.CheckHelpCenterHandle(segment) == nil {
			t.Errorf("%s: %q is accepted as a help center handle; reserve it in domain.CheckHelpCenterHandle", route, segment)
		}
	}
}
