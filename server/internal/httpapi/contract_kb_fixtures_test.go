package httpapi_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// contractKB is the knowledge base the contract cases act on: one row for each change a case makes.
type contractKB struct {
	collectionID, collectionSeen     string
	deleteCollectionID               string
	deleteCollectionSeen             string
	trashCollectionID                string
	trashCollectionSeen              string
	articleID, articleSeen           string
	deleteArticleID                  string
	deleteArticleSeen                string
	trashArticleID, trashArticleSeen string
	versionArticleID                 string
	versionArticleSeen               string
	imageID, imageSeen               string
	// Library items: one to move to the trash, one to delete, two already in the trash.
	trashMediaID, deleteMediaID  string
	restoreMediaID, purgeMediaID string
	// replaceMediaID is shown by an article; replacementID is a replacement made at seeding to undo.
	replaceMediaID, replaceWithID string
	replacementID                 string
	// A video with a poster, one to give a poster, an upload to send a part to (then cancel, in route
	// order) and one whose parts all arrived.
	videoID, posterPath, posterVideoID string
	partUploadID, completeUploadID     string
	cancelUploadID                     string
}

// contractVideoPart is the part the contract case sends: the whole of a tiny upload.
var contractVideoPart = []byte("ten bytes!")

func seedContractKB(t *testing.T, api http.Handler, admin string) contractKB {
	t.Helper()
	var kb contractKB
	collection := kbCollection(t, api, admin, map[string]any{"name": "Contract collection"})
	kb.collectionID, kb.collectionSeen = collection.ID, collection.UpdatedAt
	deleted := kbCollection(t, api, admin, map[string]any{"name": "Contract empty collection"})
	kb.deleteCollectionID, kb.deleteCollectionSeen = deleted.ID, deleted.UpdatedAt
	trashed := kbCollection(t, api, admin, map[string]any{"name": "Contract trashed collection"})
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/collections/"+trashed.ID+"?updated_at="+url.QueryEscape(trashed.UpdatedAt), nil, admin, http.StatusNoContent, nil)
	var trash struct {
		Items []kbTrashItem `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/collections/trash", nil, admin, http.StatusOK, &trash)
	kb.trashCollectionID, kb.trashCollectionSeen = trashed.ID, trashUpdatedAt(trash.Items, trashed.ID)

	image := uploadKBImage(t, api, admin, kbPNG, http.StatusCreated)
	kb.imageID, kb.imageSeen = image.ID, image.UpdatedAt
	kb.trashMediaID = uploadKBImage(t, api, admin, pngImage(201), http.StatusCreated).ID
	kb.deleteMediaID = uploadKBImage(t, api, admin, pngImage(202), http.StatusCreated).ID
	kb.restoreMediaID = uploadKBImage(t, api, admin, pngImage(203), http.StatusCreated).ID
	kb.purgeMediaID = uploadKBImage(t, api, admin, pngImage(204), http.StatusCreated).ID
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/trash", map[string]any{"ids": []string{kb.restoreMediaID, kb.purgeMediaID}}, admin, http.StatusOK, nil)
	kb.replaceMediaID = uploadKBImage(t, api, admin, pngImage(205), http.StatusCreated).ID
	kb.replaceWithID = uploadKBImage(t, api, admin, pngImage(206), http.StatusCreated).ID
	undone := uploadKBImage(t, api, admin, pngImage(207), http.StatusCreated).ID
	var replacement struct {
		ID string `json:"id"`
	}
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/"+undone+"/replace", map[string]any{"with_media_id": kb.replaceWithID}, admin, http.StatusOK, &replacement)
	kb.replacementID = replacement.ID
	video := uploadKBVideo(t, api, admin, mediaVideo(t, "h264.mp4"), 4096, nil)
	kb.videoID = video.ID
	var posted kbMediaView
	posterResponse := serve(api, kbPosterRequest(video.ID, admin, pngImage(208)))
	if posterResponse.Code != http.StatusOK {
		t.Fatalf("poster status=%d body=%s", posterResponse.Code, posterResponse.Body.String())
	}
	decode(t, posterResponse, &posted)
	kb.posterPath = *posted.PosterURL
	kb.partUploadID = startKBUpload(t, api, admin, len(contractVideoPart), nil).ID
	kb.posterVideoID = uploadKBVideo(t, api, admin, mediaVideo(t, "fragmented.mp4"), 4096, nil).ID
	webm := mediaVideo(t, "vp9.webm")
	ready := startKBUpload(t, api, admin, len(webm), nil)
	if response := serve(api, kbPartRequest(ready.ID, admin, webm, 0, len(webm)-1, len(webm))); response.Code != http.StatusOK {
		t.Fatalf("part status=%d body=%s", response.Code, response.Body.String())
	}
	kb.completeUploadID = ready.ID
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Contract article", "body_md": "![Screen](" + image.URL + ")"})
	kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Contract article to replace in", "body_md": "![Old](/api/v1/kb/images/" + kb.replaceMediaID + ")"})
	kb.articleID, kb.articleSeen = article.ID, article.UpdatedAt
	deleteArticle := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Contract article to delete", "body_md": "x"})
	kb.deleteArticleID, kb.deleteArticleSeen = deleteArticle.ID, deleteArticle.UpdatedAt
	trashArticle := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Contract trashed article", "body_md": "x"})
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/articles/"+trashArticle.ID+"?updated_at="+url.QueryEscape(trashArticle.UpdatedAt), nil, admin, http.StatusNoContent, nil)
	var trashedArticle kbArticleView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+trashArticle.ID, nil, admin, http.StatusOK, &trashedArticle)
	kb.trashArticleID, kb.trashArticleSeen = trashArticle.ID, trashedArticle.UpdatedAt
	versioned := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Contract versioned article", "body_md": "First"})
	var edited kbArticleView
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+versioned.ID, map[string]any{"updated_at": versioned.UpdatedAt, "body_md": "Second"}, admin, http.StatusOK, &edited)
	kb.versionArticleID, kb.versionArticleSeen = versioned.ID, edited.UpdatedAt
	return kb
}

// kbImageRequest is the contract request of an image upload: raw PNG bytes.
func kbImageRequest(token string, cookie *http.Cookie) *http.Request {
	req := httptest.NewRequest(http.MethodPost, kbImagesPath, bytes.NewReader(kbPNG))
	req.Header.Set("Content-Type", "image/png")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	return req
}

func kbContractCases(f contractFixture) map[string]contractCase {
	admin, kb := f.adminToken, f.kb
	return map[string]contractCase{
		"GET /api/v1/kb/collections":                               {"/api/v1/kb/collections", nil, admin, 200, ""},
		"POST /api/v1/kb/collections":                              {"/api/v1/kb/collections", map[string]any{"name": "Created contract collection", "parent_id": kb.collectionID}, admin, 201, ""},
		"GET /api/v1/kb/collections/trash":                         {"/api/v1/kb/collections/trash", nil, admin, 200, ""},
		"PATCH /api/v1/kb/collections/{id}":                        {"/api/v1/kb/collections/" + kb.collectionID, map[string]any{"updated_at": kb.collectionSeen, "description": "Updated"}, admin, 200, ""},
		"DELETE /api/v1/kb/collections/{id}":                       {"/api/v1/kb/collections/" + kb.deleteCollectionID + "?updated_at=" + url.QueryEscape(kb.deleteCollectionSeen), nil, admin, 204, ""},
		"POST /api/v1/kb/collections/{id}/restore":                 {"/api/v1/kb/collections/" + kb.trashCollectionID + "/restore", map[string]any{"updated_at": kb.trashCollectionSeen}, admin, 200, ""},
		"PUT /api/v1/kb/order":                                     {"/api/v1/kb/order", map[string]any{"parent_id": kb.collectionID, "collection_ids": []string{}, "article_ids": []string{}}, admin, 409, ""},
		"GET /api/v1/kb/articles":                                  {"/api/v1/kb/articles?q=contract&status=draft", nil, admin, 200, ""},
		"POST /api/v1/kb/articles":                                 {"/api/v1/kb/articles", map[string]any{"collection_id": kb.collectionID, "title": "Created contract article", "body_md": "Hi", "status": "published"}, admin, 201, ""},
		"GET /api/v1/kb/articles/trash":                            {"/api/v1/kb/articles/trash", nil, admin, 200, ""},
		"GET /api/v1/kb/articles/{id}":                             {"/api/v1/kb/articles/" + kb.articleID, nil, admin, 200, ""},
		"PATCH /api/v1/kb/articles/{id}":                           {"/api/v1/kb/articles/" + kb.articleID, map[string]any{"updated_at": kb.articleSeen, "status": "published"}, admin, 200, ""},
		"DELETE /api/v1/kb/articles/{id}":                          {"/api/v1/kb/articles/" + kb.deleteArticleID + "?updated_at=" + url.QueryEscape(kb.deleteArticleSeen), nil, admin, 204, ""},
		"POST /api/v1/kb/articles/{id}/restore":                    {"/api/v1/kb/articles/" + kb.trashArticleID + "/restore", map[string]any{"updated_at": kb.trashArticleSeen}, admin, 200, ""},
		"GET /api/v1/kb/articles/{id}/versions":                    {"/api/v1/kb/articles/" + kb.versionArticleID + "/versions", nil, admin, 200, ""},
		"POST /api/v1/kb/articles/{id}/versions/{version}/restore": {"/api/v1/kb/articles/" + kb.versionArticleID + "/versions/1/restore", map[string]any{"updated_at": kb.versionArticleSeen}, admin, 200, ""},
		"POST /api/v1/kb/articles/{id}/ai-exclusion":               {"/api/v1/kb/articles/" + kb.articleID + "/ai-exclusion", nil, admin, 200, ""},
		"DELETE /api/v1/kb/articles/{id}/ai-exclusion":             {"/api/v1/kb/articles/" + kb.versionArticleID + "/ai-exclusion", nil, admin, 200, ""},
		"POST /api/v1/kb/images":                                   {path: kbImagesPath, token: admin, status: 201},
		"GET /api/v1/kb/images/{id}":                               {path: "/api/v1/kb/images/" + kb.imageID, token: admin, status: 200},
		"GET /api/v1/kb/media":                                     {path: "/api/v1/kb/media?usage=unused&sort=largest", token: admin, status: 200},
		"PATCH /api/v1/kb/media/{id}":                              {"/api/v1/kb/media/" + kb.imageID, map[string]any{"updated_at": kb.imageSeen, "alt": "The settings screen"}, admin, 200, ""},
		"GET /api/v1/kb/media/trash":                               {path: "/api/v1/kb/media/trash", token: admin, status: 200},
		"POST /api/v1/kb/media/trash":                              {"/api/v1/kb/media/trash", map[string]any{"ids": []string{kb.trashMediaID}}, admin, 200, ""},
		"POST /api/v1/kb/media/trash-unused":                       {"/api/v1/kb/media/trash-unused", map[string]any{"before": "2100-01-01T00:00:00Z"}, admin, 200, ""},
		"POST /api/v1/kb/media/restore":                            {"/api/v1/kb/media/restore", map[string]any{"ids": []string{kb.trashMediaID}}, admin, 200, ""},
		"POST /api/v1/kb/media/purge":                              {"/api/v1/kb/media/purge", map[string]any{"ids": []string{kb.purgeMediaID}}, admin, 200, ""},
		"DELETE /api/v1/kb/media/{id}":                             {path: "/api/v1/kb/media/" + kb.deleteMediaID, token: admin, status: 200},
		"POST /api/v1/kb/media/{id}/restore":                       {path: "/api/v1/kb/media/" + kb.restoreMediaID + "/restore", token: admin, status: 200},
		"POST /api/v1/kb/media/{id}/replace":                       {"/api/v1/kb/media/" + kb.replaceMediaID + "/replace", map[string]any{"with_media_id": kb.replaceWithID}, admin, 200, ""},
		"POST /api/v1/kb/media-replacements/{id}/undo":             {path: "/api/v1/kb/media-replacements/" + kb.replacementID + "/undo", token: admin, status: 200},
		"POST /api/v1/kb/media-uploads":                            {"/api/v1/kb/media-uploads", map[string]any{"size": 1000, "name": "Tour"}, admin, 201, ""},
		"GET /api/v1/kb/media-uploads/{id}":                        {path: "/api/v1/kb/media-uploads/" + kb.partUploadID, token: admin, status: 200},
		"GET /api/v1/kb/media-uploads":                             {path: "/api/v1/kb/media-uploads", token: admin, status: 200},
		"PUT /api/v1/kb/media-uploads/{id}":                        {path: "/api/v1/kb/media-uploads/" + kb.partUploadID, token: admin, status: 200},
		"DELETE /api/v1/kb/media-uploads/{id}":                     {path: "/api/v1/kb/media-uploads/" + kb.partUploadID, token: admin, status: 204},
		"POST /api/v1/kb/media-uploads/{id}/complete":              {path: "/api/v1/kb/media-uploads/" + kb.completeUploadID + "/complete", token: admin, status: 201},
		"GET /api/v1/kb/media/{id}":                                {path: "/api/v1/kb/media/" + kb.videoID, token: admin, status: 200},
		"PUT /api/v1/kb/media/{id}/poster":                         {path: "/api/v1/kb/media/" + kb.posterVideoID + "/poster", token: admin, status: 200},
		"GET /api/v1/kb/media/{id}/poster/{version}":               {path: kb.posterPath, token: admin, status: 200},
		"GET /api/v1/kb/media/{id}/details":                        {path: "/api/v1/kb/media/" + kb.imageID + "/details", token: admin, status: 200},
		"GET /api/v1/kb/media/{id}/thumbnail":                      {path: "/api/v1/kb/media/" + kb.imageID + "/thumbnail", token: admin, status: 200},
		"GET /api/v1/help-center":                                  {"/api/v1/help-center", nil, admin, 200, ""},
		"PUT /api/v1/help-center":                                  {"/api/v1/help-center", map[string]any{"enabled": true, "title": "Contract help", "locale": "en"}, admin, 200, ""},
		"GET /api/v1/help-center/theme":                            {path: "/api/v1/help-center/theme", token: admin, status: 200},
		"PUT /api/v1/help-center/theme":                            {"/api/v1/help-center/theme", map[string]any{"logo_media_id": kb.imageID, "colors": map[string]any{"primary": "#0b5cad"}, "custom_css": ""}, admin, 200, ""},
		"GET /api/v1/help-center/domain":                           {path: "/api/v1/help-center/domain", token: admin, status: 200},
		"PUT /api/v1/help-center/domain":                           {"/api/v1/help-center/domain", map[string]any{"hostname": "help.contract.example"}, admin, 200, ""},
		"POST /api/v1/help-center/domain/check":                    {"/api/v1/help-center/domain/check", nil, admin, 404, ""},
		"DELETE /api/v1/help-center/domain":                        {path: "/api/v1/help-center/domain", token: admin, status: 200},
		"POST /api/v1/help-center/theme/check":                     {"/api/v1/help-center/theme/check", map[string]any{"colors": map[string]any{"text": "#999999"}}, admin, 200, ""},
		"POST /api/v1/help-center/theme/preview":                   {"/api/v1/help-center/theme/preview", map[string]any{"page": "article", "theme": map[string]any{"colors": map[string]any{"text": "#999999"}}}, admin, 200, ""},
		// The contract server has no HELP_CENTER_DOMAIN, so every host name is refused.
		"GET /api/v1/public/help-center/tls-ask": {"/api/v1/public/help-center/tls-ask?domain=acme.help.example.net", nil, "", 404, ""},
	}
}

const kbImagesPath = "/api/v1/kb/images"
