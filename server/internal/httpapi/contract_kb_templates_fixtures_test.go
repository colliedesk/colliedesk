package httpapi_test

import (
	"net/http"
	"net/url"
	"testing"
)

// contractKBTemplates are the article templates the contract cases act on: one for each change a case makes.
type contractKBTemplates struct {
	readID                 string
	updateID, updateSeen   string
	deleteID, deleteSeen   string
	trashID, trashSeen     string
	versionID, versionSeen string
}

func seedContractKBTemplates(t *testing.T, api http.Handler, admin string) contractKBTemplates {
	t.Helper()
	var templates contractKBTemplates
	templates.readID = kbTemplate(t, api, admin, map[string]any{"name": "Contract read template", "body": "## Read\n"}).ID
	updated := kbTemplate(t, api, admin, map[string]any{"name": "Contract update template", "body": "## Update\n"})
	templates.updateID, templates.updateSeen = updated.ID, updated.UpdatedAt
	deleted := kbTemplate(t, api, admin, map[string]any{"name": "Contract delete template", "body": "## Delete\n"})
	templates.deleteID, templates.deleteSeen = deleted.ID, deleted.UpdatedAt

	trashed := kbTemplate(t, api, admin, map[string]any{"name": "Contract trashed template", "body": "## Trash\n"})
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/templates/"+trashed.ID+"?updated_at="+url.QueryEscape(trashed.UpdatedAt), nil, admin, http.StatusNoContent, nil)
	var trash struct {
		Items []kbTrashItem `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/templates/trash", nil, admin, http.StatusOK, &trash)
	templates.trashID, templates.trashSeen = trashed.ID, trashUpdatedAt(trash.Items, trashed.ID)

	versioned := kbTemplate(t, api, admin, map[string]any{"name": "Contract versioned template", "body": "## First\n"})
	var edited kbTemplateView
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/templates/"+versioned.ID, map[string]any{"updated_at": versioned.UpdatedAt, "body": "## Second\n"}, admin, http.StatusOK, &edited)
	templates.versionID, templates.versionSeen = versioned.ID, edited.UpdatedAt
	return templates
}

func kbTemplateContractCases(f contractFixture) map[string]contractCase {
	admin, templates := f.adminToken, f.kbTemplates
	return map[string]contractCase{
		"GET /api/v1/kb/templates":       {"/api/v1/kb/templates", nil, admin, 200, ""},
		"POST /api/v1/kb/templates":      {"/api/v1/kb/templates", map[string]any{"name": "Created contract template", "description": "One line.", "body": "## Steps\n"}, admin, 201, ""},
		"GET /api/v1/kb/templates/trash": {"/api/v1/kb/templates/trash", nil, admin, 200, ""},
		"GET /api/v1/kb/templates/{id}":  {"/api/v1/kb/templates/" + templates.readID, nil, admin, 200, ""},
		"PATCH /api/v1/kb/templates/{id}": {"/api/v1/kb/templates/" + templates.updateID,
			map[string]any{"updated_at": templates.updateSeen, "description": "Updated."}, admin, 200, ""},
		"DELETE /api/v1/kb/templates/{id}": {"/api/v1/kb/templates/" + templates.deleteID + "?updated_at=" + url.QueryEscape(templates.deleteSeen), nil, admin, 204, ""},
		"POST /api/v1/kb/templates/{id}/restore": {"/api/v1/kb/templates/" + templates.trashID + "/restore",
			map[string]any{"updated_at": templates.trashSeen}, admin, 200, ""},
		"GET /api/v1/kb/templates/{id}/versions": {"/api/v1/kb/templates/" + templates.versionID + "/versions", nil, admin, 200, ""},
		"POST /api/v1/kb/templates/{id}/versions/{version}/restore": {"/api/v1/kb/templates/" + templates.versionID + "/versions/1/restore",
			map[string]any{"updated_at": templates.versionSeen}, admin, 200, ""},
	}
}
