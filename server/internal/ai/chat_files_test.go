package ai_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/ai"
)

// The help chat reads the text of the files an article lists with that article, each headed with its
// name, its type and the article; a new file text is a new knowledge base, so the cached prompt changes.
func TestTheChatReadsTheTextOfTheFilesAnArticleLists(t *testing.T) {
	e := newEnv(t, nil)
	kb := newKB(t, e, e.a)
	returns := kb.article(t, e.a, 1, "Returns policy", "Send it back.", "published", false, 1)
	ask := func() ai.ChatPrompt {
		t.Helper()
		prompt, err := e.service.ChatPrompt(context.Background(), chatBinding(e), e.a.id, ai.ChatPromptInput{WorkspaceName: "Acme", Question: "Which form?"})
		if err != nil {
			t.Fatal(err)
		}
		return prompt
	}
	before := ask()
	var media string
	if err := e.pool.QueryRow(context.Background(), `INSERT INTO kb_media(workspace_id,kind,storage_key,sha256,size,content_type,name,text)
		VALUES($1,'file','kb/form',repeat('a',64),4096,'application/pdf','Warranty form.pdf','Fill in the zephyrine warranty form') RETURNING id::text`, e.a.id).Scan(&media); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(context.Background(), `INSERT INTO kb_article_attachments(workspace_id,article_id,media_id,position) VALUES($1,$2,$3,0)`, e.a.id, returns, media); err != nil {
		t.Fatal(err)
	}
	after := ask()
	system := systemText(after.Request)
	if !after.Whole || !strings.Contains(system, "## Warranty form (PDF) in Returns policy") || !strings.Contains(system, "zephyrine warranty form") {
		t.Fatalf("the chat does not read the file's text: %s", system)
	}
	if bytes.Equal(before.Fingerprint, after.Fingerprint) {
		t.Errorf("a file's text was added and the knowledge base fingerprint stayed the same")
	}
}
