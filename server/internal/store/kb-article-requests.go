package store

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/textmask"
)

// kbSourceConversation is a conversation an article is asked or made from, with what is masked out
// of the text taken from it: the customer's names and companies and the team's names.
type kbSourceConversation struct {
	id, subject, displayRef, contactKey string
	contact                             aiContact
	team                                []string
}

// mask replaces every email address and run of 6 or more digits, the customer's names and companies
// and the team members' names in text.
func (c kbSourceConversation) mask(text string) string {
	text = maskAIText(textmask.Mask(text), c.contact)
	return replaceWords(text, c.team, "[team member]", true)
}

// readKBSourceConversation reads one conversation of the workspace for masking;
// domain.ErrKBSourceConversationNotFound for one of another workspace.
func readKBSourceConversation(ctx context.Context, tx pgx.Tx, workspaceID, id string) (kbSourceConversation, error) {
	c := kbSourceConversation{id: id}
	if !savedReplyID.MatchString(id) {
		return c, domain.ErrKBSourceConversationNotFound
	}
	err := tx.QueryRow(ctx, `SELECT c.subject, COALESCE(c.display_ref,''), COALESCE(c.contact_id, c.id)::text,
			ARRAY(SELECT x.name FROM contacts x WHERE x.workspace_id=c.workspace_id AND (x.id=c.contact_id OR x.merged_into_id=c.contact_id) AND x.name IS NOT NULL
				UNION SELECT f.value FROM ai_retrieval_former_names f JOIN contacts x ON x.workspace_id=f.workspace_id AND x.id=f.contact_id
				WHERE f.workspace_id=c.workspace_id AND f.kind='name' AND (x.id=c.contact_id OR x.merged_into_id=c.contact_id)),
			ARRAY(SELECT x.organization FROM contacts x WHERE x.workspace_id=c.workspace_id AND (x.id=c.contact_id OR x.merged_into_id=c.contact_id) AND x.organization IS NOT NULL
				UNION SELECT f.value FROM ai_retrieval_former_names f JOIN contacts x ON x.workspace_id=f.workspace_id AND x.id=f.contact_id
				WHERE f.workspace_id=c.workspace_id AND f.kind='organization' AND (x.id=c.contact_id OR x.merged_into_id=c.contact_id)),
			ARRAY(SELECT a.display_name FROM agents a WHERE a.workspace_id=c.workspace_id AND a.display_name IS NOT NULL AND a.display_name <> '')
		FROM conversations c WHERE c.workspace_id=$1 AND c.id=$2`, workspaceID, id).
		Scan(&c.subject, &c.displayRef, &c.contactKey, &c.contact.names, &c.contact.organizations, &c.team)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, domain.ErrKBSourceConversationNotFound
	}
	return c, err
}

// kbRequestText trims a request's title or note and refuses one longer than
// domain.MaxKBArticleRequestText; "" when it is left out or blank.
func kbRequestText(field string, value *string) (string, error) {
	if value == nil {
		return "", nil
	}
	text := strings.Join(strings.Fields(*value), " ")
	if utf8.RuneCountInString(text) > domain.MaxKBArticleRequestText {
		return "", &domain.InvalidKBError{Message: field + " must be at most 300 characters"}
	}
	return text, nil
}

// RequestKBArticle files the conversation's request for an article as a knowledge gap signal of kind
// requested: in the open gap input.GapID names, else the open gap whose words are close to the
// title, else a new gap. The title (the subject when left out) and the note are masked. A request
// asked again with the same words and note changes nothing (Created false); other words move it.
func (s *Store) RequestKBArticle(ctx context.Context, workspaceID, conversationID string, input domain.KBArticleRequestInput) (domain.KBArticleRequest, error) {
	title, err := kbRequestText("title", input.Title)
	if err != nil {
		return domain.KBArticleRequest{}, err
	}
	note, err := kbRequestText("note", input.Note)
	if err != nil {
		return domain.KBArticleRequest{}, err
	}
	gapID := ""
	if input.GapID != nil {
		if gapID = strings.TrimSpace(*input.GapID); !savedReplyID.MatchString(gapID) {
			return domain.KBArticleRequest{}, domain.ErrKBGapNotFound
		}
	}
	request, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBArticleRequest, error) {
		// The worker places signals under the same lock: two requests never open two gaps for one question.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('kb_gaps:' || $1::text, 0))`, workspaceID); err != nil {
			return domain.KBArticleRequest{}, err
		}
		conversation, err := readKBSourceConversation(ctx, tx, workspaceID, conversationID)
		if err != nil {
			return domain.KBArticleRequest{}, err
		}
		return fileKBArticleRequest(ctx, tx, workspaceID, conversation, title, note, gapID)
	})
	return request, kbError("request an article", err)
}

func fileKBArticleRequest(ctx context.Context, tx pgx.Tx, workspaceID string, conversation kbSourceConversation, title, note, gapID string) (domain.KBArticleRequest, error) {
	if gapID != "" {
		var gapTitle string
		err := tx.QueryRow(ctx, `SELECT title FROM kb_gaps WHERE workspace_id=$1 AND id=$2 AND status='open'`, workspaceID, gapID).Scan(&gapTitle)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.KBArticleRequest{}, domain.ErrKBGapNotFound
		}
		if err != nil {
			return domain.KBArticleRequest{}, err
		}
		if title == "" {
			// Added to a request without words of its own: it asks the same.
			title = gapTitle
		}
	}
	if title == "" {
		title = conversation.subject
	}
	text := cutRunes(strings.Join(strings.Fields(conversation.mask(title)), " "), kbGapTextRunes)
	key := kbGapKey(text)
	if len(kbGapTerms(key)) == 0 {
		return domain.KBArticleRequest{}, &domain.InvalidKBError{Message: "title must have words: say what the article should answer"}
	}
	var maskedNote *string
	if note != "" {
		masked := cutRunes(conversation.mask(note), domain.MaxKBArticleRequestText)
		maskedNote = &masked
	}

	var oldGap, oldText string
	var oldNote *string
	err := tx.QueryRow(ctx, `SELECT gap_id::text, text, note FROM kb_gap_signals WHERE workspace_id=$1 AND kind='requested' AND source_id=$2`,
		workspaceID, conversation.id).Scan(&oldGap, &oldText, &oldNote)
	switch {
	case err == nil && oldText == text && sameNote(oldNote, maskedNote) && (gapID == "" || gapID == oldGap):
		gap, err := readKBGapRow(ctx, tx, workspaceID, oldGap)
		return domain.KBArticleRequest{Gap: gap, AddedToExisting: true}, err
	case err == nil:
		// The old gap is counted again only after the new signal is in: a request moved within its
		// own gap must not see that gap removed for having no signal left.
		if _, err := tx.Exec(ctx, `DELETE FROM kb_gap_signals WHERE workspace_id=$1 AND kind='requested' AND source_id=$2`, workspaceID, conversation.id); err != nil {
			return domain.KBArticleRequest{}, err
		}
	case errors.Is(err, pgx.ErrNoRows):
		oldGap = ""
	default:
		return domain.KBArticleRequest{}, err
	}

	if gapID == "" {
		if gapID, err = findOpenKBGap(ctx, tx, workspaceID, key); err != nil {
			return domain.KBArticleRequest{}, err
		}
	}
	added := gapID != ""
	now := time.Now().UTC()
	if !added {
		if err := tx.QueryRow(ctx, `INSERT INTO kb_gaps(workspace_id, title, query_key, first_seen, last_seen)
			VALUES ($1, $2, $3, $4, $4) RETURNING id::text`, workspaceID, text, key, now).Scan(&gapID); err != nil {
			return domain.KBArticleRequest{}, err
		}
	}
	asker := kbGapAsker("contact", []byte(conversation.contactKey))
	if _, err := tx.Exec(ctx, `INSERT INTO kb_gap_signals(workspace_id, gap_id, kind, source_id, text, note, asker_key, created_at)
		VALUES ($1, $2, 'requested', $3, $4, $5, $6, $7)`, workspaceID, gapID, conversation.id, text, maskedNote, asker[:], now); err != nil {
		return domain.KBArticleRequest{}, err
	}
	recounts := []string{gapID}
	if oldGap != "" && oldGap != gapID {
		recounts = append(recounts, oldGap)
	}
	for _, recount := range recounts {
		if _, err := tx.Exec(ctx, `SELECT kb_gap_recount($1, $2)`, workspaceID, recount); err != nil {
			return domain.KBArticleRequest{}, err
		}
	}
	gap, err := readKBGapRow(ctx, tx, workspaceID, gapID)
	return domain.KBArticleRequest{Gap: gap, Created: true, AddedToExisting: added}, err
}

func sameNote(a, b *string) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

// findOpenKBGap is the open gap whose folded words are key, else the best open gap scoring at least
// KBGapMatchThreshold against key; "" when there is none. A request never joins a fixed or
// dismissed gap: asking again means the answer is still missing.
func findOpenKBGap(ctx context.Context, tx pgx.Tx, workspaceID, key string) (string, error) {
	var gap string
	err := tx.QueryRow(ctx, `SELECT id::text FROM kb_gaps WHERE workspace_id=$1 AND query_key=$2 AND status='open' ORDER BY created_at, id LIMIT 1`,
		workspaceID, key).Scan(&gap)
	if !errors.Is(err, pgx.ErrNoRows) {
		return gap, err
	}
	matches, err := similarOpenKBGaps(ctx, tx, workspaceID, key)
	if err != nil || len(matches) == 0 {
		return "", err
	}
	return matches[0], nil
}

// WithdrawKBArticleRequest takes back the conversation's request; the gap counts again and goes when
// it has no signal left. domain.ErrKBArticleRequestNotFound when the conversation asked for none.
func (s *Store) WithdrawKBArticleRequest(ctx context.Context, workspaceID, conversationID string) error {
	_, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (struct{}, error) {
		if _, err := readKBSourceConversation(ctx, tx, workspaceID, conversationID); err != nil {
			return struct{}{}, err
		}
		var gap string
		err := tx.QueryRow(ctx, `DELETE FROM kb_gap_signals WHERE workspace_id=$1 AND kind='requested' AND source_id=$2 RETURNING gap_id::text`,
			workspaceID, conversationID).Scan(&gap)
		if errors.Is(err, pgx.ErrNoRows) {
			return struct{}{}, domain.ErrKBArticleRequestNotFound
		}
		if err != nil {
			return struct{}{}, err
		}
		_, err = tx.Exec(ctx, `SELECT kb_gap_recount($1, $2)`, workspaceID, gap)
		return struct{}{}, err
	})
	return kbError("take back an article request", err)
}
