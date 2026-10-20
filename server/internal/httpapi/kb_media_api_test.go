package httpapi_test

import (
	"bytes"
	"context"
	"image"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

type kbMediaView struct {
	ID           string     `json:"id"`
	Kind         string     `json:"kind"`
	ContentType  string     `json:"content_type"`
	Size         int64      `json:"size"`
	URL          string     `json:"url"`
	Name         string     `json:"name"`
	Alt          string     `json:"alt"`
	Width        *int       `json:"width"`
	Height       *int       `json:"height"`
	ThumbnailURL *string    `json:"thumbnail_url"`
	PosterURL    *string    `json:"poster_url"`
	DurationMS   *int64     `json:"duration_ms"`
	Codecs       []string   `json:"codecs"`
	Kept         bool       `json:"kept"`
	UpdatedAt    string     `json:"updated_at"`
	PurgeAt      *string    `json:"purge_at"`
	Usage        *kbUsage   `json:"usage"`
	Articles     []struct { // details only
		ID      string `json:"id"`
		InTrash bool   `json:"in_trash"`
		Current bool   `json:"current"`
	} `json:"articles"`
}

type kbUsage struct {
	Articles        int `json:"articles"`
	TrashedArticles int `json:"trashed_articles"`
	OlderVersions   int `json:"older_versions"`
}

type kbMediaPage struct {
	Items      []kbMediaView `json:"items"`
	NextCursor *string       `json:"next_cursor"`
	Summary    *struct {
		Count     int   `json:"count"`
		Bytes     int64 `json:"bytes"`
		KeptCount int   `json:"kept_count"`
		KeptBytes int64 `json:"kept_bytes"`
	} `json:"summary"`
}

// uploadKBMedia uploads an image with the library's query (name, alt, keep) and checks the contract.
func uploadKBMedia(t *testing.T, api http.Handler, token string, data []byte, query url.Values) kbMediaView {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kb/images?"+query.Encode(), bytes.NewReader(data))
	req.Header.Set("Content-Type", "image/jpeg")
	req.Header.Set("Authorization", "Bearer "+token)
	validateContractRequest(t, req)
	response := serve(api, req)
	if response.Code != http.StatusCreated {
		t.Fatalf("upload status=%d body=%s", response.Code, response.Body.String())
	}
	validateContractResponse(t, req, response)
	var media kbMediaView
	decode(t, response, &media)
	return media
}

// errorMessage is the code and message of an error answer, for comparing two refusals.
func errorMessage(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	decode(t, response, &body)
	return body.Error.Code + ": " + body.Error.Message
}

func kbMediaList(t *testing.T, api http.Handler, token, query string) kbMediaPage {
	t.Helper()
	var page kbMediaPage
	kbCall(t, api, http.MethodGet, "/api/v1/kb/media?"+query, nil, token, http.StatusOK, &page)
	return page
}

func mediaTestdata(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("../media/testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// A phone photo loses its location and camera data before it is stored, keeps its orientation, and
// gets a thumbnail turned the way it is shown; the library item carries the name and alt the upload
// gave, and an upload made in the library is kept.
func TestKBImageUploadRemovesMetadataAndMakesAThumbnail(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Media Upload", "admin@kb-media-upload.example.com")
	photo := mediaTestdata(t, "photo-gps.jpg")
	media := uploadKBMedia(t, api, admin, photo, url.Values{"name": {"Phone photo"}, "alt": {"Red over blue"}, "keep": {"true"}})
	if media.Kind != "image" || media.ContentType != "image/jpeg" || media.Name != "Phone photo" || media.Alt != "Red over blue" || !media.Kept {
		t.Fatalf("media = %+v", media)
	}
	if media.Width == nil || media.Height == nil || *media.Width != 16 || *media.Height != 32 {
		t.Fatalf("size = %v×%v, want 16×32 (turned by its orientation)", media.Width, media.Height)
	}
	served := request(t, api, http.MethodGet, media.URL, nil, admin, nil)
	if served.Code != http.StatusOK || bytes.Contains(served.Body.Bytes(), []byte("SECRET")) || int64(served.Body.Len()) != media.Size || int64(len(photo)) <= media.Size {
		t.Fatalf("stored image status=%d size=%d (upload %d) still holds metadata: %v", served.Code, served.Body.Len(), len(photo), bytes.Contains(served.Body.Bytes(), []byte("SECRET")))
	}
	if media.ThumbnailURL == nil {
		t.Fatal("no thumbnail")
	}
	thumb := kbCall(t, api, http.MethodGet, *media.ThumbnailURL, nil, admin, http.StatusOK, nil)
	picture, format, err := image.Decode(bytes.NewReader(thumb.Body.Bytes()))
	if err != nil || format != "jpeg" || picture.Bounds().Dx() != 16 || picture.Bounds().Dy() != 32 {
		t.Fatalf("thumbnail %s %v (%v)", format, picture.Bounds(), err)
	}
	if thumb.Header().Get("Cache-Control") != "private, max-age=31536000, immutable" || thumb.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("thumbnail headers = %v", thumb.Header())
	}
	// The same photo again is the same item: the stored bytes are compared after the metadata left.
	// Its name stays; a name or alt the item lacks is filled from the new upload.
	if again := uploadKBMedia(t, api, admin, photo, url.Values{"name": {"Other"}}); again.ID != media.ID || again.Name != "Phone photo" {
		t.Fatalf("same photo gave %s named %q, want %s named Phone photo", again.ID, again.Name, media.ID)
	}
	pasted := uploadKBMedia(t, api, admin, pngImage(30), nil)
	if again := uploadKBMedia(t, api, admin, pngImage(30), url.Values{"alt": {"A red pixel"}}); again.ID != pasted.ID || again.Alt != "A red pixel" {
		t.Fatalf("same image with an alt text = %+v", again)
	}
	for _, refused := range [][]byte{[]byte("<svg xmlns='http://www.w3.org/2000/svg'/>"), photo[:200]} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/kb/images", bytes.NewReader(refused))
		req.Header.Set("Authorization", "Bearer "+admin)
		if response := serve(api, req); response.Code != http.StatusBadRequest {
			t.Fatalf("refused upload status=%d body=%s", response.Code, response.Body.String())
		}
	}
}

// "In use" follows the current text of live articles and translations; an article in the trash and an
// older version are counted apart, and the unused page adds up what deleting would free.
func TestKBMediaLibraryCountsUseFromCurrentTextsOnly(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Media Usage", "admin@kb-media-usage.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	used := uploadKBMedia(t, api, admin, pngImage(1), url.Values{"name": {"Used"}})
	dropped := uploadKBMedia(t, api, admin, pngImage(2), url.Values{"name": {"Dropped"}, "alt": {"x"}})
	trashed := uploadKBMedia(t, api, admin, pngImage(3), url.Values{"name": {"Trashed"}, "alt": {"x"}})
	loose := uploadKBMedia(t, api, admin, pngImage(4), url.Values{"keep": {"true"}})
	pasted := uploadKBMedia(t, api, admin, pngImage(5), nil)

	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Live", "body_md": "![a](" + used.URL + ") ![b](" + dropped.URL + ")"})
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+article.ID, map[string]any{"updated_at": article.UpdatedAt, "body_md": "![a](" + used.URL + ")"}, admin, http.StatusOK, nil)
	gone := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Gone", "body_md": "![c](" + trashed.URL + ")"})
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/articles/"+gone.ID+"?updated_at="+url.QueryEscape(gone.UpdatedAt), nil, admin, http.StatusNoContent, nil)

	usage := map[string]kbMediaView{}
	for _, item := range kbMediaList(t, api, admin, "").Items {
		usage[item.ID] = item
	}
	for _, check := range []struct {
		media kbMediaView
		want  kbUsage
	}{{used, kbUsage{Articles: 1}}, {dropped, kbUsage{OlderVersions: 1}}, {trashed, kbUsage{TrashedArticles: 1}}, {loose, kbUsage{}}} {
		if got := usage[check.media.ID]; got.Usage == nil || *got.Usage != check.want {
			t.Errorf("%s usage = %+v, want %+v", check.media.Name, got.Usage, check.want)
		}
	}
	if usage[pasted.ID].PurgeAt == nil || usage[loose.ID].PurgeAt != nil || usage[dropped.ID].PurgeAt != nil {
		t.Errorf("purge_at: pasted %v, kept %v, older version %v; want only the pasted one", usage[pasted.ID].PurgeAt, usage[loose.ID].PurgeAt, usage[dropped.ID].PurgeAt)
	}

	unused := kbMediaList(t, api, admin, "usage=unused&sort=largest")
	if len(unused.Items) != 4 || unused.Summary == nil || unused.Summary.Count != 3 || unused.Summary.KeptCount != 1 ||
		unused.Summary.Bytes != dropped.Size+trashed.Size+pasted.Size || unused.Summary.KeptBytes != loose.Size {
		t.Fatalf("unused = %d items, summary %+v", len(unused.Items), unused.Summary)
	}
	for i := 1; i < len(unused.Items); i++ {
		if unused.Items[i-1].Size < unused.Items[i].Size {
			t.Fatalf("largest first: %d before %d", unused.Items[i-1].Size, unused.Items[i].Size)
		}
	}
	if inUse := kbMediaList(t, api, admin, "usage=in_use"); len(inUse.Items) != 1 || inUse.Items[0].ID != used.ID || inUse.Summary != nil {
		t.Fatalf("in use = %+v", inUse.Items)
	}
	if missing := kbMediaList(t, api, admin, "usage=missing_alt&q=use"); len(missing.Items) != 1 || missing.Items[0].ID != used.ID {
		t.Fatalf("missing alt named like use = %+v", missing.Items)
	}
	// Pages of one follow each other with no gap and no repeat, in every sort.
	for _, sort := range []string{"newest", "oldest", "name", "largest"} {
		seen := map[string]bool{}
		cursor := ""
		for range 10 {
			page := kbMediaList(t, api, admin, "limit=2&sort="+sort+"&cursor="+url.QueryEscape(cursor))
			for _, item := range page.Items {
				if seen[item.ID] {
					t.Fatalf("%s: %s twice", sort, item.ID)
				}
				seen[item.ID] = true
			}
			if page.NextCursor == nil {
				break
			}
			cursor = *page.NextCursor
		}
		if len(seen) != 5 {
			t.Fatalf("%s pages listed %d of 5", sort, len(seen))
		}
	}
	if response := request(t, api, http.MethodGet, "/api/v1/kb/media?cursor=junk", nil, admin, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("bad cursor status=%d", response.Code)
	}

	var details kbMediaView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/media/"+trashed.ID+"/details", nil, admin, http.StatusOK, &details)
	if len(details.Articles) != 1 || details.Articles[0].ID != gone.ID || !details.Articles[0].InTrash || !details.Articles[0].Current {
		t.Fatalf("details articles = %+v", details.Articles)
	}
}

// Naming or describing an item keeps it; a stale updated_at changes nothing.
func TestKBMediaUpdateKeepsWhatAPersonNamed(t *testing.T) {
	api, service, pool, storage := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Media Update", "admin@kb-media-update.example.com")
	media := uploadKBMedia(t, api, admin, pngImage(7), nil)
	if media.Kept {
		t.Fatal("a pasted image is kept")
	}
	var named kbMediaView
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/media/"+media.ID, map[string]any{"updated_at": media.UpdatedAt, "name": " Logo ", "alt": "Acme logo"}, admin, http.StatusOK, &named)
	if named.Name != "Logo" || named.Alt != "Acme logo" || !named.Kept || named.Usage == nil {
		t.Fatalf("named = %+v", named)
	}
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/media/"+media.ID, map[string]any{"updated_at": media.UpdatedAt, "name": "Old"}, admin, http.StatusConflict, nil)
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/media/"+media.ID, map[string]any{"updated_at": named.UpdatedAt}, admin, http.StatusBadRequest, nil)
	if response := request(t, api, http.MethodPatch, "/api/v1/kb/media/"+media.ID, map[string]any{"updated_at": named.UpdatedAt, "alt": strings.Repeat("é", 501)}, admin, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("long alt status=%d", response.Code)
	}
	var unkept kbMediaView
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/media/"+media.ID, map[string]any{"updated_at": named.UpdatedAt, "kept": false}, admin, http.StatusOK, &unkept)
	if unkept.Kept || unkept.Name != "Logo" {
		t.Fatalf("unkept = %+v", unkept)
	}

	// The purge deletes an unkept, unused item and its thumbnail file, and never a kept one.
	kept := uploadKBMedia(t, api, admin, pngImage(8), url.Values{"keep": {"true"}})
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE kb_media SET last_uploaded_at=now()-interval '31 days'`); err != nil {
		t.Fatal(err)
	}
	var thumbKey string
	if err := pool.QueryRow(ctx, `SELECT thumb_storage_key FROM kb_media WHERE id=$1`, media.ID).Scan(&thumbKey); err != nil {
		t.Fatal(err)
	}
	purged, err := store.New(pool).WithObjects(storage).PurgeKBTrash(ctx)
	if err != nil || purged.Images != 1 {
		t.Fatalf("purged = %+v (%v), want one image", purged, err)
	}
	if body, err := storage.Open(ctx, thumbKey); err == nil {
		body.Close()
		t.Fatal("the purged image's thumbnail is still stored")
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/media/"+kept.ID+"/details", nil, admin, http.StatusOK, nil)
	kbCall(t, api, http.MethodGet, "/api/v1/kb/media/"+media.ID+"/details", nil, admin, http.StatusNotFound, nil)
}

// Images stored before the library (and imported ones) get their size and thumbnail from the backfill.
func TestKBMediaBackfillReadsOlderImages(t *testing.T) {
	api, service, pool, storage := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Media Backfill", "admin@kb-media-backfill.example.com")
	media := uploadKBMedia(t, api, admin, pngImage(9), nil)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE kb_media SET width=NULL, height=NULL, thumb_storage_key=NULL, thumb_size=NULL, inspected_at=NULL WHERE id=$1`, media.ID); err != nil {
		t.Fatal(err)
	}
	data := store.New(pool).WithObjects(storage)
	if done, err := data.BackfillKBMedia(ctx, store.KBBackfillBatch); err != nil || done != 1 {
		t.Fatalf("backfill = %d (%v), want 1", done, err)
	}
	var after kbMediaView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/media/"+media.ID+"/details", nil, admin, http.StatusOK, &after)
	if after.Width == nil || *after.Width != 2 || after.ThumbnailURL == nil {
		t.Fatalf("after backfill = %+v", after)
	}
	kbCall(t, api, http.MethodGet, *after.ThumbnailURL, nil, admin, http.StatusOK, nil)
	if done, err := data.BackfillKBMedia(ctx, store.KBBackfillBatch); err != nil || done != 0 {
		t.Fatalf("second backfill = %d (%v), want 0", done, err)
	}
}

// Another workspace's item answers 404 exactly like an id that does not exist, and its list never
// shows one.
func TestKBMediaNeverCrossesWorkspaces(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	alpha := createSession(t, api, service, "KB Media Alpha", "admin@kb-media-alpha.example.com")
	beta := createSession(t, api, service, "KB Media Beta", "admin@kb-media-beta.example.com")
	media := uploadKBMedia(t, api, alpha, pngImage(10), url.Values{"name": {"Alpha secret"}})
	uploadKBMedia(t, api, beta, pngImage(11), url.Values{"name": {"Beta own"}})
	missing := "00000000-0000-4000-8000-000000000000"
	for _, call := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/v1/kb/media/%s/details", nil},
		{http.MethodGet, "/api/v1/kb/media/%s/thumbnail", nil},
		{http.MethodPatch, "/api/v1/kb/media/%s", map[string]any{"updated_at": media.UpdatedAt, "name": "Taken"}},
	} {
		theirs := request(t, api, call.method, strings.Replace(call.path, "%s", media.ID, 1), call.body, beta, nil)
		nobody := request(t, api, call.method, strings.Replace(call.path, "%s", missing, 1), call.body, beta, nil)
		if theirs.Code != http.StatusNotFound || nobody.Code != http.StatusNotFound || errorMessage(t, theirs) != errorMessage(t, nobody) {
			t.Errorf("beta %s %s: %d %s, missing id %d %s", call.method, call.path, theirs.Code, theirs.Body.String(), nobody.Code, nobody.Body.String())
		}
	}
	for _, query := range []string{"", "q=secret", "usage=unused"} {
		page := kbMediaList(t, api, beta, query)
		for _, item := range page.Items {
			if item.ID == media.ID {
				t.Fatalf("beta list %q shows alpha's item", query)
			}
		}
		if query == "usage=unused" && (page.Summary == nil || page.Summary.Count != 1) {
			t.Fatalf("beta unused summary = %+v", page.Summary)
		}
	}
	var own kbMediaView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/media/"+media.ID+"/details", nil, alpha, http.StatusOK, &own)
	if own.Name != "Alpha secret" {
		t.Fatalf("alpha details = %+v", own)
	}
}

// The library holds at most domain.MaxKBMedia items; the same bytes again still answer at the cap.
func TestKBMediaLibraryRefusesAnUploadOverTheCap(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Media Cap", "admin@kb-media-cap.example.com")
	first := uploadKBMedia(t, api, admin, pngImage(20), nil)
	if _, err := pool.Exec(context.Background(), `INSERT INTO kb_media(workspace_id,storage_key,sha256,size,content_type)
		SELECT workspace_id, storage_key, lpad(to_hex(n),64,'0'), 10, 'image/png' FROM kb_media, generate_series(1,$2::int) n WHERE id=$1`,
		first.ID, domain.MaxKBMedia-1); err != nil {
		t.Fatal(err)
	}
	if again := uploadKBMedia(t, api, admin, pngImage(20), nil); again.ID != first.ID {
		t.Fatal("the same bytes at the cap gave another item")
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kb/images", bytes.NewReader(pngImage(21)))
	req.Header.Set("Authorization", "Bearer "+admin)
	if response := serve(api, req); response.Code != http.StatusConflict || errorCode(t, response) != "kb_limit" {
		t.Fatalf("upload over the cap status=%d body=%s", response.Code, response.Body.String())
	}
}
