package freshdesk_test

import (
	"context"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// Unpublished changes are a person's edit: a rerun that meets a Freshdesk change keeps both the
// published text and the draft, and reports the article as changed in both.
func TestSolutionsRerunLeavesAnArticleWithUnpublishedChangesAlone(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	env.mustRun(t)
	before := env.kbArticles(t, env.workspaceID)
	env.saveArticleDraft(t, before["101"].ID, "Drafted in Colliedesk.")
	env.editSolutions(func(fixture map[string]any) {
		article := fixtureArticle(fixture, "11", 101)
		article["description"], article["updated_at"] = "<p>Changed in Freshdesk.</p>", "2024-06-01T10:00:00Z"
	})
	report := env.mustRun(t)
	if report.Status != freshdesk.StatusOK || strings.Join(report.KnowledgeBase.ChangedInBoth, ",") != "101" {
		t.Fatalf("status=%s changed in both=%v", report.Status, report.KnowledgeBase.ChangedInBoth)
	}
	after := env.kbArticles(t, env.workspaceID)
	if after["101"].Body != before["101"].Body || !after["101"].UpdatedAt.Equal(before["101"].UpdatedAt) {
		t.Fatalf("published article 101 changed: %q at %s", after["101"].Body, after["101"].UpdatedAt)
	}
	article, err := store.New(env.pool).GetKBArticle(context.Background(), env.workspaceID, before["101"].ID)
	if err != nil {
		t.Fatal(err)
	}
	if article.Draft == nil || article.Draft.BodyMD != "Drafted in Colliedesk." {
		t.Fatalf("draft after rerun = %+v", article.Draft)
	}
}

// Unpublished changes that were discarded leave the article as the import wrote it: a rerun takes
// the Freshdesk change again, as for an article nobody touched.
func TestSolutionsRerunUpdatesAnArticleWhoseUnpublishedChangesWereDiscarded(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	env.mustRun(t)
	before := env.kbArticles(t, env.workspaceID)
	saved := env.saveArticleDraft(t, before["101"].ID, "Drafted in Colliedesk.")
	if _, err := store.New(env.pool).DiscardKBArticleDraft(context.Background(), env.workspaceID, store.Actor{}, before["101"].ID, saved.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	env.editSolutions(func(fixture map[string]any) {
		article := fixtureArticle(fixture, "11", 101)
		article["description"], article["updated_at"] = "<p>Changed in Freshdesk.</p>", "2024-06-01T10:00:00Z"
	})
	report := env.mustRun(t)
	if report.Status != freshdesk.StatusOK || len(report.KnowledgeBase.ChangedInBoth) != 0 {
		t.Fatalf("status=%s changed in both=%v", report.Status, report.KnowledgeBase.ChangedInBoth)
	}
	if after := env.kbArticles(t, env.workspaceID)["101"]; after.Body != "Changed in Freshdesk." {
		t.Fatalf("article 101 after the rerun: %q, want the Freshdesk change", after.Body)
	}
}

// Undo keeps an article while it has unpublished changes, and removes one whose unpublished changes
// were discarded, as one nobody touched.
func TestSolutionsUndoKeepsOnlyArticlesThatStillHaveUnpublishedChanges(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	ctx := context.Background()
	env.mustRun(t)
	articles := env.kbArticles(t, env.workspaceID)
	env.saveArticleDraft(t, articles["101"].ID, "Drafted in Colliedesk.")
	saved := env.saveArticleDraft(t, articles["106"].ID, "Drafted, then discarded.")
	if _, err := store.New(env.pool).DiscardKBArticleDraft(ctx, env.workspaceID, store.Actor{}, articles["106"].ID, saved.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	result, err := freshdesk.Undo(ctx, env.pool, env.storage, freshdesk.UndoOptions{WorkspaceSlug: "acme-support"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(result.KeptKBArticles, ",") != "101" {
		t.Fatalf("undo kept %v, want only 101", result.KeptKBArticles)
	}
}
