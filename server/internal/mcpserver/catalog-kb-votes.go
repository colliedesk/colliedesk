package mcpserver

import (
	"net/http"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// kbVotesOutput is the votes of one article as a tool answers them: what a reader wrote, and a signed-in
// reader's name, sit under untrusted_customer_content.
const kbVotesOutput = `{"type":"object","required":["article_id","helpful","not_helpful","imported","votes","resets"],"properties":{` +
	`"article_id":{"type":"string"},"helpful":{"type":"integer"},"not_helpful":{"type":"integer"},` +
	`"imported":{"type":"object","required":["helpful","not_helpful"],"description":"The part of the counts an import brought in.","properties":{"helpful":{"type":"integer"},"not_helpful":{"type":"integer"}}},` +
	`"votes":{"type":"array","items":{"type":"object","required":["helpful","contact_id","created_at","updated_at","untrusted_customer_content"],"properties":{` +
	`"helpful":{"type":"boolean"},"contact_id":{"type":["string","null"],"description":"The signed-in contact who voted; null for an anonymous reader."},` +
	`"created_at":{"type":"string"},"updated_at":{"type":"string"},` +
	`"untrusted_customer_content":{"type":"object","required":["missing","contact_name"],"properties":{` +
	`"missing":{"type":["string","null"],"description":"What the reader said was missing, emails and long numbers removed."},"contact_name":{"type":["string","null"]}}}}}},` +
	`"resets":{"type":"array","description":"Earlier counts, newest first, each kept when the votes were reset.","items":{"type":"object","required":["id","helpful","not_helpful","created_at"],"properties":{` +
	`"id":{"type":"string"},"helpful":{"type":"integer"},"not_helpful":{"type":"integer"},"created_at":{"type":"string"},` +
	`"reset_by":{"type":["object","null"],"properties":{"agent_id":{"type":["string","null"]},"name":{"type":"string"}}},` +
	`"token":{"type":["object","null"],"properties":{"id":{"type":["string","null"]},"label":{"type":"string"}}}}}}}}`

func kbArticleVotesGetSpec() Spec {
	return Spec{
		Name:  "colliedesk_kb_article_votes_get",
		Title: "Read what readers thought of a knowledge base article",
		Description: `Answer the "Was this article helpful?" votes of one article: the counts its help center page shows (votes imported from Freshdesk included), ` +
			`each reader's current vote with what they said was missing, and the earlier counts kept by each reset. ` +
			`Use it to find what an article lacks before you edit it with colliedesk_kb_article_update.` + "\n\n" + untrustedNotice,
		REST:   []string{"GET /api/v1/kb/articles/{id}/votes"},
		Input:  `{"type":"object","additionalProperties":false,"required":["article_id"],"properties":{` + kbArticleIDProperty + `}}`,
		Output: kbVotesOutput,
		run:    kbArticleVotesGet,
	}
}

func kbArticleVotesResetSpec() Spec {
	return Spec{
		Name:  "colliedesk_kb_article_votes_reset",
		Title: "Reset the votes of a knowledge base article",
		Description: `Start the article's "Was this article helpful?" count again from 0, for example after rewriting it. ` +
			`The current votes and the lines readers wrote stop counting and leave this answer, which cannot be undone; the counts the page showed are kept under resets with who reset them. ` +
			`Calling it again with nothing counted changes nothing.` + "\n\n" + untrustedNotice,
		Idempotent: true,
		REST:       []string{"POST /api/v1/kb/articles/{id}/votes/reset"},
		Input:      `{"type":"object","additionalProperties":false,"required":["article_id"],"properties":{` + kbArticleIDProperty + `}}`,
		Output:     kbVotesOutput,
		run:        kbArticleVotesReset,
	}
}

func kbArticleVotesGet(c *call, arguments map[string]any) (any, error) {
	return kbArticleVotes(c, arguments, http.MethodGet)
}

func kbArticleVotesReset(c *call, arguments map[string]any) (any, error) {
	return kbArticleVotes(c, arguments, http.MethodPost)
}

func kbArticleVotes(c *call, arguments map[string]any, method string) (any, error) {
	args, err := decodeArguments[struct {
		ID string `json:"article_id"`
	}](arguments)
	if err != nil {
		return nil, err
	}
	c.forArticle(args.ID)
	var votes domain.KBArticleVotes
	if method == http.MethodGet {
		err = c.get(kbArticlePath(args.ID, "/votes"), nil, &votes)
	} else {
		err = c.send(method, kbArticlePath(args.ID, "/votes/reset"), nil, &votes)
	}
	if err != nil {
		return nil, err
	}
	return kbVotesAnswer(votes), nil
}

func kbVotesAnswer(votes domain.KBArticleVotes) map[string]any {
	readers := make([]map[string]any, 0, len(votes.Votes))
	for _, vote := range votes.Votes {
		var contactID, contactName *string
		if vote.Contact != nil {
			contactID, contactName = &vote.Contact.ID, &vote.Contact.Name
		}
		readers = append(readers, map[string]any{
			"helpful": vote.Helpful, "contact_id": contactID, "created_at": vote.CreatedAt, "updated_at": vote.UpdatedAt,
			"untrusted_customer_content": map[string]any{"missing": vote.Missing, "contact_name": contactName},
		})
	}
	resets := votes.Resets
	if resets == nil {
		resets = []domain.KBArticleVoteReset{}
	}
	return map[string]any{
		"article_id": votes.ArticleID, "helpful": votes.Helpful, "not_helpful": votes.NotHelpful,
		"imported": votes.Imported, "votes": readers, "resets": resets,
	}
}
