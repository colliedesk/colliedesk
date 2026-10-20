package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// A published article plays a library video from the help center's own address, with the poster,
// width and height the library holds; nothing an author writes adds a poster, autoplay or another
// address, and a video no published text shows, in the trash or of another workspace, is not public.
func TestHelpCenterPlaysLibraryVideosOfPublishedArticles(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Help Video", "admin@help-video.example.com")
	other := createSession(t, api, service, "Help Video Other", "admin@help-video-other.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Video Answers", true)
	data := mediaVideo(t, "h264.mp4")
	video := uploadKBVideo(t, api, admin, data, len(data), nil)
	if response := serve(api, kbPosterRequest(video.ID, admin, pngImage(40))); response.Code != http.StatusOK {
		t.Fatalf("poster status=%d body=%s", response.Code, response.Body.String())
	}
	var item kbMediaView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/media/"+video.ID+"/details", nil, admin, http.StatusOK, &item)
	unshown := uploadKBVideo(t, api, admin, mediaVideo(t, "vp9.webm"), 1<<20, nil)
	foreign := uploadKBVideo(t, api, other, data, len(data), nil)
	image := uploadKBImage(t, api, admin, kbPNG, http.StatusCreated)

	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	body := `<video src="` + video.URL + `" poster="https://evil.example/p.png" autoplay muted loop title="Tour"></video>` + "\n\n" +
		`<video src="` + foreign.URL + `"></video>` + "\n\n" + `<video src="` + strings.Replace(video.URL, video.ID, image.ID, 1) + `"></video>`
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Product tour", "status": "published", "body_md": body})
	kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Draft", "body_md": `<video src="` + unshown.URL + `"></video>`})

	base := "/help/" + slug
	page := fmt.Sprintf("%s/a/%d-%s", base, article.Number, article.Slug)
	version := (*item.PosterURL)[len(*item.PosterURL)-8:]
	wantPage(t, visit(site, page, "192.0.2.20"), http.StatusOK, []string{
		`<video src="` + base + `/media/` + video.ID + `" title="Tour" preload="metadata" muted="" loop="" poster="` + base + `/media/` + video.ID + `/poster/` + version +
			fmt.Sprintf(`" width="%d" height="%d" controls=""></video>`, *item.Width, *item.Height),
	}, []string{"autoplay", "evil.example", "/api/v1/", foreign.ID, image.ID})

	played := visit(site, base+"/media/"+video.ID, "192.0.2.20", "Range", "bytes=0-99")
	if played.Code != http.StatusPartialContent || played.Body.Len() != 100 || played.Header().Get("Content-Type") != "video/mp4" ||
		played.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" || played.Header().Get("Content-Security-Policy") != "default-src 'none'; sandbox" ||
		played.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatalf("range status=%d len=%d headers=%v", played.Code, played.Body.Len(), played.Header())
	}
	if poster := visit(site, base+"/media/"+video.ID+"/poster/"+version, "192.0.2.20"); poster.Code != http.StatusOK || poster.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("poster status=%d headers=%v", poster.Code, poster.Header())
	}
	for _, path := range []string{
		base + "/media/" + video.ID + "/poster/00000000",
		base + "/media/" + unshown.ID,
		base + "/media/" + foreign.ID,
		base + "/media/" + image.ID,
		base + "/media/" + strings.ToUpper(video.ID) + "x",
		"/help/" + workspaceSlug(t, api, other) + "/media/" + video.ID,
	} {
		if response := visit(site, path, "192.0.2.20"); response.Code != http.StatusNotFound {
			t.Errorf("%s status=%d, want 404", path, response.Code)
		}
	}

	// A new poster is on the page at once, under a new address; the old one is gone.
	if response := serve(api, kbPosterRequest(video.ID, admin, pngImage(41))); response.Code != http.StatusOK {
		t.Fatalf("second poster status=%d", response.Code)
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/media/"+video.ID+"/details", nil, admin, http.StatusOK, &item)
	newVersion := (*item.PosterURL)[len(*item.PosterURL)-8:]
	wantPage(t, visit(site, page, "192.0.2.20"), http.StatusOK, []string{"/poster/" + newVersion}, []string{"/poster/" + version})
	if response := visit(site, base+"/media/"+video.ID+"/poster/"+version, "192.0.2.20"); response.Code != http.StatusNotFound {
		t.Fatalf("old poster status=%d, want 404", response.Code)
	}

	// In the trash, the video leaves the page and its address. (The trash refuses a video a published
	// article shows; this is the rule underneath.)
	if _, err := pool.Exec(context.Background(), `UPDATE kb_media SET deleted_at=now() WHERE id=$1`, video.ID); err != nil {
		t.Fatal(err)
	}
	kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Bump", "body_md": "x"})
	wantPage(t, visit(site, page, "192.0.2.20"), http.StatusOK, nil, []string{"<video", video.ID})
	if response := visit(site, base+"/media/"+video.ID, "192.0.2.20"); response.Code != http.StatusNotFound {
		t.Fatalf("trashed video status=%d, want 404", response.Code)
	}
}
