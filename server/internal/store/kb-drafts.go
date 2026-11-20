package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// kbHasDraft is true for an article a with unpublished changes to its own text.
const kbHasDraft = `EXISTS(SELECT 1 FROM kb_article_drafts d WHERE d.workspace_id=a.workspace_id AND d.article_id=a.id AND d.locale IS NULL)`

// kbArticleToken is the concurrency token of article a that every read shows and every write checks:
// the later of its own updated_at and its draft's. Saving a draft moves it without touching the
// article's updated_at, which the help center shows; trashing or restoring the article moves it too.
const kbArticleToken = `GREATEST(a.updated_at,(SELECT d.updated_at FROM kb_article_drafts d WHERE d.workspace_id=a.workspace_id AND d.article_id=a.id AND d.locale IS NULL))`

// lockKBArticle locks a live article and checks seen against its concurrency token, kbArticleToken. It returns the status and whether a draft exists.
func lockKBArticle(ctx context.Context, tx pgx.Tx, workspaceID, id string, seen time.Time) (string, bool, error) {
	var status string
	var updatedAt time.Time
	var draftAt *time.Time
	err := tx.QueryRow(ctx, `SELECT a.status, a.updated_at, d.updated_at FROM kb_articles a
		LEFT JOIN kb_article_drafts d ON d.workspace_id=a.workspace_id AND d.article_id=a.id AND d.locale IS NULL
		WHERE a.workspace_id=$1 AND a.id=$2 AND a.deleted_at IS NULL FOR UPDATE OF a`, workspaceID, id).Scan(&status, &updatedAt, &draftAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, domain.ErrKBArticleNotFound
	}
	if err != nil {
		return "", false, err
	}
	if draftAt != nil && draftAt.After(updatedAt) {
		updatedAt = *draftAt
	}
	if !updatedAt.Equal(seen) {
		return "", false, domain.ErrKBChanged
	}
	return status, draftAt != nil, nil
}

// saveKBArticleDraft writes change into the draft of a published article, made from the live article
// when it has none, and records the draft as a version of kind draft. The live row, its updated_at
// and the help center stay as they are.
func saveKBArticleDraft(ctx context.Context, tx pgx.Tx, workspaceID string, actor Actor, id string, change KBArticleChange) (domain.KBArticle, error) {
	if change.CollectionID != nil {
		if err := checkKBCollection(ctx, tx, workspaceID, *change.CollectionID); err != nil {
			return domain.KBArticle{}, err
		}
	}
	extras := []string{}
	if change.ExtraCollectionIDs != nil {
		home := ""
		if change.CollectionID != nil {
			home = *change.CollectionID
		} else if err := tx.QueryRow(ctx, `SELECT COALESCE(d.collection_id,a.collection_id)::text FROM kb_articles a
			LEFT JOIN kb_article_drafts d ON d.workspace_id=a.workspace_id AND d.article_id=a.id AND d.locale IS NULL
			WHERE a.workspace_id=$1 AND a.id=$2`, workspaceID, id).Scan(&home); err != nil {
			return domain.KBArticle{}, err
		}
		var err error
		if extras, err = checkKBExtraCollections(ctx, tx, workspaceID, home, *change.ExtraCollectionIDs); err != nil {
			return domain.KBArticle{}, err
		}
	}
	// Without new extras, a new home is no longer one of the draft's other collections.
	if _, err := tx.Exec(ctx, `INSERT INTO kb_article_drafts(workspace_id,article_id,title,slug,body_md,seo_title,seo_description,collection_id,extra_collection_ids,
			show_contents,updated_by_agent_id,created_at,updated_at)
		SELECT a.workspace_id,a.id,COALESCE($3,d.title,a.title),COALESCE($4,d.slug,a.slug),COALESCE($5,d.body_md,a.body_md),
			COALESCE($6,d.seo_title,a.seo_title),COALESCE($7,d.seo_description,a.seo_description),COALESCE($8::uuid,d.collection_id),
			CASE WHEN $9 THEN $10::uuid[] ELSE array_remove(d.extra_collection_ids,$8::uuid) END,COALESCE($12,d.show_contents),$11,clock_timestamp(),clock_timestamp()
		FROM kb_articles a LEFT JOIN kb_article_drafts d ON d.workspace_id=a.workspace_id AND d.article_id=a.id AND d.locale IS NULL
		WHERE a.workspace_id=$1 AND a.id=$2
		ON CONFLICT (workspace_id,article_id) WHERE locale IS NULL DO UPDATE SET title=EXCLUDED.title, slug=EXCLUDED.slug, body_md=EXCLUDED.body_md,
			seo_title=EXCLUDED.seo_title, seo_description=EXCLUDED.seo_description, collection_id=EXCLUDED.collection_id,
			extra_collection_ids=EXCLUDED.extra_collection_ids, show_contents=EXCLUDED.show_contents, updated_by_agent_id=EXCLUDED.updated_by_agent_id,
			updated_at=EXCLUDED.updated_at`,
		workspaceID, id, change.Title, change.Slug, change.BodyMD, change.SEOTitle, change.SEODescription, change.CollectionID,
		change.ExtraCollectionIDs != nil, extras, optionalID(actor.AgentID), change.ShowContents); err != nil {
		return domain.KBArticle{}, fmt.Errorf("save knowledge base article draft: %w", err)
	}
	return recordKBArticleDraft(ctx, tx, workspaceID, actor, id)
}

// recordKBArticleDraft records the article's draft as a version of kind draft, with the live status,
// links the media the draft names and returns the article.
func recordKBArticleDraft(ctx context.Context, tx pgx.Tx, workspaceID string, actor Actor, id string) (domain.KBArticle, error) {
	if _, err := tx.Exec(ctx, `INSERT INTO kb_article_versions(workspace_id,article_id,version,kind,title,slug,body_md,seo_title,seo_description,status,collection_id,
			show_contents,changed_by_agent_id,token_id,token_label,agent_request_id,created_at)
		SELECT a.workspace_id,a.id,COALESCE((SELECT max(version) FROM kb_article_versions WHERE workspace_id=$1 AND article_id=$2),0)+1,
			$3,d.title,d.slug,d.body_md,d.seo_title,d.seo_description,a.status,COALESCE(d.collection_id,a.collection_id),
			COALESCE(d.show_contents,a.show_contents),$4,t.id,t.label,$6,clock_timestamp()
		FROM kb_articles a JOIN kb_article_drafts d ON d.workspace_id=a.workspace_id AND d.article_id=a.id AND d.locale IS NULL
		LEFT JOIN sessions t ON t.workspace_id=a.workspace_id AND t.id=$5
		WHERE a.workspace_id=$1 AND a.id=$2`,
		workspaceID, id, domain.KBArticleDrafted, optionalID(actor.AgentID), optionalID(actor.TokenID), optionalID(actor.RequestID)); err != nil {
		return domain.KBArticle{}, fmt.Errorf("record knowledge base article draft: %w", err)
	}
	if _, err := linkKBMedia(ctx, tx, workspaceID, id); err != nil {
		return domain.KBArticle{}, err
	}
	return readKBArticle(ctx, tx, workspaceID, id)
}

// foldKBArticleDraft fills the fields change leaves out from the article's draft, then deletes the
// draft, so writing change publishes it. Other collections of the draft in the trash are dropped; a
// link to one stays on the article, as setKBArticleCollections keeps them.
func foldKBArticleDraft(ctx context.Context, tx pgx.Tx, workspaceID, id string, change KBArticleChange) (KBArticleChange, error) {
	var title, slug, body, seoTitle, seoDescription string
	var home *string
	var showContents *bool
	var extras, attachments []string
	var keepExtras bool
	err := tx.QueryRow(ctx, `DELETE FROM kb_article_drafts WHERE workspace_id=$1 AND article_id=$2 AND locale IS NULL
		RETURNING title,slug,body_md,seo_title,seo_description,collection_id::text,show_contents,COALESCE(extra_collection_ids::text[],'{}'),
			extra_collection_ids IS NULL,attachment_ids::text[]`,
		workspaceID, id).Scan(&title, &slug, &body, &seoTitle, &seoDescription, &home, &showContents, &extras, &keepExtras, &attachments)
	if errors.Is(err, pgx.ErrNoRows) {
		return change, nil
	}
	if err != nil {
		return change, fmt.Errorf("read knowledge base article draft: %w", err)
	}
	// The draft's files become the article's; a draft that never changed them leaves them as they are.
	if attachments != nil {
		if err := replaceKBAttachments(ctx, tx, workspaceID, id, nil, attachments); err != nil {
			return change, err
		}
	}
	for _, field := range []struct {
		target **string
		value  string
	}{{&change.Title, title}, {&change.Slug, slug}, {&change.BodyMD, body}, {&change.SEOTitle, seoTitle}, {&change.SEODescription, seoDescription}} {
		if *field.target == nil {
			value := field.value
			*field.target = &value
		}
	}
	if change.CollectionID == nil {
		change.CollectionID = home
	}
	if change.ShowContents == nil {
		change.ShowContents = showContents
	}
	if change.ExtraCollectionIDs != nil || keepExtras {
		return change, nil
	}
	effectiveHome := ""
	if change.CollectionID != nil {
		effectiveHome = *change.CollectionID
	} else if err := tx.QueryRow(ctx, `SELECT collection_id::text FROM kb_articles WHERE workspace_id=$1 AND id=$2`, workspaceID, id).Scan(&effectiveHome); err != nil {
		return change, err
	}
	rows, err := tx.Query(ctx, `SELECT id::text FROM kb_collections WHERE workspace_id=$1 AND id=ANY($2::uuid[]) AND deleted_at IS NULL AND id::text <> $3
		ORDER BY lower(name), id`, workspaceID, extras, effectiveHome)
	if err != nil {
		return change, err
	}
	live, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return change, err
	}
	change.ExtraCollectionIDs = &live
	return change, nil
}

// publishKBArticleChange writes change, with the fields it leaves out taken from the draft, to the
// live article and deletes the draft.
func publishKBArticleChange(ctx context.Context, tx pgx.Tx, workspaceID, id string, change KBArticleChange) error {
	change, err := foldKBArticleDraft(ctx, tx, workspaceID, id, change)
	if err != nil {
		return err
	}
	return writeKBArticleChange(ctx, tx, workspaceID, id, change)
}

// restoreKBArticleDraft makes the title, slug, text and search fields of version the draft of a
// published article; where the draft lists the article and its contents switch stay.
func restoreKBArticleDraft(ctx context.Context, tx pgx.Tx, workspaceID string, actor Actor, id string, version int) (domain.KBArticle, error) {
	tag, err := tx.Exec(ctx, `INSERT INTO kb_article_drafts(workspace_id,article_id,title,slug,body_md,seo_title,seo_description,updated_by_agent_id,created_at,updated_at)
		SELECT v.workspace_id,v.article_id,v.title,v.slug,v.body_md,v.seo_title,v.seo_description,$4,clock_timestamp(),clock_timestamp()
		FROM kb_article_versions v WHERE v.workspace_id=$1 AND v.article_id=$2 AND v.version=$3
		ON CONFLICT (workspace_id,article_id) WHERE locale IS NULL DO UPDATE SET title=EXCLUDED.title, slug=EXCLUDED.slug, body_md=EXCLUDED.body_md,
			seo_title=EXCLUDED.seo_title, seo_description=EXCLUDED.seo_description, updated_by_agent_id=EXCLUDED.updated_by_agent_id, updated_at=EXCLUDED.updated_at`,
		workspaceID, id, version, optionalID(actor.AgentID))
	if err != nil {
		return domain.KBArticle{}, fmt.Errorf("restore knowledge base article version into its draft: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.KBArticle{}, domain.ErrKBArticleVersionNotFound
	}
	return recordKBArticleDraft(ctx, tx, workspaceID, actor, id)
}

// readKBArticleDraft reads the article's draft, or nil without one. collection_ids is the draft's home
// first, then its other collections outside the trash by name; a draft that keeps them lists the
// article's other collections except the new home.
func readKBArticleDraft(ctx context.Context, tx pgx.Tx, workspaceID string, article domain.KBArticle) (*domain.KBArticleDraft, error) {
	var draft domain.KBArticleDraft
	var extras []string
	var keepExtras bool
	var agentID, agentName *string
	err := tx.QueryRow(ctx, `SELECT d.title,d.slug,d.body_md,d.seo_title,d.seo_description,COALESCE(d.collection_id,a.collection_id)::text,
			COALESCE(d.show_contents,a.show_contents),COALESCE(d.extra_collection_ids::text[],'{}'),d.extra_collection_ids IS NULL,d.created_at,d.updated_at,d.updated_by_agent_id::text,COALESCE(NULLIF(p.display_name,''),p.email)
		FROM kb_article_drafts d JOIN kb_articles a ON a.workspace_id=d.workspace_id AND a.id=d.article_id
		LEFT JOIN agents p ON p.workspace_id=d.workspace_id AND p.id=d.updated_by_agent_id
		WHERE d.workspace_id=$1 AND d.article_id=$2 AND d.locale IS NULL`, workspaceID, article.ID).Scan(&draft.Title, &draft.Slug, &draft.BodyMD,
		&draft.SEOTitle, &draft.SEODescription, &draft.CollectionID, &draft.ShowContents, &extras, &keepExtras, &draft.CreatedAt, &draft.UpdatedAt, &agentID, &agentName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read knowledge base article draft: %w", err)
	}
	draft.CreatedAt, draft.UpdatedAt = domain.UTC(draft.CreatedAt), domain.UTC(draft.UpdatedAt)
	draft.UpdatedBy = savedReplyPerson(agentID, agentName)
	draft.CollectionIDs = []string{draft.CollectionID}
	if keepExtras {
		for _, collectionID := range article.CollectionIDs[1:] {
			if collectionID != draft.CollectionID {
				draft.CollectionIDs = append(draft.CollectionIDs, collectionID)
			}
		}
		return &draft, nil
	}
	rows, err := tx.Query(ctx, `SELECT id::text FROM kb_collections WHERE workspace_id=$1 AND id=ANY($2::uuid[]) AND deleted_at IS NULL
		ORDER BY lower(name), id`, workspaceID, extras)
	if err != nil {
		return nil, err
	}
	live, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	for _, collectionID := range live {
		if !slices.Contains(draft.CollectionIDs, collectionID) {
			draft.CollectionIDs = append(draft.CollectionIDs, collectionID)
		}
	}
	return &draft, nil
}

// PublishKBArticleDraft makes the draft of a published article live as a version of kind edit; seen
// is the draft's updated_at. domain.ErrKBDraftNotFound without a draft.
func (s *Store) PublishKBArticleDraft(ctx context.Context, workspaceID string, actor Actor, id string, seen time.Time) (domain.KBArticle, error) {
	if !savedReplyID.MatchString(id) {
		return domain.KBArticle{}, domain.ErrKBArticleNotFound
	}
	article, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBArticle, error) {
		// The draft may move the article: the knowledge base lock first, as in changeKBArticle.
		if err := lockKB(ctx, tx, workspaceID); err != nil {
			return domain.KBArticle{}, err
		}
		_, hasDraft, err := lockKBArticle(ctx, tx, workspaceID, id, seen)
		if err != nil {
			return domain.KBArticle{}, err
		}
		if !hasDraft {
			return domain.KBArticle{}, domain.ErrKBDraftNotFound
		}
		// A draft that moves the article to a collection trashed since is refused as a conflict: the
		// request names no collection, so the request is not what is wrong. Share lock as in
		// checkKBCollection.
		var homeTrashed bool
		if err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT c.deleted_at IS NOT NULL FROM kb_article_drafts d JOIN kb_collections c ON c.workspace_id=d.workspace_id AND c.id=d.collection_id
			WHERE d.workspace_id=$1 AND d.article_id=$2 AND d.locale IS NULL FOR SHARE OF c), false)`, workspaceID, id).Scan(&homeTrashed); err != nil {
			return domain.KBArticle{}, err
		}
		if homeTrashed {
			return domain.KBArticle{}, domain.ErrKBCollectionInTrash
		}
		return changeKBArticleLocked(ctx, tx, workspaceID, actor, id, domain.KBArticleEdited, nil, func(tx pgx.Tx) error {
			return publishKBArticleChange(ctx, tx, workspaceID, id, KBArticleChange{})
		})
	})
	return article, kbError("publish knowledge base article draft", err)
}

// DiscardKBArticleDraft deletes the draft of an article; the live article and its versions stay as
// they are. seen is the article's concurrency token, kbArticleToken.
func (s *Store) DiscardKBArticleDraft(ctx context.Context, workspaceID string, id string, seen time.Time) (domain.KBArticle, error) {
	if !savedReplyID.MatchString(id) {
		return domain.KBArticle{}, domain.ErrKBArticleNotFound
	}
	article, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBArticle, error) {
		_, hasDraft, err := lockKBArticle(ctx, tx, workspaceID, id, seen)
		if err != nil {
			return domain.KBArticle{}, err
		}
		if !hasDraft {
			return domain.KBArticle{}, domain.ErrKBDraftNotFound
		}
		if _, err := tx.Exec(ctx, `DELETE FROM kb_article_drafts WHERE workspace_id=$1 AND article_id=$2 AND locale IS NULL`, workspaceID, id); err != nil {
			return domain.KBArticle{}, fmt.Errorf("discard knowledge base article draft: %w", err)
		}
		if _, err := linkKBMedia(ctx, tx, workspaceID, id); err != nil {
			return domain.KBArticle{}, err
		}
		return readKBArticle(ctx, tx, workspaceID, id)
	})
	return article, kbError("discard knowledge base article draft", err)
}
