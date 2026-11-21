package freshdesk_test

import (
	"context"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// changeSpanishTranslation makes Freshdesk's Spanish translation of article 101 newer.
func (e *importEnv) changeSpanishTranslation() {
	e.editSolutions(func(fixture map[string]any) {
		translation := fixture["translations"].(map[string]any)["es"].(map[string]any)["11"].([]any)[0].(map[string]any)
		translation["description"], translation["updated_at"] = "<p>Cambiado en Freshdesk.</p>", "2024-06-01T10:00:00Z"
	})
}

func (e *importEnv) spanishTranslation(t *testing.T, articleID string) domain.KBArticleTranslation {
	t.Helper()
	translation, err := store.New(e.pool).GetKBTranslation(context.Background(), e.workspaceID, articleID, "es")
	if err != nil {
		t.Fatal(err)
	}
	return translation
}

// The unpublished changes of a published translation are a person's edit, even an autosave that
// records no version: a rerun keeps the translation and its draft, and undo keeps the article.
func TestSolutionsRerunAndUndoLeaveATranslationWithAnAutosavedDraftAlone(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	ctx := context.Background()
	env.mustRun(t)
	reset := env.kbArticles(t, env.workspaceID)["101"]
	before := env.spanishTranslation(t, reset.ID)
	body := "Borrador en Colliedesk."
	if _, err := store.New(env.pool).UpdateKBTranslation(ctx, env.workspaceID, store.Actor{}, reset.ID, "es", before.UpdatedAt,
		store.KBTranslationChange{BodyMD: &body, Autosave: true}); err != nil {
		t.Fatal(err)
	}
	env.changeSpanishTranslation()
	report := env.mustRun(t)
	if report.Status != freshdesk.StatusOK || strings.Join(report.KnowledgeBase.TranslationsChangedInBoth, ",") != "101:es" {
		t.Fatalf("status=%s changed in both=%v", report.Status, report.KnowledgeBase.TranslationsChangedInBoth)
	}
	after := env.spanishTranslation(t, reset.ID)
	if after.BodyMD != before.BodyMD || after.Draft == nil || after.Draft.BodyMD != body {
		t.Fatalf("translation after the rerun: published %q, draft %+v", after.BodyMD, after.Draft)
	}

	result, err := freshdesk.Undo(ctx, env.pool, env.storage, freshdesk.UndoOptions{WorkspaceSlug: "acme-support"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(result.KeptKBArticles, ",") != "101" || strings.Join(result.KeptKBTranslations, ",") != "101:es" {
		t.Fatalf("undo kept articles %v translations %v, want 101 and 101:es", result.KeptKBArticles, result.KeptKBTranslations)
	}
	if kept := env.spanishTranslation(t, reset.ID); kept.Draft == nil || kept.Draft.BodyMD != body {
		t.Fatalf("the draft after undo = %+v", kept.Draft)
	}
}

// A translation draft that was discarded leaves the translation as the import wrote it: a rerun takes
// the Freshdesk change again, as for a translation nobody touched.
func TestSolutionsRerunUpdatesATranslationWhoseDraftWasDiscarded(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	ctx := context.Background()
	env.mustRun(t)
	kbStore := store.New(env.pool)
	reset := env.kbArticles(t, env.workspaceID)["101"]
	body := "Borrador, luego descartado."
	saved, err := kbStore.UpdateKBTranslation(ctx, env.workspaceID, store.Actor{}, reset.ID, "es", env.spanishTranslation(t, reset.ID).UpdatedAt, store.KBTranslationChange{BodyMD: &body})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := kbStore.DiscardKBTranslationDraft(ctx, env.workspaceID, store.Actor{}, reset.ID, "es", saved.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	env.changeSpanishTranslation()
	report := env.mustRun(t)
	if report.Status != freshdesk.StatusOK || len(report.KnowledgeBase.TranslationsChangedInBoth) != 0 {
		t.Fatalf("status=%s changed in both=%v", report.Status, report.KnowledgeBase.TranslationsChangedInBoth)
	}
	if after := env.spanishTranslation(t, reset.ID); after.BodyMD != "Cambiado en Freshdesk." || after.Draft != nil {
		t.Fatalf("translation after the rerun: %q draft %+v, want the Freshdesk change", after.BodyMD, after.Draft)
	}
}
