package store

// The conditions every reader outside the team uses to decide which articles and translations it may
// read. They go through the visibility views (migration kb-visibility), never the status column, so an
// article kept for the team or for help chat cannot leak through a query that forgot it.

// publicKBArticle holds for an article a of workspace $1 the help center shows: published, outside the
// trash and visible to everyone through every collection it is in.
const publicKBArticle = `a.workspace_id=$1 AND EXISTS (SELECT 1 FROM kb_help_center_public_articles kv WHERE kv.workspace_id=a.workspace_id AND kv.id=a.id)`

// helpChatKBArticle holds for an article a of workspace $1 help chat may read: one the help center
// shows, or one kept for help chat. It reads kb_help_center_chat_articles, which returns rows only in
// a help chat transaction (database.InHelpChat), so it holds for the reader of the chat's help center
// alone, whose policies keep that help center's articles.
const helpChatKBArticle = `a.workspace_id=$1 AND EXISTS (SELECT 1 FROM kb_help_center_chat_articles kv WHERE kv.workspace_id=a.workspace_id AND kv.id=a.id)`

// shownArticleKBTranslation holds for a translation t of an article a the help center shows once the
// same statement holds publicKBArticle for a: a published translation outside the trash is all
// kb_help_center_public_translations adds to its article. Checking that view instead, in a list of
// articles, makes the planner expect one row per collection and run the whole join for each.
const shownArticleKBTranslation = `EXISTS (SELECT 1 FROM kb_published_translations kt WHERE kt.workspace_id=t.workspace_id AND kt.article_id=t.article_id AND kt.locale=t.locale)`

// publishedKBTranslation holds for a published translation t of a published article, whoever may read
// it: what the team counts and its AI drafts read.
const publishedKBTranslation = `EXISTS (SELECT 1 FROM kb_published_translations kt WHERE kt.workspace_id=t.workspace_id AND kt.article_id=t.article_id AND kt.locale=t.locale)`

// chatArticleKBTranslation holds for a translation t (its language aside) of an article a once the
// same statement joins kb_help_center_chat_articles for a: a published translation outside the trash
// is all kb_help_center_chat_translations adds to its article. That view decides the article again
// and computes every collection's rank once more; kb_published_translations does not.
const chatArticleKBTranslation = `EXISTS (SELECT 1 FROM kb_published_translations kt WHERE kt.workspace_id=t.workspace_id AND kt.article_id=t.article_id AND kt.locale=t.locale)`

// publicKBArticleFlag is true for an article a the help center shows, so a reader that may read more
// (help chat) knows which ones it may link.
const publicKBArticleFlag = `EXISTS (SELECT 1 FROM kb_help_center_public_articles kp WHERE kp.workspace_id=a.workspace_id AND kp.id=a.id)`

// publicKBCollection holds for a collection c the help center shows: outside the trash and visible to
// everyone, its parents included.
const publicKBCollection = `EXISTS (SELECT 1 FROM kb_collection_visibility kc WHERE kc.workspace_id=c.workspace_id AND kc.id=c.id AND kc.visibility_rank=0)`
