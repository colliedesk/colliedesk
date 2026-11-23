package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// A theme preview of a help center other than the primary shows that help center: its title, its
// collections and its first article, never the primary's.
func TestThemePreviewShowsTheHelpCenterItNames(t *testing.T) {
	f := newTwoHelpCenters(t)
	theme := map[string]any{"colors": map[string]any{"primary": "#0B5FFF"}}
	previewOf := func(helpCenterID, page string) string {
		t.Helper()
		path := previewPath
		if helpCenterID != "" {
			path += "?help_center_id=" + helpCenterID
		}
		var view previewView
		response := kbCall(t, f.api, http.MethodPost, path, map[string]any{"page": page, "theme": theme}, f.admin, http.StatusOK, nil)
		if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil {
			t.Fatal(err)
		}
		return view.HTML
	}
	wants := func(name, html string, shown, hidden []string) {
		t.Helper()
		for _, text := range shown {
			if !strings.Contains(html, text) {
				t.Errorf("%s does not show %q", name, text)
			}
		}
		for _, text := range hidden {
			if strings.Contains(html, text) {
				t.Errorf("%s shows %q", name, text)
			}
		}
	}

	wants("hardware's home preview", previewOf(f.hardware, "home"), []string{"Hardware answers", "Hardware", "Shared shelf"}, []string{"Main answers", "Main shelf"})
	wants("hardware's article preview", previewOf(f.hardware, "article"), []string{f.shared.Title}, []string{f.main.Title})
	wants("the primary's home preview", previewOf("", "home"), []string{"Main answers", "Main shelf"}, []string{"Hardware answers"})
	wants("the primary's article preview", previewOf("", "article"), []string{f.main.Title}, nil)

	other := createSession(t, f.api, f.service, "Preview Elsewhere", "admin@preview-elsewhere.example.com")
	kbCall(t, f.api, http.MethodPost, previewPath+"?help_center_id="+f.hardware, map[string]any{"page": "home", "theme": theme}, other, http.StatusNotFound, nil)
}
