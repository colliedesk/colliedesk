package store

// The conditions every reader outside the team uses to decide which articles and translations it may
// read. They go through the visibility views (migration kb-visibility), never the status column, so an
// article kept for the team or for help chat cannot leak through a query that forgot it.

// publicKBArticle holds for an article a of workspace $1 the help center shows: published, outside the
// trash and visible to everyone through every collection it is in.
const publicKBArticle = `a.workspace_id=$1 AND EXISTS (SELECT 1 FROM kb_public_articles kv WHERE kv.workspace_id=a.workspace_id AND kv.id=a.id)`

// helpChatKBArticle holds for an article a of workspace $1 help chat may read: one the help center
// shows, or one kept for help chat.
const helpChatKBArticle = `a.workspace_id=$1 AND EXISTS (SELECT 1 FROM kb_help_chat_articles kv WHERE kv.workspace_id=a.workspace_id AND kv.id=a.id)`

// publicKBTranslation holds for a translation t the help center shows (its language aside).
const publicKBTranslation = `EXISTS (SELECT 1 FROM kb_public_translations kt WHERE kt.workspace_id=t.workspace_id AND kt.article_id=t.article_id AND kt.locale=t.locale)`

// helpChatKBTranslation holds for a translation t help chat may read (its language aside).
const helpChatKBTranslation = `EXISTS (SELECT 1 FROM kb_help_chat_translations kt WHERE kt.workspace_id=t.workspace_id AND kt.article_id=t.article_id AND kt.locale=t.locale)`

// publicKBArticleFlag is true for an article a the help center shows, so a reader that may read more
// (help chat) knows which ones it may link.
const publicKBArticleFlag = `EXISTS (SELECT 1 FROM kb_public_articles kp WHERE kp.workspace_id=a.workspace_id AND kp.id=a.id)`

// publicKBCollection holds for a collection c the help center shows: outside the trash and visible to
// everyone, its parents included.
const publicKBCollection = `EXISTS (SELECT 1 FROM kb_collection_visibility kc WHERE kc.workspace_id=c.workspace_id AND kc.id=c.id AND kc.visibility_rank=0)`
