package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/colliedesk/colliedesk/server/internal/auth"
)

// The custom domains of the two help centers of twoHelpCenters.
const hostMain, hostHardware = "help.multi-main.example", "help.multi-hardware.example"

// twoHelpCenters is a workspace with two help centers over one knowledge base, both on, both in
// English and French: the primary ("Main answers", on hostMain) and "hardware" ("Hardware answers",
// on hostHardware). Main shelf is placed on the primary only, Hardware (with its section Cables) on
// hardware only, and Shared shelf on both.
type twoHelpCenters struct {
	api, site http.Handler
	pool      *pgxpool.Pool
	service   *auth.Service
	admin     string
	slug      string
	workspace string
	// hardware is the id of the second help center.
	hardware string
	// main is on the primary only, shared on both. cable, only on hardware, shows image and video,
	// lists file and has a published French translation, cableFR.
	main, shared, cable kbArticleView
	cableFR             kbTranslationView
	image               kbImageView
	video, file         kbMediaView
	clients             int
}

func newTwoHelpCenters(t *testing.T) *twoHelpCenters {
	t.Helper()
	api, service, pool, _ := testAPIWithStorage(t)
	f := &twoHelpCenters{api: api, site: helpCenterSite(api), pool: pool, service: service}
	f.admin = createSession(t, api, service, "Multi Desk", "admin@multi-desk.example.com")
	f.slug, f.workspace = workspaceSlug(t, api, f.admin), workspaceID(t, api, f.admin)
	enableHelpCenter(t, api, f.admin, "Main answers", true)
	kbCall(t, api, http.MethodPut, "/api/v1/help-center/languages", map[string]any{"languages": []map[string]any{{"locale": "fr", "visible": true}}},
		f.admin, http.StatusOK, nil)
	mainShelf := kbCollection(t, api, f.admin, map[string]any{"name": "Main shelf"})
	hardware := kbCollection(t, api, f.admin, map[string]any{"name": "Hardware"})
	cables := kbCollection(t, api, f.admin, map[string]any{"name": "Cables", "parent_id": hardware.ID})
	shared := kbCollection(t, api, f.admin, map[string]any{"name": "Shared shelf"})
	f.main = kbArticle(t, api, f.admin, map[string]any{"collection_id": mainShelf.ID, "title": "Reset the router", "status": "published", "body_md": "Hold it: bramble."})
	f.shared = kbArticle(t, api, f.admin, map[string]any{"collection_id": shared.ID, "title": "Warranty terms", "status": "published", "body_md": "Two years: thistle."})

	f.image = uploadKBImage(t, api, f.admin, otherPNG(61), http.StatusCreated)
	data := mediaVideo(t, "h264.mp4")
	f.video = uploadKBVideo(t, api, f.admin, data, len(data), nil)
	f.file = uploadKBFile(t, api, f.admin, "Cable chart.csv", []byte("pin,colour\n1,red\n"))
	draft := kbArticle(t, api, f.admin, map[string]any{"collection_id": cables.ID, "title": "Replace a cable",
		"body_md": "Unplug the copperline cable.\n\n![Cable](" + f.image.URL + ")\n\n<video src=\"" + f.video.URL + "\"></video>"})
	path := "/api/v1/kb/articles/" + draft.ID
	listed := putKBAttachments(t, api, f.admin, path, []string{f.file.ID}, http.StatusOK)
	kbCall(t, api, http.MethodPatch, path, map[string]any{"status": "published", "updated_at": listed.UpdatedAt}, f.admin, http.StatusOK, &f.cable)
	kbCall(t, api, http.MethodPost, path+"/translations", map[string]any{"locale": "fr", "title": "Changer un câble", "status": "published",
		"body_md": "Débranchez le câble cuivreline."}, f.admin, http.StatusCreated, &f.cableFR)

	ctx := context.Background()
	if err := pool.QueryRow(ctx, `INSERT INTO help_centers (workspace_id, handle, primary_help_center, enabled, title, locale)
		VALUES ($1, 'hardware', false, true, 'Hardware answers', 'en') RETURNING id::text`, f.workspace).Scan(&f.hardware); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		sql  string
		args []any
	}{
		{`DELETE FROM kb_collection_help_centers WHERE workspace_id=$1 AND collection_id=$2`, []any{f.workspace, hardware.ID}},
		{`INSERT INTO kb_collection_help_centers (workspace_id, collection_id, help_center_id, position)
			VALUES ($1, $3, $2, 0), ($1, $4, $2, 1)`, []any{f.workspace, f.hardware, hardware.ID, shared.ID}},
		{`INSERT INTO help_center_languages (workspace_id, help_center_id, locale, visible) VALUES ($1, $2, 'fr', true)`,
			[]any{f.workspace, f.hardware}},
		{`INSERT INTO help_center_domains (workspace_id, help_center_id, hostname, claim, token, status, verified_at) VALUES
			($1, primary_help_center_id($1), $3, left(md5($3), 24), md5($3), 'active', now()),
			($1, $2, $4, left(md5($4), 24), md5($4), 'active', now())`, []any{f.workspace, f.hardware, hostMain, hostHardware}},
	} {
		if _, err := pool.Exec(ctx, step.sql, step.args...); err != nil {
			t.Fatalf("%s: %v", step.sql, err)
		}
	}
	return f
}

// at loads path on host, each time from a new client address, so no visit spends another's allowance.
func (f *twoHelpCenters) at(host, path string) *httptest.ResponseRecorder {
	f.clients++
	return visit(f.site, path, fmt.Sprintf("198.51.100.%d", 10+f.clients), "Host", host)
}

// vote posts a "helpful" vote on the article page path of host, the way its own page sends it.
func (f *twoHelpCenters) vote(host, path string) *httptest.ResponseRecorder {
	f.clients++
	req := httptest.NewRequest(http.MethodPost, path+"/vote", strings.NewReader("helpful=yes"))
	req.RemoteAddr, req.Host = fmt.Sprintf("198.51.100.%d:4242", 10+f.clients), host
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://"+host)
	return serve(f.site, req)
}

// articlePath is the address of an article on its help center, in its own language.
func articlePath(article kbArticleView) string {
	return fmt.Sprintf("/a/%d-%s", article.Number, article.Slug)
}
