package store

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
)

// The help center shows published articles, the media in them and their collections. These say
// whether a change would alter what it shows, which an agent token needs kb_articles:publish for.

// KBArticlePublished reports whether the article (in the trash or not) is published; an unknown id is
// not.
func (s *Store) KBArticlePublished(ctx context.Context, workspaceID, id string) (bool, error) {
	if !savedReplyID.MatchString(id) {
		return false, nil
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (bool, error) {
		var published bool
		err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM kb_articles WHERE workspace_id=$1 AND id=$2 AND status='published')`, workspaceID, id).Scan(&published)
		return published, err
	})
}

// KBMediaShownPublished reports whether the help center shows the media item now, by the rule of
// HelpCenterImage (shownTextNames): unpublished changes that name it are not on any live page. The text
// is searched for the id itself, as texts link an image by either of its paths.
func (s *Store) KBMediaShownPublished(ctx context.Context, workspaceID, mediaID string) (bool, error) {
	if !savedReplyID.MatchString(mediaID) {
		return false, nil
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (bool, error) {
		var shown bool
		err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM kb_article_media l JOIN kb_articles a ON a.workspace_id=l.workspace_id AND a.id=l.article_id
			WHERE l.workspace_id=$1 AND l.media_id=$2 AND l.current AND `+shownTextNames+`)
			OR EXISTS(SELECT 1 FROM kb_article_attachments l JOIN kb_articles a ON a.workspace_id=l.workspace_id AND a.id=l.article_id
			WHERE l.workspace_id=$1 AND l.media_id=$2 AND `+listedByShownArticle+`)`, workspaceID, mediaID, strings.ToLower(mediaID)).Scan(&shown)
		return shown, err
	})
}

// KBReplacementTouchesPublished reports whether undoing the replacement would change a live published
// article. An article listed with version 0 had only its unpublished changes rewritten, so undoing them
// changes nothing people outside see.
func (s *Store) KBReplacementTouchesPublished(ctx context.Context, workspaceID, replacementID string) (bool, error) {
	if !savedReplyID.MatchString(replacementID) {
		return false, nil
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (bool, error) {
		var touches bool
		err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM kb_media_replacements r CROSS JOIN LATERAL unnest(r.article_ids, r.article_versions) AS v(article_id, version)
			JOIN kb_articles a ON a.workspace_id=r.workspace_id AND a.id=v.article_id
			WHERE r.workspace_id=$1 AND r.id=$2 AND v.version > 0 AND a.deleted_at IS NULL AND a.status='published')
			OR EXISTS(SELECT 1 FROM kb_media_replacements r JOIN kb_article_attachments l ON l.workspace_id=r.workspace_id AND l.media_id=r.from_media_id
			JOIN kb_articles a ON a.workspace_id=l.workspace_id AND a.id=l.article_id WHERE r.workspace_id=$1 AND r.id=$2 AND r.in_place AND `+listedByShownArticle+`)`, workspaceID, replacementID).Scan(&touches)
		return touches, err
	})
}
