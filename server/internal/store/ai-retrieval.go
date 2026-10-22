package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5"
	"golang.org/x/text/unicode/norm"
)

// AI retrieval finds what an AI feature may quote: published knowledge base articles in every
// language and resolved conversations, from ai_retrieval_docs (docs/internals/ai-retrieval.md).

// Kinds of AIRetrievedDoc.
const (
	AIRetrievedKBArticle    = "kb_article"
	AIRetrievedConversation = "conversation"
)

const (
	// aiRetrievalMaxTerms is how many words of a query count: the longest first, short words are the common ones.
	aiRetrievalMaxTerms = 16
	// aiRetrievalWindow is how many of the newest resolved conversations a search ranks: 10,000 took
	// 210 ms on the CI runner with the knowledge base at its cap, past the budget (docs/internals/performance.md).
	aiRetrievalWindow = 5000
	// aiWholeKBTokens: a knowledge base this small (its articles' own language) is sent whole.
	aiWholeKBTokens = 15000
	// aiKBExcerptRunes is the most of one article a search returns, about 1,500 tokens.
	aiKBExcerptRunes = 6000
	// aiConversationRunes is the most a conversation's text holds.
	aiConversationRunes = 4000
	aiCustomerRunes     = 1800
	// aiRetrievalBatch is how many queued rows one transaction of the worker builds: about 100 ms.
	// 82,000 resolved conversations took 43 s to build after the upgrade (docs/internals/ai-retrieval.md).
	aiRetrievalBatch               = 200
	aiRetrievalBatchesPerWorkspace = 25
)

// AIRetrievalQuery is what to look for. Text is the conversation's subject, the customer's newest
// message and what the person asked for; the search folds it and keeps aiRetrievalMaxTerms words.
type AIRetrievalQuery struct {
	Text string
	// ExcludeConversationID is the conversation being answered, never its own source.
	ExcludeConversationID string
	KBLimit               int
	ConversationLimit     int
}

// AIRetrievedDoc is one source. Title and Number come from the database, so a source shown to a
// person never comes from the model. Text is masked (conversations) and cut to its budget.
type AIRetrievedDoc struct {
	Kind           string
	KBArticleID    string
	KBLocale       string // "" is the article's own language
	ConversationID string
	Number         int64 // the article's number; 0 for a conversation
	Title          string
	Text           string
	Score          float64
}

// FoldForAIRetrieval lowers text and drops its accents (NFD without combining marks; đ, ł and ø,
// which have none, by hand), so a customer who types "dang nhap" finds "đăng nhập".
func FoldForAIRetrieval(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range norm.NFD.String(text) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		switch r {
		case 'đ', 'Đ':
			r = 'd'
		case 'ł', 'Ł':
			r = 'l'
		case 'ø', 'Ø':
			r = 'o'
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// aiTerms returns the words of the query as kb_search_vector splits them, the longest first.
func aiTerms(ctx context.Context, tx pgx.Tx, text string) ([]string, error) {
	var terms []string
	err := tx.QueryRow(ctx, `SELECT COALESCE(array_agg(lexeme ORDER BY length(lexeme) DESC, lexeme), '{}') FROM (
		SELECT lexeme FROM unnest(kb_search_vector($1::text)) WHERE length(lexeme) >= 2 ORDER BY length(lexeme) DESC, lexeme LIMIT $2) t`,
		FoldForAIRetrieval(text), aiRetrievalMaxTerms).Scan(&terms)
	return terms, err
}

// aiScored ranks the candidate rows docs (a query of id, source, locale and folded) by the rarity of the words they
// match: idf = ln((N + 1) / (df + 0.5)) per word, N the candidates, df those matching the word; it
// stays above zero, and a word in every candidate weighs almost nothing, so no stop word list is
// needed in any language. Words match as prefixes. $2 is the query's words; the lexemes are cast,
// never parsed, as in kbQuery. Each vector is read once per row and only there (OFFSET 0 keeps the
// copy from being inlined into every word's test): a vector in TOAST would be fetched again for every
// word, and the vectors copied into a materialized CTE would spill past work_mem. Together that was
// most of a second at 20,000 rows; folded also stays in the row when it fits (STORAGE MAIN).
func aiScored(docs string) string {
	return `q AS MATERIALIZED (SELECT t AS lexeme, ('''' || replace(replace(t, '\', '\\'), '''', '''''') || ''':*')::tsquery AS query FROM unnest($2::text[]) t),
	hits AS MATERIALIZED (SELECT d.id, d.source, d.locale, ARRAY(SELECT q.lexeme FROM q WHERE v.folded @@ q.query) AS words
		FROM (` + docs + `) d CROSS JOIN LATERAL (SELECT d.folded || ''::tsvector AS folded OFFSET 0) v),
	n AS (SELECT count(*)::float8 AS n FROM hits),
	hit AS (SELECT hits.id, hits.source, hits.locale, w AS lexeme FROM hits, unnest(hits.words) w),
	df AS (SELECT lexeme, count(*)::float8 AS df FROM hit GROUP BY lexeme),
	score AS (SELECT hit.id, min(hit.source::text) AS source, min(hit.locale) AS locale, sum(ln(((SELECT n FROM n) + 1) / (df.df + 0.5))) AS s
		FROM hit JOIN df USING (lexeme) GROUP BY hit.id)`
}

// AIRetrieve returns the knowledge base articles and resolved conversations of the workspace that
// best match the query, at most KBLimit and ConversationLimit (a small knowledge base comes whole,
// whatever KBLimit is). Run it in the workspace's scoped transaction and end that transaction
// before calling a provider. A source is checked again here, so one unpublished, excluded or
// reopened since it was built is never returned.
func AIRetrieve(ctx context.Context, tx pgx.Tx, workspaceID string, query AIRetrievalQuery) ([]AIRetrievedDoc, error) {
	terms, err := aiTerms(ctx, tx, query.Text)
	if err != nil {
		return nil, fmt.Errorf("read AI retrieval terms: %w", err)
	}
	kb, err := aiRetrieveKB(ctx, tx, workspaceID, terms, query.KBLimit)
	if err != nil {
		return nil, err
	}
	conversations, err := aiRetrieveConversations(ctx, tx, workspaceID, terms, query)
	if err != nil {
		return nil, err
	}
	return append(kb, conversations...), nil
}

// aiRecheckSlack is how many more of the best rows than asked for are checked against their source,
// so rows whose source changed since they were built (and wait in the queue) leave enough behind.
const aiRecheckSlack = `20`

// aiReadableArticle is the check of an article at search time.
const aiReadableArticle = `a.workspace_id=d.workspace_id AND a.id=d.kb_article_id AND a.status='published' AND a.deleted_at IS NULL AND NOT a.ai_excluded`

func aiRetrieveKB(ctx context.Context, tx pgx.Tx, workspaceID string, terms []string, limit int) ([]AIRetrievedDoc, error) {
	var tokens int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(sum(d.tokens),0) FROM ai_retrieval_docs d JOIN kb_articles a ON `+aiReadableArticle+`
		WHERE d.workspace_id=$1 AND d.kb_article_id IS NOT NULL AND d.kb_locale IS NULL`, workspaceID).Scan(&tokens); err != nil {
		return nil, fmt.Errorf("size the knowledge base for AI: %w", err)
	}
	whole := tokens <= aiWholeKBTokens
	if !whole && (limit <= 0 || len(terms) == 0) {
		return nil, nil
	}
	var rows pgx.Rows
	var err error
	if whole {
		// Every article in its own language, the matching ones first.
		rows, err = tx.Query(ctx, `WITH `+aiScored(`SELECT id, kb_article_id AS source, kb_locale AS locale, folded FROM ai_retrieval_docs WHERE workspace_id=$1 AND kb_article_id IS NOT NULL AND kb_locale IS NULL`)+`
			SELECT d.kb_article_id::text, '', a.number, d.title, d.body, COALESCE(score.s, 0)
			FROM ai_retrieval_docs d JOIN kb_articles a ON `+aiReadableArticle+` LEFT JOIN score ON score.id=d.id
			WHERE d.workspace_id=$1 AND d.kb_article_id IS NOT NULL AND d.kb_locale IS NULL
			ORDER BY COALESCE(score.s, 0) DESC, a.number`, workspaceID, terms)
	} else {
		// Each article once, in its best language; the text is read for the few that are returned.
		kbSQL := `WITH ` + aiScored(`SELECT id, kb_article_id AS source, kb_locale AS locale, folded FROM ai_retrieval_docs WHERE workspace_id=$1 AND kb_article_id IS NOT NULL`) + `,
			best AS (SELECT DISTINCT ON (source) id, s FROM score ORDER BY source, s DESC, locale NULLS FIRST),
			top AS (SELECT id, s FROM best ORDER BY s DESC LIMIT $3 + ` + aiRecheckSlack + `)
			SELECT d.kb_article_id::text, COALESCE(d.kb_locale, ''), a.number, d.title, d.body, top.s
			FROM top JOIN ai_retrieval_docs d ON d.workspace_id=$1 AND d.id=top.id JOIN kb_articles a ON ` + aiReadableArticle + `
			ORDER BY top.s DESC, a.number LIMIT $3`
		rows, err = tx.Query(ctx, kbSQL, workspaceID, terms, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("search the knowledge base for AI: %w", err)
	}
	docs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (AIRetrievedDoc, error) {
		doc := AIRetrievedDoc{Kind: AIRetrievedKBArticle}
		err := row.Scan(&doc.KBArticleID, &doc.KBLocale, &doc.Number, &doc.Title, &doc.Text, &doc.Score)
		return doc, err
	})
	if err != nil {
		return nil, fmt.Errorf("search the knowledge base for AI: %w", err)
	}
	if !whole {
		for i := range docs {
			docs[i].Text = aiExcerpt(docs[i].Text, terms, aiKBExcerptRunes)
		}
	}
	return docs, nil
}

func aiRetrieveConversations(ctx context.Context, tx pgx.Tx, workspaceID string, terms []string, query AIRetrievalQuery) ([]AIRetrievedDoc, error) {
	if query.ConversationLimit <= 0 || len(terms) == 0 {
		return nil, nil
	}
	conversationSQL := `WITH ` + aiScored(`SELECT id, conversation_id AS source, NULL::text AS locale, folded FROM ai_retrieval_docs
				WHERE workspace_id=$1 AND conversation_id IS NOT NULL AND conversation_id IS DISTINCT FROM $4::uuid ORDER BY updated_at DESC LIMIT $5`) + `
,
		top AS (SELECT id, s FROM score ORDER BY s DESC LIMIT $3 + ` + aiRecheckSlack + `)
		SELECT d.conversation_id::text, d.title, d.body, top.s
		FROM top JOIN ai_retrieval_docs d ON d.workspace_id=$1 AND d.id=top.id
		JOIN conversations c ON c.workspace_id=d.workspace_id AND c.id=d.conversation_id AND c.status IN ('resolved','closed')
		ORDER BY top.s DESC, d.updated_at DESC LIMIT $3`
	rows, err := tx.Query(ctx, conversationSQL, workspaceID, terms, query.ConversationLimit, optionalID(query.ExcludeConversationID), aiRetrievalWindow)
	if err != nil {
		return nil, fmt.Errorf("search resolved conversations for AI: %w", err)
	}
	docs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (AIRetrievedDoc, error) {
		doc := AIRetrievedDoc{Kind: AIRetrievedConversation}
		err := row.Scan(&doc.ConversationID, &doc.Title, &doc.Text, &doc.Score)
		return doc, err
	})
	if err != nil {
		return nil, fmt.Errorf("search resolved conversations for AI: %w", err)
	}
	return docs, nil
}

// aiExcerpt keeps the run of paragraphs of at most limit runes that holds the most query words.
func aiExcerpt(text string, terms []string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	paragraphs := strings.Split(text, "\n\n")
	scores := make([]int, len(paragraphs))
	sizes := make([]int, len(paragraphs))
	for i, paragraph := range paragraphs {
		folded := FoldForAIRetrieval(paragraph)
		for _, term := range terms {
			if strings.Contains(folded, term) {
				scores[i]++
			}
		}
		sizes[i] = utf8.RuneCountInString(paragraph) + 2
	}
	bestStart, bestEnd, bestScore := 0, 1, -1
	for start := range paragraphs {
		size, score, end := 0, 0, start
		for end < len(paragraphs) && (end == start || size+sizes[end] <= limit) {
			size += sizes[end]
			score += scores[end]
			end++
		}
		if score > bestScore {
			bestStart, bestEnd, bestScore = start, end, score
		}
	}
	excerpt := []rune(strings.Join(paragraphs[bestStart:bestEnd], "\n\n"))
	if len(excerpt) > limit {
		excerpt = excerpt[:limit]
	}
	return string(excerpt)
}

// aiContact is who wrote in to a conversation, and the contacts merged into it: the details masked
// out of its text.
type aiContact struct {
	names, organizations []string
}

var (
	aiEmailPattern = regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}`)
	// Six or more digits, which may be split by spaces, dots, dashes, slashes or brackets: phone,
	// account and card numbers ("090 123 4567", "+84 90-123-4567").
	aiNumberPattern = regexp.MustCompile(`\+?\d(?:[ .\-/()]{0,2}\d){5,}`)
)

// maskAIText replaces every email address, the contact's companies and whole names in any case, with
// or without accents, each word of a name on its own, and every long number. A word alone counts only
// capitalized as a name is ("Long", "Tran" for Trần): "long" and "an" stay words, so a team writing
// in English about a customer named Trần Văn Long keeps its text.
func maskAIText(text string, contact aiContact) string {
	text = aiEmailPattern.ReplaceAllString(text, "[email]")
	text = replaceWords(text, contact.organizations, "[company]", true)
	text = replaceWords(text, contact.names, "[customer]", true)
	var words []string
	for _, name := range contact.names {
		for _, word := range strings.FieldsFunc(name, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }) {
			if utf8.RuneCountInString(word) >= 2 {
				words = append(words, capitalized(word), capitalized(FoldForAIRetrieval(word)))
			}
		}
	}
	text = replaceWords(text, words, "[customer]", false)
	return aiNumberPattern.ReplaceAllString(text, "[number]")
}

func capitalized(word string) string {
	first, size := utf8.DecodeRuneInString(word)
	return string(unicode.ToUpper(first)) + strings.ToLower(word[size:])
}

// replaceWords replaces each phrase where it stands as whole words; ignoreCase also ignores case and accents.
func replaceWords(text string, phrases []string, with string, ignoreCase bool) string {
	var alternatives []string
	for _, phrase := range phrases {
		phrase = strings.TrimSpace(phrase)
		if phrase == "" {
			continue
		}
		alternatives = append(alternatives, regexp.QuoteMeta(phrase))
		if ignoreCase {
			alternatives = append(alternatives, regexp.QuoteMeta(FoldForAIRetrieval(phrase)))
		}
	}
	if len(alternatives) == 0 {
		return text
	}
	// The longest first, so a whole name goes before its words.
	slices.SortFunc(alternatives, func(a, b string) int { return len(b) - len(a) })
	flags := ""
	if ignoreCase {
		flags = "(?i)"
	}
	pattern := regexp.MustCompile(flags + `(^|[^\p{L}\p{N}])(` + strings.Join(alternatives, "|") + `)([^\p{L}\p{N}]|$)`)
	// A match takes the character after it, so run again for words that follow each other.
	for range 4 {
		next := pattern.ReplaceAllString(text, "${1}"+with+"${3}")
		if next == text {
			break
		}
		text = next
	}
	return text
}

// cutRunes keeps the first limit runes of text.
func cutRunes(text string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	return string([]rune(text)[:limit])
}

func aiTokens(text string) int {
	return (utf8.RuneCountInString(text) + 3) / 4
}

// BuildAIRetrieval builds the queued sources of every workspace and returns how many it built. A
// workspace another process is building is left to it, and one with more than a few thousand queued
// goes on at the next call.
func (s *Store) BuildAIRetrieval(ctx context.Context) (int, error) {
	var workspaces []string
	// Only the ids of workspaces with queued rows cross workspaces; each is built in its own scope.
	err := s.db.Privileged(ctx, database.PrivilegeAIRetrievalQueue, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `WITH RECURSIVE w AS (
				(SELECT workspace_id FROM ai_retrieval_pending ORDER BY workspace_id LIMIT 1)
				UNION ALL
				SELECT (SELECT p.workspace_id FROM ai_retrieval_pending p WHERE p.workspace_id > w.workspace_id ORDER BY p.workspace_id LIMIT 1) FROM w WHERE w.workspace_id IS NOT NULL)
			SELECT workspace_id::text FROM w WHERE workspace_id IS NOT NULL`)
		if err != nil {
			return err
		}
		workspaces, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("list workspaces with AI retrieval to build: %w", err)
	}
	built := 0
	var failed []error
	for _, workspaceID := range workspaces {
		// A workspace with a large import queued yields to the others after a few seconds; the next
		// call goes on with it.
		for range aiRetrievalBatchesPerWorkspace {
			n, more, err := s.buildAIRetrievalBatch(ctx, workspaceID)
			built += n
			if err != nil {
				// One workspace that fails (its rows stay queued) does not hold up the others.
				failed = append(failed, err)
				break
			}
			if !more || ctx.Err() != nil {
				break
			}
		}
		if ctx.Err() != nil {
			break
		}
	}
	return built, errors.Join(append(failed, ctx.Err())...)
}

// buildAIRetrievalBatch builds the oldest queued rows of one workspace and deletes them; more says
// a full batch was read.
func (s *Store) buildAIRetrievalBatch(ctx context.Context, workspaceID string) (built int, more bool, err error) {
	err = s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		var locked bool
		if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended('ai_retrieval:' || $1::text, 0))`, workspaceID).Scan(&locked); err != nil || !locked {
			return err
		}
		var ids []string
		var articles, conversations []string
		rows, err := tx.Query(ctx, `SELECT id::text, kb_article_id::text, conversation_id::text FROM ai_retrieval_pending WHERE workspace_id=$1 ORDER BY created_at, id LIMIT $2`,
			workspaceID, aiRetrievalBatch)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			var article, conversation *string
			if err := rows.Scan(&id, &article, &conversation); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
			if article != nil && !slices.Contains(articles, *article) {
				articles = append(articles, *article)
			}
			if conversation != nil && !slices.Contains(conversations, *conversation) {
				conversations = append(conversations, *conversation)
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		if err := buildAIArticles(ctx, tx, workspaceID, articles); err != nil {
			return err
		}
		if err := buildAIConversations(ctx, tx, workspaceID, conversations); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM ai_retrieval_pending WHERE workspace_id=$1 AND id=ANY($2::uuid[])`, workspaceID, ids); err != nil {
			return err
		}
		built, more = len(articles)+len(conversations), len(ids) == aiRetrievalBatch
		return nil
	})
	if err != nil {
		return 0, false, fmt.Errorf("build AI retrieval of workspace %s: %w", workspaceID, err)
	}
	return built, more, nil
}

// aiDocRows are retrieval rows to write, column by column.
type aiDocRows struct {
	articles, locales, conversations []*string
	titles, bodies, folded           []string
	tokens                           []int
	updated                          []time.Time
}

func (r *aiDocRows) add(article, locale, conversation *string, title, body string, updated time.Time) {
	r.articles, r.locales, r.conversations = append(r.articles, article), append(r.locales, locale), append(r.conversations, conversation)
	r.titles, r.bodies = append(r.titles, title), append(r.bodies, body)
	r.folded = append(r.folded, FoldForAIRetrieval(title+"\n"+body))
	r.tokens, r.updated = append(r.tokens, aiTokens(title)+aiTokens(body)), append(r.updated, updated)
}

func (r *aiDocRows) insert(ctx context.Context, tx pgx.Tx, workspaceID string) error {
	if len(r.titles) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO ai_retrieval_docs(workspace_id,kb_article_id,kb_locale,conversation_id,title,body,folded,tokens,updated_at)
		SELECT $1, x.article::uuid, x.locale, x.conversation::uuid, x.title, x.body, kb_search_vector(x.folded), x.tokens, x.updated
		FROM unnest($2::text[], $3::text[], $4::text[], $5::text[], $6::text[], $7::text[], $8::int[], $9::timestamptz[]) AS x(article, locale, conversation, title, body, folded, tokens, updated)`,
		workspaceID, r.articles, r.locales, r.conversations, r.titles, r.bodies, r.folded, r.tokens, r.updated)
	return err
}

// buildAIArticles replaces the rows of the articles: one per language of each that AI may read.
func buildAIArticles(ctx context.Context, tx pgx.Tx, workspaceID string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `DELETE FROM ai_retrieval_docs WHERE workspace_id=$1 AND kb_article_id=ANY($2::uuid[])`, workspaceID, ids); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT a.id::text, NULL::text, a.title, a.body_md, a.updated_at FROM kb_articles a
			WHERE a.workspace_id=$1 AND a.id=ANY($2::uuid[]) AND a.status='published' AND a.deleted_at IS NULL AND NOT a.ai_excluded
		UNION ALL
		SELECT a.id::text, t.locale, t.title, t.body_md, t.updated_at FROM kb_articles a JOIN kb_article_translations t ON t.workspace_id=a.workspace_id AND t.article_id=a.id
			WHERE a.workspace_id=$1 AND a.id=ANY($2::uuid[]) AND a.status='published' AND a.deleted_at IS NULL AND NOT a.ai_excluded`, workspaceID, ids)
	if err != nil {
		return err
	}
	var docs aiDocRows
	for rows.Next() {
		var article string
		var locale *string
		var title, body string
		var updated time.Time
		if err := rows.Scan(&article, &locale, &title, &body, &updated); err != nil {
			rows.Close()
			return err
		}
		docs.add(&article, locale, nil, title, body, updated)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return docs.insert(ctx, tx, workspaceID)
}

// buildAIConversations replaces the rows of the conversations: the subject, the first customer
// message and the last reply of each resolved or closed one, masked. Private notes never count. One
// without a customer message yet (an import writes messages after the conversation) waits for it:
// the message queues the conversation again.
func buildAIConversations(ctx context.Context, tx pgx.Tx, workspaceID string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `DELETE FROM ai_retrieval_docs WHERE workspace_id=$1 AND conversation_id=ANY($2::uuid[])`, workspaceID, ids); err != nil {
		return err
	}
	// The names and companies of the contact and of the contacts merged into it: a merge moves their
	// conversations, whose text still names them.
	rows, err := tx.Query(ctx, `SELECT c.id::text, c.subject,
			ARRAY(SELECT x.name FROM contacts x WHERE x.workspace_id=c.workspace_id AND (x.id=c.contact_id OR x.merged_into_id=c.contact_id)),
			ARRAY(SELECT x.organization FROM contacts x WHERE x.workspace_id=c.workspace_id AND (x.id=c.contact_id OR x.merged_into_id=c.contact_id) AND x.organization IS NOT NULL),
			c.updated_at,
			(SELECT m.body FROM messages m WHERE m.workspace_id=c.workspace_id AND m.conversation_id=c.id AND m.kind='incoming' ORDER BY m.created_at, m.id LIMIT 1),
			(SELECT m.body FROM messages m WHERE m.workspace_id=c.workspace_id AND m.conversation_id=c.id AND m.kind='outgoing' ORDER BY m.created_at DESC, m.id DESC LIMIT 1)
		FROM conversations c
		WHERE c.workspace_id=$1 AND c.id=ANY($2::uuid[]) AND c.status IN ('resolved','closed')`, workspaceID, ids)
	if err != nil {
		return err
	}
	var docs aiDocRows
	for rows.Next() {
		var id, subject string
		var contact aiContact
		var updated time.Time
		var incoming, reply *string
		if err := rows.Scan(&id, &subject, &contact.names, &contact.organizations, &updated, &incoming, &reply); err != nil {
			rows.Close()
			return err
		}
		if incoming == nil {
			continue
		}
		body := "Customer: " + cutRunes(strings.TrimSpace(*incoming), aiCustomerRunes)
		if reply != nil {
			body += "\n\nReply: " + strings.TrimSpace(*reply)
		}
		title := cutRunes(maskAIText(subject, contact), 300)
		docs.add(nil, nil, &id, title, cutRunes(maskAIText(body, contact), aiConversationRunes-utf8.RuneCountInString(title)), updated)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return docs.insert(ctx, tx, workspaceID)
}

// RunAIRetrieval builds queued sources every interval until ctx ends.
func (s *Store) RunAIRetrieval(ctx context.Context, interval time.Duration, logError func(error)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := s.BuildAIRetrieval(ctx); err != nil && !errors.Is(err, context.Canceled) && ctx.Err() == nil {
			logError(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
