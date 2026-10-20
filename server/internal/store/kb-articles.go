package store

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// kbArticleChangeColumns read a version row v with its person p.
const kbArticleChangeColumns = `v.version,v.kind,v.restored_from_version,v.changed_by_agent_id::text,COALESCE(NULLIF(p.display_name,''),p.email),
	v.token_id::text,v.token_label,v.agent_request_id::text,v.created_at`

// kbArticleSelect reads articles a with their newest version; extra columns follow the change columns.
func kbArticleSelect(extra, join string) string {
	return `SELECT a.id::text,a.number,a.collection_id::text,a.title,a.slug,a.status,a.published_at,a.author_agent_id::text,a.created_at,a.updated_at,` +
		kbArticleChangeColumns + extra + `
	FROM kb_articles a
	LEFT JOIN LATERAL (SELECT * FROM kb_article_versions x WHERE x.workspace_id=a.workspace_id AND x.article_id=a.id ORDER BY x.version DESC LIMIT 1) v ON true
	LEFT JOIN agents p ON p.workspace_id=v.workspace_id AND p.id=v.changed_by_agent_id` + join
}

func scanKBSummary(row pgx.Row, extra ...any) (domain.KBArticleSummary, error) {
	var item domain.KBArticleSummary
	var change changeRow
	targets := append([]any{&item.ID, &item.Number, &item.CollectionID, &item.Title, &item.Slug, &item.Status, &item.PublishedAt, &item.AuthorAgentID,
		&item.CreatedAt, &item.UpdatedAt}, change.targets()...)
	if err := row.Scan(append(targets, extra...)...); err != nil {
		return item, err
	}
	item.CreatedAt, item.UpdatedAt, item.PublishedAt = domain.UTC(item.CreatedAt), domain.UTC(item.UpdatedAt), domain.UTCOptional(item.PublishedAt)
	item.LastChange = change.change()
	return item, nil
}

// readKBArticle reads one article of the workspace, in or out of the trash, with its text and translations.
func readKBArticle(ctx context.Context, tx pgx.Tx, workspaceID, id string) (domain.KBArticle, error) {
	var article domain.KBArticle
	summary, err := scanKBSummary(tx.QueryRow(ctx, kbArticleSelect(`,a.body_md,a.source_html IS NOT NULL,a.deleted_at`, ``)+` WHERE a.workspace_id=$1 AND a.id=$2`, workspaceID, id),
		&article.BodyMD, &article.HasSourceHTML, &article.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return article, domain.ErrKBArticleNotFound
	}
	if err != nil {
		return article, err
	}
	article.KBArticleSummary, article.DeletedAt = summary, domain.UTCOptional(article.DeletedAt)
	rows, err := tx.Query(ctx, `SELECT locale,title,body_md,updated_at FROM kb_article_translations WHERE workspace_id=$1 AND article_id=$2 ORDER BY locale`, workspaceID, id)
	if err != nil {
		return article, err
	}
	article.Translations, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.KBArticleTranslation, error) {
		var translation domain.KBArticleTranslation
		err := row.Scan(&translation.Locale, &translation.Title, &translation.BodyMD, &translation.UpdatedAt)
		translation.UpdatedAt = domain.UTC(translation.UpdatedAt)
		return translation, err
	})
	if article.Translations == nil {
		article.Translations = []domain.KBArticleTranslation{}
	}
	return article, err
}

// GetKBArticle returns one article of the workspace, also from the trash.
func (s *Store) GetKBArticle(ctx context.Context, workspaceID, id string) (domain.KBArticle, error) {
	if !savedReplyID.MatchString(id) {
		return domain.KBArticle{}, domain.ErrKBArticleNotFound
	}
	article, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBArticle, error) { return readKBArticle(ctx, tx, workspaceID, id) })
	return article, kbError("get knowledge base article", err)
}

// KBArticleFilter selects articles outside the trash. Query matches every word of Query as a word or
// the start of one, or Query inside the title.
type KBArticleFilter struct {
	Query, Status, CollectionID string
	Limit                       int
	Cursor                      string
}

// likePattern escapes the wildcards of ILIKE so the text matches as typed.
var likePattern = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// kbQuery turns text into a tsquery that matches every word as a prefix. The words come from
// kb_search_vector, the function that made the search column, so an address, a hyphenated word or
// a URL is split the same way on both sides. Text without a word gives NULL, which matches nothing.
// The lexemes are cast, not parsed again, so nothing in them is read as tsquery syntax.
const kbQuery = `(SELECT string_agg('''' || replace(replace(lexeme, '\', '\\'), '''', '''''') || ''':*', ' & ')::tsquery
	FROM unnest(kb_search_vector($2::text)))`

// ListKBArticles returns one page of the workspace's live articles without their text, and the
// cursor of the next page ("" on the last one).
func (s *Store) ListKBArticles(ctx context.Context, workspaceID string, filter KBArticleFilter) ([]domain.KBArticleSummary, string, error) {
	offset := 0
	if filter.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(filter.Cursor)
		value, found := strings.CutPrefix(string(raw), "o:")
		parsed, parseErr := strconv.Atoi(value)
		if err != nil || !found || parseErr != nil || parsed < 0 {
			return nil, "", ErrBadCursor
		}
		offset = parsed
	}
	query := strings.TrimSpace(filter.Query)
	args := []any{workspaceID, query, filter.Limit + 1, offset}
	where := ` WHERE a.workspace_id=$1 AND a.deleted_at IS NULL`
	if filter.Status != "" {
		args = append(args, filter.Status)
		where += fmt.Sprintf(` AND a.status=$%d`, len(args))
	}
	if filter.CollectionID != "" {
		if !savedReplyID.MatchString(filter.CollectionID) {
			return []domain.KBArticleSummary{}, "", nil
		}
		args = append(args, filter.CollectionID)
		where += fmt.Sprintf(` AND a.collection_id=$%d`, len(args))
	}
	order := ` ORDER BY a.updated_at DESC, a.id DESC`
	join := ``
	if query != "" {
		args = append(args, "%"+likePattern.Replace(query)+"%")
		join = ` CROSS JOIN LATERAL (SELECT ` + kbQuery + ` AS q) t`
		where += fmt.Sprintf(` AND (a.search @@ t.q OR a.title ILIKE $%d)`, len(args))
		order = ` ORDER BY COALESCE(ts_rank(a.search, t.q), 0) DESC, a.updated_at DESC, a.id DESC`
	} else {
		where += ` AND $2::text = ''`
	}
	items, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.KBArticleSummary, error) {
		rows, err := tx.Query(ctx, kbArticleSelect(``, join)+where+order+` LIMIT $3 OFFSET $4`, args...)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.KBArticleSummary, error) { return scanKBSummary(row) })
	})
	if err != nil {
		return nil, "", fmt.Errorf("list knowledge base articles: %w", err)
	}
	next := ""
	if len(items) > filter.Limit {
		items = items[:filter.Limit]
		next = base64.RawURLEncoding.EncodeToString([]byte("o:" + strconv.Itoa(offset+filter.Limit)))
	}
	return items, next, nil
}

// ListTrashedKBArticles returns one page of the articles in the trash, most recently deleted first.
func (s *Store) ListTrashedKBArticles(ctx context.Context, workspaceID string, limit int, cursor string) ([]domain.TrashedKBArticle, string, error) {
	query := kbArticleSelect(`,a.deleted_at,a.deleted_by_agent_id::text,COALESCE(NULLIF(d.display_name,''),d.email)`,
		` LEFT JOIN agents d ON d.workspace_id=a.workspace_id AND d.id=a.deleted_by_agent_id`) + ` WHERE a.workspace_id=$1 AND a.deleted_at IS NOT NULL`
	args := []any{workspaceID, limit + 1}
	if cursor != "" {
		at, id, err := decodeTrashCursor(cursor)
		if err != nil {
			return nil, "", err
		}
		query += ` AND (a.deleted_at, a.id) < ($3, $4)`
		args = append(args, at, id)
	}
	items, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.TrashedKBArticle, error) {
		rows, err := tx.Query(ctx, query+` ORDER BY a.deleted_at DESC, a.id DESC LIMIT $2`, args...)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.TrashedKBArticle, error) {
			var item domain.TrashedKBArticle
			var deleterID, deleterName *string
			summary, err := scanKBSummary(row, &item.DeletedAt, &deleterID, &deleterName)
			if err != nil {
				return item, err
			}
			item.KBArticleSummary, item.DeletedAt = summary, domain.UTC(item.DeletedAt)
			item.DeletedBy = savedReplyPerson(deleterID, deleterName)
			item.PurgesAt = item.DeletedAt.Add(domain.KBTrashRetention)
			return item, nil
		})
	})
	if err != nil {
		return nil, "", fmt.Errorf("list knowledge base article trash: %w", err)
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		last := items[limit-1]
		next = base64.RawURLEncoding.EncodeToString([]byte(last.DeletedAt.Format(time.RFC3339Nano) + " " + last.ID))
	}
	return items, next, nil
}

// addKBArticleVersion records the article as it is now as its next version, links the images its
// text points at, and bumps the knowledge base version. The caller changed the row in tx first, so
// the row lock orders concurrent versions of one article.
func addKBArticleVersion(ctx context.Context, tx pgx.Tx, workspaceID, articleID string, actor Actor, kind string, restoredFrom *int) error {
	if _, err := tx.Exec(ctx, `INSERT INTO kb_article_versions(workspace_id,article_id,version,kind,restored_from_version,title,slug,body_md,status,collection_id,
			changed_by_agent_id,token_id,token_label,agent_request_id,created_at)
		SELECT a.workspace_id,a.id,COALESCE((SELECT max(version) FROM kb_article_versions WHERE workspace_id=$1 AND article_id=$2),0)+1,
			$3,$4,a.title,a.slug,a.body_md,a.status,a.collection_id,$5,t.id,t.label,$7,clock_timestamp()
		FROM kb_articles a LEFT JOIN sessions t ON t.workspace_id=a.workspace_id AND t.id=$6
		WHERE a.workspace_id=$1 AND a.id=$2`,
		workspaceID, articleID, kind, restoredFrom, optionalID(actor.AgentID), optionalID(actor.TokenID), optionalID(actor.RequestID)); err != nil {
		return fmt.Errorf("record knowledge base article version: %w", err)
	}
	var body string
	if err := tx.QueryRow(ctx, `SELECT body_md FROM kb_articles WHERE workspace_id=$1 AND id=$2`, workspaceID, articleID).Scan(&body); err != nil {
		return err
	}
	if err := linkKBImages(ctx, tx, workspaceID, articleID, body); err != nil {
		return err
	}
	return bumpKB(ctx, tx, workspaceID)
}

// linkKBImages links the images a text of the article points at to the article.
func linkKBImages(ctx context.Context, tx pgx.Tx, workspaceID, articleID, body string) error {
	if ids := domain.KBImageIDs(body); len(ids) > 0 {
		// Only images of this workspace are linked; an id of another workspace's image matches no row.
		if _, err := tx.Exec(ctx, `INSERT INTO kb_article_images(workspace_id,article_id,image_id)
			SELECT $1,$2,i.id FROM kb_images i WHERE i.workspace_id=$1 AND i.id=ANY($3::uuid[]) ON CONFLICT DO NOTHING`, workspaceID, articleID, ids); err != nil {
			return fmt.Errorf("link knowledge base images: %w", err)
		}
	}
	return nil
}

// admitKBArticle takes the knowledge base lock, refuses a full workspace (the trash counts) and returns
// the next article number.
func admitKBArticle(ctx context.Context, tx pgx.Tx, workspaceID string) (int64, error) {
	if err := lockKB(ctx, tx, workspaceID); err != nil {
		return 0, err
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM kb_articles WHERE workspace_id=$1`, workspaceID).Scan(&count); err != nil {
		return 0, err
	}
	if count >= domain.MaxKBArticles {
		return 0, domain.ErrKBArticleLimit
	}
	var number int64
	if err := tx.QueryRow(ctx, `INSERT INTO help_centers(workspace_id,last_article_number) VALUES($1,1)
		ON CONFLICT (workspace_id) DO UPDATE SET last_article_number=help_centers.last_article_number+1 RETURNING last_article_number`, workspaceID).Scan(&number); err != nil {
		return 0, fmt.Errorf("number the article: %w", err)
	}
	return number, nil
}

// checkKBCollection refuses a collection that is missing or in the trash. The share lock holds off a
// concurrent delete of the collection until this transaction commits, and makes this read wait for one
// in progress, so a live article never lands in a collection in the trash.
func checkKBCollection(ctx context.Context, tx pgx.Tx, workspaceID, id string) error {
	if !savedReplyID.MatchString(id) {
		return &domain.InvalidKBError{Message: "The collection was not found."}
	}
	var live bool
	err := tx.QueryRow(ctx, `SELECT deleted_at IS NULL FROM kb_collections WHERE workspace_id=$1 AND id=$2 FOR SHARE`, workspaceID, id).Scan(&live)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !live) {
		return &domain.InvalidKBError{Message: "The collection was not found, or it is in the trash."}
	}
	return err
}

// KBArticleInput is a new article; an empty Slug is made from the title, an empty Status is a draft.
type KBArticleInput struct {
	CollectionID, Title, Slug, BodyMD, Status string
}

// CreateKBArticle adds an article with the next number of the workspace and its first version.
func (s *Store) CreateKBArticle(ctx context.Context, workspaceID string, actor Actor, input KBArticleInput) (domain.KBArticle, error) {
	title, err := domain.CleanKBText("title", input.Title, domain.MaxKBArticleTitle)
	if err != nil {
		return domain.KBArticle{}, err
	}
	if input.Slug == "" {
		input.Slug = domain.Slugify(title, "article")
	} else if err := domain.CheckKBSlug(input.Slug); err != nil {
		return domain.KBArticle{}, err
	}
	if err := domain.CheckKBBody(input.BodyMD); err != nil {
		return domain.KBArticle{}, err
	}
	if input.Status == "" {
		input.Status = domain.KBDraft
	}
	if err := domain.CheckKBStatus(input.Status); err != nil {
		return domain.KBArticle{}, err
	}
	article, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBArticle, error) {
		number, err := admitKBArticle(ctx, tx, workspaceID)
		if err != nil {
			return domain.KBArticle{}, err
		}
		if err := checkKBCollection(ctx, tx, workspaceID, input.CollectionID); err != nil {
			return domain.KBArticle{}, err
		}
		var id string
		if err := tx.QueryRow(ctx, `INSERT INTO kb_articles(workspace_id,collection_id,number,title,slug,body_md,status,published_at,author_agent_id)
			VALUES($1,$2,$3,$4,$5,$6,$7,CASE WHEN $7='published' THEN clock_timestamp() END,$8) RETURNING id::text`,
			workspaceID, input.CollectionID, number, title, input.Slug, input.BodyMD, input.Status, optionalID(actor.AgentID)).Scan(&id); err != nil {
			return domain.KBArticle{}, err
		}
		if err := addKBArticleVersion(ctx, tx, workspaceID, id, actor, domain.KBArticleCreated, nil); err != nil {
			return domain.KBArticle{}, err
		}
		return readKBArticle(ctx, tx, workspaceID, id)
	})
	return article, kbError("create knowledge base article", err)
}

// KBArticleChange is a partial update of an article; a nil field stays as it is.
type KBArticleChange struct {
	Title, Slug, BodyMD, CollectionID, Status *string
}

// UpdateKBArticle changes a live article and records the version. seen is the updated_at the client read.
func (s *Store) UpdateKBArticle(ctx context.Context, workspaceID string, actor Actor, id string, seen time.Time, change KBArticleChange) (domain.KBArticle, error) {
	if !savedReplyID.MatchString(id) {
		return domain.KBArticle{}, domain.ErrKBArticleNotFound
	}
	if change.Title != nil {
		title, err := domain.CleanKBText("title", *change.Title, domain.MaxKBArticleTitle)
		if err != nil {
			return domain.KBArticle{}, err
		}
		change.Title = &title
	}
	if change.Slug != nil {
		if err := domain.CheckKBSlug(*change.Slug); err != nil {
			return domain.KBArticle{}, err
		}
	}
	if change.BodyMD != nil {
		if err := domain.CheckKBBody(*change.BodyMD); err != nil {
			return domain.KBArticle{}, err
		}
	}
	if change.Status != nil {
		if err := domain.CheckKBStatus(*change.Status); err != nil {
			return domain.KBArticle{}, err
		}
	}
	return s.changeKBArticle(ctx, workspaceID, actor, id, seen, domain.KBArticleEdited, nil, func(tx pgx.Tx) error {
		if change.CollectionID != nil {
			if err := checkKBCollection(ctx, tx, workspaceID, *change.CollectionID); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `UPDATE kb_articles SET title=COALESCE($3,title), slug=COALESCE($4,slug), body_md=COALESCE($5,body_md),
			collection_id=COALESCE($6::uuid,collection_id), status=COALESCE($7,status),
			published_at=CASE WHEN COALESCE($7,status)='published' THEN COALESCE(published_at, clock_timestamp()) END, updated_at=clock_timestamp()
			WHERE workspace_id=$1 AND id=$2`, workspaceID, id, change.Title, change.Slug, change.BodyMD, change.CollectionID, change.Status)
		return err
	})
}

// RestoreKBArticleVersion makes the title, slug and text of version current as a new version; the
// status and collection stay.
func (s *Store) RestoreKBArticleVersion(ctx context.Context, workspaceID string, actor Actor, id string, version int, seen time.Time) (domain.KBArticle, error) {
	if !savedReplyID.MatchString(id) {
		return domain.KBArticle{}, domain.ErrKBArticleNotFound
	}
	return s.changeKBArticle(ctx, workspaceID, actor, id, seen, domain.KBArticleRestored, &version, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE kb_articles a SET title=v.title, slug=v.slug, body_md=v.body_md, updated_at=clock_timestamp() FROM kb_article_versions v
			WHERE a.workspace_id=$1 AND a.id=$2 AND v.workspace_id=a.workspace_id AND v.article_id=a.id AND v.version=$3`, workspaceID, id, version)
		if err == nil && tag.RowsAffected() == 0 {
			return domain.ErrKBArticleVersionNotFound
		}
		return err
	})
}

// changeKBArticle locks a live, unchanged article, runs write and records the version of kind.
func (s *Store) changeKBArticle(ctx context.Context, workspaceID string, actor Actor, id string, seen time.Time, kind string, restoredFrom *int, write func(pgx.Tx) error) (domain.KBArticle, error) {
	article, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBArticle, error) {
		if err := kbLiveAndCurrent(ctx, tx, `kb_articles`, workspaceID, id, seen, domain.ErrKBArticleNotFound); err != nil {
			return domain.KBArticle{}, err
		}
		if err := write(tx); err != nil {
			return domain.KBArticle{}, err
		}
		if err := addKBArticleVersion(ctx, tx, workspaceID, id, actor, kind, restoredFrom); err != nil {
			return domain.KBArticle{}, err
		}
		return readKBArticle(ctx, tx, workspaceID, id)
	})
	return article, kbError("change knowledge base article", err)
}

// DeleteKBArticle moves an article to the trash and records the version.
func (s *Store) DeleteKBArticle(ctx context.Context, workspaceID string, actor Actor, id string, seen time.Time) error {
	if !savedReplyID.MatchString(id) {
		return domain.ErrKBArticleNotFound
	}
	_, err := s.changeKBArticle(ctx, workspaceID, actor, id, seen, domain.KBArticleDeleted, nil, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE kb_articles SET deleted_at=$3, deleted_by_agent_id=$4, updated_at=clock_timestamp() WHERE workspace_id=$1 AND id=$2`,
			workspaceID, id, s.now(), optionalID(actor.AgentID))
		return err
	})
	return err
}

// UndeleteKBArticle takes an article out of the trash as it was: domain.ErrKBCollectionInTrash while
// its collection is in the trash.
func (s *Store) UndeleteKBArticle(ctx context.Context, workspaceID string, actor Actor, id string, seen time.Time) (domain.KBArticle, error) {
	if !savedReplyID.MatchString(id) {
		return domain.KBArticle{}, domain.ErrKBArticleNotFound
	}
	article, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBArticle, error) {
		if err := kbTrashedAndCurrent(ctx, tx, `kb_articles`, workspaceID, id, seen, domain.ErrKBArticleNotFound); err != nil {
			return domain.KBArticle{}, err
		}
		var collectionLive bool
		// Share lock as in checkKBCollection.
		if err := tx.QueryRow(ctx, `SELECT c.deleted_at IS NULL FROM kb_articles a JOIN kb_collections c ON c.workspace_id=a.workspace_id AND c.id=a.collection_id
			WHERE a.workspace_id=$1 AND a.id=$2 FOR SHARE OF c`, workspaceID, id).Scan(&collectionLive); err != nil {
			return domain.KBArticle{}, err
		}
		if !collectionLive {
			return domain.KBArticle{}, domain.ErrKBCollectionInTrash
		}
		if _, err := tx.Exec(ctx, `UPDATE kb_articles SET deleted_at=NULL, deleted_by_agent_id=NULL, updated_at=clock_timestamp() WHERE workspace_id=$1 AND id=$2`,
			workspaceID, id); err != nil {
			return domain.KBArticle{}, err
		}
		if err := addKBArticleVersion(ctx, tx, workspaceID, id, actor, domain.KBArticleUndeleted, nil); err != nil {
			return domain.KBArticle{}, err
		}
		return readKBArticle(ctx, tx, workspaceID, id)
	})
	return article, kbError("restore knowledge base article", err)
}

// KBArticleVersions returns one page of the versions of an article, newest first, as
// SavedReplyVersions does for a saved reply.
func (s *Store) KBArticleVersions(ctx context.Context, workspaceID, id string, limit, before int) ([]domain.KBArticleVersion, int, error) {
	if !savedReplyID.MatchString(id) {
		return nil, 0, domain.ErrKBArticleNotFound
	}
	if before <= 0 {
		before = math.MaxInt32
	}
	versions, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.KBArticleVersion, error) {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM kb_articles WHERE workspace_id=$1 AND id=$2)`, workspaceID, id).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, domain.ErrKBArticleNotFound
		}
		rows, err := tx.Query(ctx, `SELECT `+kbArticleChangeColumns+`,v.title,v.slug,v.body_md,v.status,v.collection_id::text FROM kb_article_versions v
			LEFT JOIN agents p ON p.workspace_id=v.workspace_id AND p.id=v.changed_by_agent_id
			WHERE v.workspace_id=$1 AND v.article_id=$2 AND v.version<$3 ORDER BY v.version DESC LIMIT $4`, workspaceID, id, before, limit+1)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.KBArticleVersion, error) {
			var change changeRow
			var version domain.KBArticleVersion
			if err := row.Scan(append(change.targets(), &version.Title, &version.Slug, &version.BodyMD, &version.Status, &version.CollectionID)...); err != nil {
				return version, err
			}
			version.KBArticleChange = *change.change()
			return version, nil
		})
	})
	if err != nil {
		return nil, 0, kbError("list knowledge base article versions", err)
	}
	next := 0
	if len(versions) > limit {
		versions = versions[:limit]
		next = versions[limit-1].Version
	}
	return versions, next, nil
}
