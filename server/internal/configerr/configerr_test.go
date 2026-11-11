package configerr_test

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/configerr"
)

func TestRenderFillsNamesQuotesAndPlurals(t *testing.T) {
	got := configerr.Render(`{name} has {count} {count?model|models}, not {"key"}`, configerr.Params{"name": "x", "count": 1, "key": "a b"})
	if got != `x has 1 model, not "a b"` {
		t.Fatal(got)
	}
	if got := configerr.Render("{count} {count?model|models}", configerr.Params{"count": 3}); got != "3 models" {
		t.Fatal(got)
	}
}

func TestLeavesAndAsFindEveryCodedErrorOfAJoin(t *testing.T) {
	first := configerr.Say("ai_server_provider_invalid", nil)
	joined := errors.Join(first, errors.Join(errors.New("plain"), fmt.Errorf("prefix: %w", first)))
	leaves := configerr.Leaves(joined)
	if len(leaves) != 3 {
		t.Fatalf("leaves: %v", leaves)
	}
	if coded, ok := configerr.As(leaves[0]); !ok || coded.Code != "ai_server_provider_invalid" {
		t.Fatal("the coded leaf lost its code")
	}
	if _, ok := configerr.As(leaves[1]); ok {
		t.Fatal("a plain error has no code")
	}
	// A wrapper that adds words says more than its code: it is not coded.
	if _, ok := configerr.As(leaves[2]); ok {
		t.Fatal("a wrapper with words of its own kept the code")
	}
}

// Every code the server says is in the table of templates, and every template is said somewhere.
func TestEveryCodeSaidIsATemplateAndEveryTemplateIsSaid(t *testing.T) {
	said := regexp.MustCompile(`(?:configerr\.Say|\brefuseCoded|\.previews|\.refuses)\("([a-z0-9_]+)"`)
	used := map[string]bool{}
	err := filepath.WalkDir("../..", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range said.FindAllStringSubmatch(string(data), -1) {
			code := match[1]
			used[code] = true
			if _, ok := configerr.Templates[code]; !ok {
				t.Errorf("%s says %s, which has no template", path, code)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for code := range configerr.Templates {
		if !used[code] {
			t.Errorf("%s has a template nobody says", code)
		}
	}
}
