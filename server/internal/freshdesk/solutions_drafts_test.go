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
