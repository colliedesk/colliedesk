package freshdesk_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

type storedTranslation struct {
	Title, Slug, Status, SEOTitle, SEODescription string
	Outdated, InTrash                             bool
	UpdatedAt                                     time.Time
	Newest                                        string
}

func (e *importEnv) kbTranslation(t *testing.T, articleID, locale string) (storedTranslation, bool) {
	t.Helper()
	var item storedTranslation
	err := e.pool.QueryRow(context.Background(), `SELECT t.title,t.slug,t.status,t.seo_title,t.seo_description,t.outdated,t.deleted_at IS NOT NULL,t.updated_at,
			COALESCE((SELECT kind FROM kb_article_translation_versions v WHERE v.workspace_id=t.workspace_id AND v.article_id=t.article_id AND v.locale=t.locale ORDER BY version DESC LIMIT 1),'')
		FROM kb_article_translations t WHERE t.workspace_id=$1 AND t.article_id=$2 AND t.locale=$3`, e.workspaceID, articleID, locale).
		Scan(&item.Title, &item.Slug, &item.Status, &item.SEOTitle, &item.SEODescription, &item.Outdated, &item.InTrash, &item.UpdatedAt, &item.Newest)
	if err != nil {
		return item, false
	}
	return item, true
}

// kbNames lists "collection/locale=name" of the workspace, sorted.
func (e *importEnv) kbNames(t *testing.T) string {
	t.Helper()
	var names string
	if err := e.pool.QueryRow(context.Background(), `SELECT COALESCE(string_agg(c.name||'/'||t.locale||'='||t.name, ' | ' ORDER BY c.name, t.locale), '')
		FROM kb_collection_translations t JOIN kb_collections c ON c.workspace_id=t.workspace_id AND c.id=t.collection_id WHERE t.workspace_id=$1`, e.workspaceID).Scan(&names); err != nil {
		t.Fatal(err)
	}
	return names
}

// kbLanguages lists the help center's primary language and "locale:shown" of the others.
func (e *importEnv) kbLanguages(t *testing.T) string {
	t.Helper()
	var languages string
	if err := e.pool.QueryRow(context.Background(), `SELECT COALESCE((SELECT locale FROM help_centers WHERE workspace_id=$1),'en') || ' ' ||
		COALESCE((SELECT string_agg(locale||':'||visible::text, ',' ORDER BY position, locale) FROM help_center_languages WHERE workspace_id=$1), '')`, e.workspaceID).Scan(&languages); err != nil {
		t.Fatal(err)
	}
	return languages
}

func TestSolutionsTranslationsKeepStatusSearchFieldsCollectionNamesAndPortalLanguages(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	report := env.mustRun(t)
	kb := report.KnowledgeBase
	if report.Status != freshdesk.StatusOK {
		t.Fatalf("status=%s missing=%v failures=%+v", report.Status, report.Missing, report.Failures)
	}
	assertCount(t, "kb collection names", kb.CollectionNames, 3, 3)
	if strings.Join(kb.LanguagesAdded, ",") != "es,fr" {
		t.Fatalf("languages added = %v", kb.LanguagesAdded)
	}
	// es is on the Freshdesk portal, fr is supported but not shown there.
	if got := env.kbLanguages(t); got != "en es:true,fr:false" {
		t.Fatalf("languages = %s", got)
	}
	if got := env.kbNames(t); got != "Advanced / Deep/es=Advanced / Profundo | Basics/es=Conceptos básicos | Getting started/es=Primeros pasos" {
		t.Fatalf("names = %s", got)
	}
	articles := env.kbArticles(t, env.workspaceID)
	reset := articles["101"]
	translation, ok := env.kbTranslation(t, reset.ID, "es")
	want := storedTranslation{Title: "Restablecer la contraseña", Slug: "restablecer-la-contraseña", Status: "published", SEOTitle: "Contraseña", SEODescription: "Cómo restablecerla",
		UpdatedAt: time.Date(2023, 3, 11, 10, 0, 0, 0, time.UTC), Newest: "import"}
	translation.UpdatedAt = translation.UpdatedAt.UTC()
	if !ok || translation != want {
		t.Fatalf("translation = %+v, want %+v", translation, want)
	}
	if !strings.Contains(report.Markdown(), "| Knowledge base collection names | 3 | 3 | 0 |") {
		t.Fatalf("report lacks the collection names:\n%s", report.Markdown())
	}

	// A second run writes nothing again.
	versions := env.count(t, "kb_article_translation_versions")
	if again := env.mustRun(t); again.Status != freshdesk.StatusOK || env.count(t, "kb_article_translation_versions") != versions || len(again.KnowledgeBase.LanguagesAdded) != 0 {
		t.Fatalf("rerun: status=%s versions %d -> %d added=%v", again.Status, versions, env.count(t, "kb_article_translation_versions"), again.KnowledgeBase.LanguagesAdded)
	}
}

func TestSolutionsRerunKeepsATranslationAPersonChangedAndBringsBackARemovedName(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	ctx := context.Background()
	env.mustRun(t)
	kbStore := store.New(env.pool)
	articles := env.kbArticles(t, env.workspaceID)
	reset := articles["101"]
	current, err := kbStore.GetKBTranslation(ctx, env.workspaceID, reset.ID, "es")
	if err != nil {
		t.Fatal(err)
	}
	body := "Editado en Colliedesk."
	if _, err := kbStore.UpdateKBTranslation(ctx, env.workspaceID, store.Actor{}, reset.ID, "es", current.UpdatedAt, store.KBTranslationChange{BodyMD: &body}); err != nil {
		t.Fatal(err)
	}
	// An admin hides es and removes the Spanish name of a category.
	if _, err := kbStore.SetHelpCenterLanguages(ctx, env.workspaceID, []domain.HelpCenterLanguageInput{{Locale: "es"}, {Locale: "fr"}}); err != nil {
		t.Fatal(err)
	}
	var category string
	if err := env.pool.QueryRow(ctx, `SELECT id::text FROM kb_collections WHERE workspace_id=$1 AND name='Getting started'`, env.workspaceID).Scan(&category); err != nil {
		t.Fatal(err)
	}
	if _, err := kbStore.DeleteKBCollectionTranslation(ctx, env.workspaceID, category, "es"); err != nil {
		t.Fatal(err)
	}
	env.editSolutions(func(fixture map[string]any) {
		translation := fixture["translations"].(map[string]any)["es"].(map[string]any)["11"].([]any)[0].(map[string]any)
		translation["description"], translation["updated_at"] = "<p>Cambiado en Freshdesk.</p>", "2024-06-01T10:00:00Z"
		article := fixtureArticle(fixture, "11", 104-3)
		article["description"], article["updated_at"] = "<p>Changed in Freshdesk.</p>", "2024-06-02T10:00:00Z"
	})
	report := env.mustRun(t)
	if report.Status != freshdesk.StatusOK || strings.Join(report.KnowledgeBase.TranslationsChangedInBoth, ",") != "101:es" || warningIDs(report, "kb_translation_changed_in_both") != "101:es" {
		t.Fatalf("status=%s changed=%v warnings=%+v", report.Status, report.KnowledgeBase.TranslationsChangedInBoth, report.Warnings)
	}
	translation, _ := env.kbTranslation(t, reset.ID, "es")
	if translation.Newest != "edit" {
		t.Fatalf("the person's translation was overwritten: %+v", translation)
	}
	// The article changed in Freshdesk, its translation did not come again: it is outdated now.
	if !translation.Outdated {
		t.Fatalf("translation after the article changed = %+v, want outdated", translation)
	}
	if got := env.kbLanguages(t); got != "en es:false,fr:false" {
		t.Fatalf("the rerun changed what the admin set: %s", got)
	}
	if got := env.kbNames(t); !strings.Contains(got, "Getting started/es=Primeros pasos") {
		t.Fatalf("the removed name did not come back: %s", got)
	}

	// Undo keeps the article whose translation a person changed, and the names a person set; it takes
	// the languages the import added that hold nothing.
	result, err := freshdesk.Undo(ctx, env.pool, env.storage, freshdesk.UndoOptions{WorkspaceSlug: "acme-support"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(result.KeptKBArticles, ",") != "101" || strings.Join(result.KeptKBTranslations, ",") != "101:es" || result.KBCollectionNames != 3 || result.KBLanguages != 1 {
		t.Fatalf("undo = %+v", result)
	}
	if kept, ok := env.kbTranslation(t, reset.ID, "es"); !ok || kept.Newest != "edit" {
		t.Fatalf("the changed translation is gone: %+v", kept)
	}
	if got := env.kbLanguages(t); got != "en es:false" {
		t.Fatalf("languages after undo = %s", got)
	}
	if got := env.kbNames(t); got != "" {
		t.Fatalf("names after undo = %s", got)
	}
}

func TestSolutionsUndoRemovesTranslationsOnlyTheImportWroteFromAnArticleThatStays(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	ctx := context.Background()
	env.mustRun(t)
	reset := env.kbArticles(t, env.workspaceID)["101"]
	env.editArticle(t, reset.ID, "Edited in Colliedesk.")
	result, err := freshdesk.Undo(ctx, env.pool, env.storage, freshdesk.UndoOptions{WorkspaceSlug: "acme-support"})
	if err != nil {
		t.Fatal(err)
	}
	if result.KBTranslations != 1 || len(result.KeptKBTranslations) != 0 || result.KBLanguages != 2 {
		t.Fatalf("undo = %+v", result)
	}
	if _, ok := env.kbTranslation(t, reset.ID, "es"); ok {
		t.Fatal("the imported translation of the kept article stayed")
	}
}

func TestSolutionsTheAccountsPrimaryLanguageBecomesTheHelpCentersOnlyWhenSafe(t *testing.T) {
	german := newImportEnv(t).withSolutions()
	german.editSolutions(func(fixture map[string]any) {
		fixture["settings"] = map[string]any{"primary_language": "de", "supported_languages": []any{"es"}, "portal_languages": []any{}}
	})
	german.mustRun(t)
	if got := german.kbLanguages(t); got != "de es:false" {
		t.Fatalf("languages = %s", got)
	}

	own := newImportEnv(t).withSolutions()
	ctx := context.Background()
	collection, err := store.New(own.pool).CreateKBCollection(ctx, own.workspaceID, store.Actor{}, store.KBCollectionInput{Name: "Ours"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.New(own.pool).CreateKBArticle(ctx, own.workspaceID, store.Actor{}, store.KBArticleInput{CollectionID: collection.ID, Title: "Ours", BodyMD: "Written here."}); err != nil {
		t.Fatal(err)
	}
	own.editSolutions(func(fixture map[string]any) {
		fixture["settings"] = map[string]any{"primary_language": "de", "supported_languages": []any{"es"}, "portal_languages": []any{"es"}}
	})
	report := own.mustRun(t)
	if got := own.kbLanguages(t); got != "en es:true" || warningIDs(report, "kb_primary_language_differs") != "de" {
		t.Fatalf("languages = %s warnings = %+v", got, report.Warnings)
	}
}

// Undo gives the help center back the primary language it had before the import made Freshdesk's its own.
func TestUndoGivesBackThePrimaryLanguageTheImportSet(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	env.editSolutions(func(fixture map[string]any) {
		fixture["settings"] = map[string]any{"primary_language": "de", "supported_languages": []any{"es"}, "portal_languages": []any{}}
	})
	env.mustRun(t)
	if got := env.kbLanguages(t); got != "de es:false" {
		t.Fatalf("languages after the import = %s", got)
	}
	// A second run changes nothing and must not forget what the help center had first.
	env.mustRun(t)
	result, err := freshdesk.Undo(context.Background(), env.pool, env.storage, freshdesk.UndoOptions{WorkspaceSlug: "acme-support"})
	if err != nil {
		t.Fatal(err)
	}
	if got := env.kbLanguages(t); got != "en " || result.KBPrimaryLanguage != "en" || result.KeptKBPrimaryLanguage != "" {
		t.Fatalf("after undo languages = %q, result = %+v", got, result)
	}
}

// A primary language a person changed after the import stays; undo says so.
func TestUndoKeepsAPrimaryLanguageAPersonChanged(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	env.editSolutions(func(fixture map[string]any) {
		fixture["settings"] = map[string]any{"primary_language": "de", "supported_languages": []any{"es"}, "portal_languages": []any{}}
	})
	env.mustRun(t)
	if _, err := env.pool.Exec(context.Background(), `UPDATE help_centers SET locale='fr' WHERE workspace_id=$1`, env.workspaceID); err != nil {
		t.Fatal(err)
	}
	result, err := freshdesk.Undo(context.Background(), env.pool, env.storage, freshdesk.UndoOptions{WorkspaceSlug: "acme-support"})
	if err != nil {
		t.Fatal(err)
	}
	if got := env.kbLanguages(t); got != "fr " || result.KBPrimaryLanguage != "" || result.KeptKBPrimaryLanguage != "fr" ||
		!strings.Contains(result.Markdown(), "Primary language kept") {
		t.Fatalf("after undo languages = %q, result = %+v", got, result)
	}
}

// The earlier primary language is often one Freshdesk supports too, which the import added as another
// language: undo removes that one first, then gives the primary language back.
func TestUndoGivesBackThePrimaryLanguageAlsoAmongTheSupportedOnes(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	env.editSolutions(func(fixture map[string]any) {
		fixture["settings"] = map[string]any{"primary_language": "de", "supported_languages": []any{"en", "es"}, "portal_languages": []any{}}
	})
	env.mustRun(t)
	if got := env.kbLanguages(t); got != "de en:false,es:false" {
		t.Fatalf("languages after the import = %s", got)
	}
	result, err := freshdesk.Undo(context.Background(), env.pool, env.storage, freshdesk.UndoOptions{WorkspaceSlug: "acme-support"})
	if err != nil {
		t.Fatal(err)
	}
	if got := env.kbLanguages(t); got != "en " || result.KBPrimaryLanguage != "en" || result.KeptKBPrimaryLanguage != "" {
		t.Fatalf("after undo languages = %q, result = %+v", got, result)
	}
}
