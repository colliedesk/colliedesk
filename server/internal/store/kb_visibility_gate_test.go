package store

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// directPublishedRead matches a query that decides on its own that an article or a translation is
// published: status='published', or status IN (... 'published' ...).
var directPublishedRead = regexp.MustCompile(`(?i)status\s*(=\s*'published'|in\s*\([^)]*'published')`)

// teamSideKBFiles read the status column for the team's own screens and writes, never for what a customer
// sees. Every other file reads published articles through the visibility views (kb_public_articles,
// kb_help_chat_articles, kb_published_articles and their translations).
var teamSideKBFiles = map[string]string{
	"store/kb-articles.go":            "the team's article list, edits and publishing",
	"store/kb-translations.go":        "the team's translations and their counts",
	"store/kb-review-edits.go":        "the version a review compares with",
	"store/kb-review-translations.go": "the version a translation review compares with",
	"store/kb-public.go":              "whether a change needs kb_articles:publish",
	"store/kb-visibility.go":          "which published articles a change shows to more people",
	"store/setup-ai-and-targets.go":   "the setup step that asks for a first published article",
}

func directPublishedReads(source string) []string {
	var found []string
	for i, line := range strings.Split(source, "\n") {
		if directPublishedRead.MatchString(line) {
			found = append(found, strings.TrimSpace(line)+" (line "+strconv.Itoa(i+1)+")")
		}
	}
	return found
}

func TestTheVisibilityGateFlagsADirectPublishedRead(t *testing.T) {
	leaky := "rows, err := tx.Query(ctx, `SELECT a.title FROM kb_articles a WHERE a.workspace_id=$1 AND a.status = 'published'`)"
	if got := directPublishedReads(leaky); len(got) != 1 {
		t.Fatalf("a direct read of the status was not flagged: %v", got)
	}
	translated := "JOIN kb_article_translations t ON t.article_id=a.id AND t.status IN ('draft', 'published')"
	if got := directPublishedReads(translated); len(got) != 1 {
		t.Fatalf("a status list naming published was not flagged: %v", got)
	}
	viewed := "WHERE EXISTS (SELECT 1 FROM kb_public_articles p WHERE p.workspace_id=a.workspace_id AND p.id=a.id)"
	if got := directPublishedReads(viewed); len(got) != 0 {
		t.Fatalf("a read through the view was flagged: %v", got)
	}
}

// Only the team's own files may read whether an article is published; every reader of the help center,
// help chat or AI goes through the visibility views, so an article kept for the team never leaks.
func TestOnlyTeamSideFilesReadThePublishedStatusDirectly(t *testing.T) {
	root := ".."
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		name := filepath.ToSlash(strings.TrimPrefix(path, root+string(filepath.Separator)))
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if found := directPublishedReads(string(source)); len(found) > 0 {
			if _, teamSide := teamSideKBFiles[name]; !teamSide {
				t.Errorf("%s reads the published status directly; read kb_public_articles, kb_help_chat_articles or kb_published_articles instead:\n%s",
					name, strings.Join(found, "\n"))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
