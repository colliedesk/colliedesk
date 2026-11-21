package httpapi_test

import (
	"net/http"
	"testing"
)

// Every text an article can hold is saved with its formatting tags in one spelling, so the visual
// editor reads back exactly what it wrote and two ways of writing the same text never differ.
func TestArticleTextsAreSavedWithCanonicalFormattingTags(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Body Format", "admin@kb-body-format.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	const loose = "Say <U>this</U> and <SPAN style=\"color:red\" data-color='danger'>that</SPAN>.\n"
	const canonical = "Say <u>this</u> and <span data-color=\"danger\">that</span>.\n"

	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Formatting", "body_md": loose})
	if article.BodyMD != canonical {
		t.Fatalf("created body = %q, want %q", article.BodyMD, canonical)
	}
	var updated kbArticleView
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+article.ID, map[string]any{"updated_at": article.UpdatedAt,
		"body_md": "<P DATA-ALIGN='right'>\n\nEnd\n\n</P>\n"}, admin, http.StatusOK, &updated)
	if updated.BodyMD != "<p data-align=\"right\">\n\nEnd\n\n</p>\n" {
		t.Fatalf("updated body = %q", updated.BodyMD)
	}

	kbCall(t, api, http.MethodPut, "/api/v1/help-center/languages", map[string]any{"languages": []map[string]any{{"locale": "fr", "visible": true}}}, admin, http.StatusOK, nil)
	var translation kbTranslationView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+article.ID+"/translations", map[string]any{"locale": "fr", "title": "Mise en forme", "body_md": loose},
		admin, http.StatusCreated, &translation)
	if translation.BodyMD != canonical {
		t.Fatalf("translation body = %q, want %q", translation.BodyMD, canonical)
	}

	template := kbTemplate(t, api, admin, map[string]any{"name": "Formatted", "body": "<MARK>Note</MARK>\n"})
	if template.Body != "<mark>Note</mark>\n" {
		t.Fatalf("template body = %q", template.Body)
	}
	fromTemplate := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "From template", "template_id": template.ID})
	if fromTemplate.BodyMD != "<mark>Note</mark>\n" {
		t.Fatalf("article from the template = %q", fromTemplate.BodyMD)
	}
}
