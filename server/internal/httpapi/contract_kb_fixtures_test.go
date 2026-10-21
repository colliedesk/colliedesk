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
}

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
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Contract article", "body_md": "![Screen](" + image.URL + ")"})
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
		"GET /api/v1/kb/articles":                                  {"/api/v1/kb/articles?q=contract&status=draft", nil, admin, 200, ""},
		"POST /api/v1/kb/articles":                                 {"/api/v1/kb/articles", map[string]any{"collection_id": kb.collectionID, "title": "Created contract article", "body_md": "Hi", "status": "published"}, admin, 201, ""},
		"GET /api/v1/kb/articles/trash":                            {"/api/v1/kb/articles/trash", nil, admin, 200, ""},
		"GET /api/v1/kb/articles/{id}":                             {"/api/v1/kb/articles/" + kb.articleID, nil, admin, 200, ""},
		"PATCH /api/v1/kb/articles/{id}":                           {"/api/v1/kb/articles/" + kb.articleID, map[string]any{"updated_at": kb.articleSeen, "status": "published"}, admin, 200, ""},
		"DELETE /api/v1/kb/articles/{id}":                          {"/api/v1/kb/articles/" + kb.deleteArticleID + "?updated_at=" + url.QueryEscape(kb.deleteArticleSeen), nil, admin, 204, ""},
		"POST /api/v1/kb/articles/{id}/restore":                    {"/api/v1/kb/articles/" + kb.trashArticleID + "/restore", map[string]any{"updated_at": kb.trashArticleSeen}, admin, 200, ""},
		"GET /api/v1/kb/articles/{id}/versions":                    {"/api/v1/kb/articles/" + kb.versionArticleID + "/versions", nil, admin, 200, ""},
		"POST /api/v1/kb/articles/{id}/versions/{version}/restore": {"/api/v1/kb/articles/" + kb.versionArticleID + "/versions/1/restore", map[string]any{"updated_at": kb.versionArticleSeen}, admin, 200, ""},
		"POST /api/v1/kb/images":                                   {path: kbImagesPath, token: admin, status: 201},
		"GET /api/v1/kb/images/{id}":                               {path: "/api/v1/kb/images/" + kb.imageID, token: admin, status: 200},
		"GET /api/v1/kb/media":                                     {path: "/api/v1/kb/media?usage=unused&sort=largest", token: admin, status: 200},
		"PATCH /api/v1/kb/media/{id}":                              {"/api/v1/kb/media/" + kb.imageID, map[string]any{"updated_at": kb.imageSeen, "alt": "The settings screen"}, admin, 200, ""},
		"GET /api/v1/kb/media/{id}/details":                        {path: "/api/v1/kb/media/" + kb.imageID + "/details", token: admin, status: 200},
		"GET /api/v1/kb/media/{id}/thumbnail":                      {path: "/api/v1/kb/media/" + kb.imageID + "/thumbnail", token: admin, status: 200},
		"GET /api/v1/help-center":                                  {"/api/v1/help-center", nil, admin, 200, ""},
		"PUT /api/v1/help-center":                                  {"/api/v1/help-center", map[string]any{"enabled": true, "title": "Contract help", "locale": "en"}, admin, 200, ""},
		// The contract server has no HELP_CENTER_DOMAIN, so every host name is refused.
		"GET /api/v1/public/help-center/tls-ask": {"/api/v1/public/help-center/tls-ask?domain=acme.help.example.net", nil, "", 404, ""},
	}
}

const kbImagesPath = "/api/v1/kb/images"
