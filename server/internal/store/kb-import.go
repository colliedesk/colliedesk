package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// The functions below write what an import brings into the knowledge base, in the importer's
// transaction: the source's own timestamps, its original HTML and the fields Colliedesk has no column
// for. Every write takes the same lock, limits, numbering and version history as the API writes.

// KBImportCollection is a collection an import creates. Slug is made from Name; a slug taken by a
// sibling gets "-" and SlugSuffix appended.
type KBImportCollection struct {
	ParentID             *string
	Name, Description    string
	SlugSuffix           string
	SourceMeta           []byte
	CreatedAt, UpdatedAt time.Time
}

// ImportKBCollection adds a collection with the source's timestamps.
func ImportKBCollection(ctx context.Context, tx pgx.Tx, workspaceID string, input KBImportCollection) (string, error) {
	clean := KBCollectionInput{Name: input.Name, Description: input.Description, ParentID: input.ParentID}
	if err := cleanKBCollection(&clean); err != nil {
		return "", err
	}
	if err := admitKBCollection(ctx, tx, workspaceID, input.ParentID); err != nil {
		return "", err
	}
	insert := func(slug string) (string, error) {
		return kbSavepoint(ctx, tx, func(sub pgx.Tx) (string, error) {
			var id string
			err := sub.QueryRow(ctx, `INSERT INTO kb_collections(workspace_id,parent_id,name,slug,description,source_meta,created_at,updated_at)
				VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id::text`,
				workspaceID, input.ParentID, clean.Name, slug, clean.Description, input.SourceMeta, input.CreatedAt, input.UpdatedAt).Scan(&id)
			return id, err
		})
	}
	id, err := insert(clean.Slug)
	if errors.Is(err, domain.ErrKBDuplicateSlug) && input.SlugSuffix != "" {
		id, err = insert(domain.Slugify(clean.Slug+"-"+input.SlugSuffix, "collection"))
	}
	if err != nil {
		return "", kbCollectionError("import", err)
	}
	return id, bumpKB(ctx, tx, workspaceID)
}

// KBImportArticle is an article as an import writes it. A published article's published_at is its
// UpdatedAt: the source does not say when it was published.
type KBImportArticle struct {
	CollectionID, Title, BodyMD, Status string
	AuthorAgentID                       *string
	SourceMeta                          []byte
	SourceHTML                          string
	CreatedAt, UpdatedAt                time.Time
}

func cleanKBImportArticle(input *KBImportArticle) error {
	title, err := domain.CleanKBText("title", input.Title, domain.MaxKBArticleTitle)
	if err != nil {
		return err
	}
	input.Title = title
	if err := domain.CheckKBBody(input.BodyMD); err != nil {
		return err
	}
	return domain.CheckKBStatus(input.Status)
}

func publishedAt(input KBImportArticle) *time.Time {
	if input.Status != domain.KBPublished {
		return nil
	}
	at := input.UpdatedAt
	return &at
}

// ImportKBArticle adds an article with the next number and its first version, of kind import.
func ImportKBArticle(ctx context.Context, tx pgx.Tx, workspaceID string, input KBImportArticle) (string, error) {
	if err := cleanKBImportArticle(&input); err != nil {
		return "", err
	}
	number, err := admitKBArticle(ctx, tx, workspaceID)
	if err != nil {
		return "", err
	}
	if err := checkKBCollection(ctx, tx, workspaceID, input.CollectionID); err != nil {
		return "", err
	}
	var id string
	if err := tx.QueryRow(ctx, `INSERT INTO kb_articles(workspace_id,collection_id,number,title,slug,body_md,status,published_at,author_agent_id,
			source_meta,source_html,created_at,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING id::text`,
		workspaceID, input.CollectionID, number, input.Title, domain.Slugify(input.Title, "article"), input.BodyMD, input.Status, publishedAt(input),
		input.AuthorAgentID, input.SourceMeta, input.SourceHTML, input.CreatedAt, input.UpdatedAt).Scan(&id); err != nil {
		return "", fmt.Errorf("import knowledge base article: %w", err)
	}
	return id, addKBArticleVersion(ctx, tx, workspaceID, id, Actor{}, domain.KBArticleImported, nil)
}

// Outcomes of ReimportKBArticle.
const (
	KBReimported = "reimported"
	// KBKept: the article has a version written in Colliedesk after the last import (an edit, a
	// restore, a delete), so the import leaves it alone.
	KBKept = "kept"
	// KBGone: the article no longer exists (deleted for good); an import does not bring it back.
	KBGone = "gone"
)

// KBArticleImportedLast tells whether an article exists and whether its newest version is an import.
func KBArticleImportedLast(ctx context.Context, tx pgx.Tx, workspaceID, id string) (exists, imported bool, err error) {
	var kind string
	err = tx.QueryRow(ctx, `SELECT kind FROM kb_article_versions WHERE workspace_id=$1 AND article_id=$2 ORDER BY version DESC LIMIT 1`, workspaceID, id).Scan(&kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, nil
	}
	return err == nil, kind == domain.KBArticleImported, err
}

// ReimportKBArticle writes the source's newer article over an imported one, as a new version of
// kind import, only while no one changed it in Colliedesk since the last import. The article row is
// locked first, so an edit cannot land between the check and the write.
func ReimportKBArticle(ctx context.Context, tx pgx.Tx, workspaceID, id string, input KBImportArticle) (string, error) {
	if err := cleanKBImportArticle(&input); err != nil {
		return "", err
	}
	var live bool
	err := tx.QueryRow(ctx, `SELECT deleted_at IS NULL FROM kb_articles WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, workspaceID, id).Scan(&live)
	if errors.Is(err, pgx.ErrNoRows) {
		return KBGone, nil
	}
	if err != nil {
		return "", err
	}
	_, imported, err := KBArticleImportedLast(ctx, tx, workspaceID, id)
	if err != nil {
		return "", err
	}
	if !live || !imported {
		return KBKept, nil
	}
	if err := checkKBCollection(ctx, tx, workspaceID, input.CollectionID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `UPDATE kb_articles SET collection_id=$3,title=$4,slug=$5,body_md=$6,status=$7,published_at=$8,author_agent_id=$9,
			source_meta=$10,source_html=$11,created_at=$12,updated_at=$13 WHERE workspace_id=$1 AND id=$2`,
		workspaceID, id, input.CollectionID, input.Title, domain.Slugify(input.Title, "article"), input.BodyMD, input.Status, publishedAt(input),
		input.AuthorAgentID, input.SourceMeta, input.SourceHTML, input.CreatedAt, input.UpdatedAt); err != nil {
		return "", fmt.Errorf("import knowledge base article: %w", err)
	}
	return KBReimported, addKBArticleVersion(ctx, tx, workspaceID, id, Actor{}, domain.KBArticleImported, nil)
}

// KBImportTranslation is one language of an article as an import reads it.
type KBImportTranslation struct {
	Locale, Title, BodyMD string
	SourceMeta            []byte
	SourceHTML            string
	CreatedAt, UpdatedAt  time.Time
}

// PutKBArticleTranslation adds or replaces a translation of an article and links the images its text
// points at to the article, so the purge keeps them.
func PutKBArticleTranslation(ctx context.Context, tx pgx.Tx, workspaceID, articleID string, input KBImportTranslation) error {
	if err := domain.CheckLocale(input.Locale); err != nil {
		return err
	}
	title, err := domain.CleanKBText("title", input.Title, domain.MaxKBArticleTitle)
	if err != nil {
		return err
	}
	if err := domain.CheckKBBody(input.BodyMD); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO kb_article_translations(workspace_id,article_id,locale,title,body_md,source_meta,source_html,created_at,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT (workspace_id,article_id,locale) DO UPDATE SET
		title=EXCLUDED.title,body_md=EXCLUDED.body_md,source_meta=EXCLUDED.source_meta,source_html=EXCLUDED.source_html,
		created_at=EXCLUDED.created_at,updated_at=EXCLUDED.updated_at`,
		workspaceID, articleID, input.Locale, title, input.BodyMD, input.SourceMeta, input.SourceHTML, input.CreatedAt, input.UpdatedAt); err != nil {
		return fmt.Errorf("import knowledge base translation: %w", err)
	}
	if err := linkKBImages(ctx, tx, workspaceID, articleID, input.BodyMD); err != nil {
		return err
	}
	return bumpKB(ctx, tx, workspaceID)
}

// ImportKBImage records stored image bytes an import brought in. It does not count toward the cap
// (counted false, like imported attachments; a plan that counts imports counts stored bytes). The same
// bytes give the image that already holds them, and created false.
func ImportKBImage(ctx context.Context, tx pgx.Tx, storage attachments.Storage, workspaceID string, object attachments.Object, contentType string) (string, bool, error) {
	existing, err := touchKBImage(ctx, tx, workspaceID, object.SHA256)
	if err == nil {
		return existing.ID, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", false, err
	}
	if err := VerifyObjects(ctx, tx, storage, object.Key); err != nil {
		return "", false, err
	}
	var id string
	if err := tx.QueryRow(ctx, `INSERT INTO kb_images(workspace_id,storage_key,sha256,size,content_type,counted)
		VALUES($1,$2,$3,$4,$5,false) RETURNING id::text`, workspaceID, object.Key, object.SHA256, object.Size, contentType).Scan(&id); err != nil {
		return "", false, fmt.Errorf("import knowledge base image: %w", err)
	}
	return id, true, nil
}
