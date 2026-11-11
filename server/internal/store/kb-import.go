package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

// ImportKBCollection adds a collection with the source's timestamps, after its siblings once they were
// ordered by hand.
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
			err := sub.QueryRow(ctx, `INSERT INTO kb_collections(workspace_id,parent_id,name,slug,description,source_meta,created_at,updated_at,position)
				VALUES($1,$2,$3,$4,$5,$6,$7,$8,`+kbCollectionEnd(`$2::uuid`, `NULL::uuid`)+`) RETURNING id::text`,
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

// ImportKBArticle adds an article with the next number and its first version, of kind import; it goes
// last in a collection ordered by hand.
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
			source_meta,source_html,created_at,updated_at,position)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,`+kbArticleEnd(`$2::uuid`)+`) RETURNING id::text`,
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
// locked before the check, so an edit cannot land between the check and the write; the knowledge base
// lock comes before it, as for every write that may place an article: one moved to another folder goes
// last there once that collection was ordered by hand.
func ReimportKBArticle(ctx context.Context, tx pgx.Tx, workspaceID, id string, input KBImportArticle) (string, error) {
	if err := cleanKBImportArticle(&input); err != nil {
		return "", err
	}
	if err := lockKB(ctx, tx, workspaceID); err != nil {
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
	var wasTitle, wasBody string
	if err := tx.QueryRow(ctx, `SELECT title, body_md FROM kb_articles WHERE workspace_id=$1 AND id=$2`, workspaceID, id).Scan(&wasTitle, &wasBody); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `UPDATE kb_articles SET position=CASE WHEN collection_id<>$3::uuid THEN `+kbArticleEnd(`$3::uuid`)+` ELSE position END,
			collection_id=$3,title=$4,slug=$5,body_md=$6,status=$7,published_at=$8,author_agent_id=$9,
			source_meta=$10,source_html=$11,created_at=$12,updated_at=$13 WHERE workspace_id=$1 AND id=$2`,
		workspaceID, id, input.CollectionID, input.Title, domain.Slugify(input.Title, "article"), input.BodyMD, input.Status, publishedAt(input),
		input.AuthorAgentID, input.SourceMeta, input.SourceHTML, input.CreatedAt, input.UpdatedAt); err != nil {
		return "", fmt.Errorf("import knowledge base article: %w", err)
	}
	// The source's newer article leaves its translations behind until their own newer versions come.
	if err := markKBTranslationsOutdated(ctx, tx, workspaceID, id, wasTitle, wasBody); err != nil {
		return "", err
	}
	return KBReimported, addKBArticleVersion(ctx, tx, workspaceID, id, Actor{}, domain.KBArticleImported, nil)
}

// UnpublishImportedKBArticle turns a published article back into a draft, as a new version of kind
// import, when the source no longer shows it publicly (a folder or portal changed, not the article)
// and nobody changed it in Colliedesk since the last import. It returns KBReimported when it did,
// KBKept when someone changed it, KBGone when it no longer exists, and "" when it was not published.
func UnpublishImportedKBArticle(ctx context.Context, tx pgx.Tx, workspaceID, id string) (string, error) {
	var live bool
	var status string
	err := tx.QueryRow(ctx, `SELECT deleted_at IS NULL, status FROM kb_articles WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, workspaceID, id).Scan(&live, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return KBGone, nil
	}
	if err != nil || !live || status != domain.KBPublished {
		return "", err
	}
	_, imported, err := KBArticleImportedLast(ctx, tx, workspaceID, id)
	if err != nil {
		return "", err
	}
	if !imported {
		return KBKept, nil
	}
	if _, err := tx.Exec(ctx, `UPDATE kb_articles SET status='draft', published_at=NULL WHERE workspace_id=$1 AND id=$2`, workspaceID, id); err != nil {
		return "", fmt.Errorf("unpublish imported knowledge base article: %w", err)
	}
	return KBReimported, addKBArticleVersion(ctx, tx, workspaceID, id, Actor{}, domain.KBArticleImported, nil)
}

// KBImportTranslation is one language of an article as an import reads it. Status is draft or
// published (published shows only while the article is published and the language is shown).
type KBImportTranslation struct {
	Locale, Title, BodyMD, Status string
	SEOTitle, SEODescription      string
	SourceMeta                    []byte
	SourceHTML                    string
	CreatedAt, UpdatedAt          time.Time
}

// KBTranslationImportedLast tells whether a translation exists and whether its newest version is an import.
func KBTranslationImportedLast(ctx context.Context, tx pgx.Tx, workspaceID, articleID, locale string) (exists, imported bool, err error) {
	var kind string
	err = tx.QueryRow(ctx, `SELECT kind FROM kb_article_translation_versions WHERE workspace_id=$1 AND article_id=$2 AND locale=$3 ORDER BY version DESC LIMIT 1`,
		workspaceID, articleID, locale).Scan(&kind)
	if errors.Is(err, pgx.ErrNoRows) {
		var found bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM kb_article_translations WHERE workspace_id=$1 AND article_id=$2 AND locale=$3)`,
			workspaceID, articleID, locale).Scan(&found); err != nil {
			return false, false, err
		}
		// A translation with no version at all was written before versions existed: the import's.
		return found, found, nil
	}
	return err == nil, kind == domain.KBArticleImported, err
}

// PutKBArticleTranslation adds a translation of an article, or writes the source's newer one over it
// while nobody changed it in Colliedesk since the last import (its newest version is the import's), as
// a version of kind import, up to date with the article. It returns KBReimported when it wrote and
// KBKept when someone changed it (an edit, a restore, a delete): that version stays. The article row is
// locked first, as on every path that writes a translation, and the images its text points at are
// linked to the article, so the purge keeps them.
func PutKBArticleTranslation(ctx context.Context, tx pgx.Tx, workspaceID, articleID string, input KBImportTranslation) (string, error) {
	if err := domain.CheckLocale(input.Locale); err != nil {
		return "", err
	}
	title, err := domain.CleanKBText("title", input.Title, domain.MaxKBArticleTitle)
	if err != nil {
		return "", err
	}
	if err := domain.CheckKBBody(input.BodyMD); err != nil {
		return "", err
	}
	if input.Status == "" {
		input.Status = domain.KBDraft
	}
	if err := domain.CheckKBStatus(input.Status); err != nil {
		return "", err
	}
	// Search fields that do not fit are left empty (the defaults), never cut; the source keeps them.
	if input.SEOTitle, err = domain.CleanKBSEO("search title", input.SEOTitle, domain.MaxKBSEOTitle); err != nil {
		input.SEOTitle = ""
	}
	if input.SEODescription, err = domain.CleanKBSEO("meta description", input.SEODescription, domain.MaxKBSEODescription); err != nil {
		input.SEODescription = ""
	}
	if err := lockKBArticleRow(ctx, tx, workspaceID, articleID); err != nil {
		return "", err
	}
	exists, imported, err := KBTranslationImportedLast(ctx, tx, workspaceID, articleID, input.Locale)
	if err != nil {
		return "", err
	}
	if exists && !imported {
		return KBKept, nil
	}
	var published *time.Time
	if input.Status == domain.KBPublished {
		published = &input.UpdatedAt
	}
	if _, err := tx.Exec(ctx, `INSERT INTO kb_article_translations(workspace_id,article_id,locale,title,slug,body_md,status,published_at,seo_title,seo_description,
			source_meta,source_html,created_at,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) ON CONFLICT (workspace_id,article_id,locale) DO UPDATE SET
		title=EXCLUDED.title,slug=EXCLUDED.slug,body_md=EXCLUDED.body_md,status=EXCLUDED.status,published_at=EXCLUDED.published_at,
		seo_title=EXCLUDED.seo_title,seo_description=EXCLUDED.seo_description,source_meta=EXCLUDED.source_meta,source_html=EXCLUDED.source_html,
		outdated=false,outdated_at=NULL,deleted_at=NULL,deleted_by_agent_id=NULL,created_at=EXCLUDED.created_at,updated_at=EXCLUDED.updated_at`,
		workspaceID, articleID, input.Locale, title, domain.Slugify(title, "article"), input.BodyMD, input.Status, published, input.SEOTitle, input.SEODescription,
		input.SourceMeta, input.SourceHTML, input.CreatedAt, input.UpdatedAt); err != nil {
		return "", fmt.Errorf("import knowledge base translation: %w", err)
	}
	return KBReimported, addKBTranslationVersion(ctx, tx, workspaceID, articleID, input.Locale, Actor{}, domain.KBArticleImported, nil)
}

// ImportKBCollectionTranslation writes a collection's name and description in one language as the
// source has it: added when absent (a person who removed it gets it back from the next run, which is
// how an imported name is never lost), replaced while it is still as an import wrote it, and left as
// it is once a person set it. It reports whether it wrote.
func ImportKBCollectionTranslation(ctx context.Context, tx pgx.Tx, workspaceID, collectionID, locale, name, description string, sourceMeta []byte) (bool, error) {
	if err := domain.CheckLocale(locale); err != nil {
		return false, err
	}
	name, err := domain.CleanKBText("name", name, domain.MaxKBCollectionName)
	if err != nil {
		return false, err
	}
	if err := domain.CheckKBDescription(description); err != nil {
		return false, err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO kb_collection_translations(workspace_id,collection_id,locale,name,description,source_meta,created_at,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,clock_timestamp(),clock_timestamp())
		ON CONFLICT (workspace_id,collection_id,locale) DO UPDATE SET name=EXCLUDED.name, description=EXCLUDED.description, source_meta=EXCLUDED.source_meta,
			updated_at=clock_timestamp()
		WHERE kb_collection_translations.source_meta IS NOT NULL
			AND (kb_collection_translations.name, kb_collection_translations.description, kb_collection_translations.source_meta) IS DISTINCT FROM (EXCLUDED.name, EXCLUDED.description, EXCLUDED.source_meta)`,
		workspaceID, collectionID, locale, name, description, sourceMeta)
	if err != nil {
		return false, fmt.Errorf("import knowledge base collection translation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	return true, bumpKB(ctx, tx, workspaceID)
}

// ImportHelpCenterLanguages adds the languages an import reads to the help center, each shown when
// the source showed it: only languages the help center does not list yet, so a run never undoes what
// an admin set. It returns the locales it added. The primary language is never added.
func ImportHelpCenterLanguages(ctx context.Context, tx pgx.Tx, workspaceID string, locales []string, visible map[string]bool) ([]string, error) {
	var primary string
	if err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT locale FROM help_centers WHERE workspace_id=$1), 'en')`, workspaceID).Scan(&primary); err != nil {
		return nil, err
	}
	shown := make([]bool, 0, len(locales))
	keep := make([]string, 0, len(locales))
	for _, locale := range locales {
		if domain.CheckLocale(locale) != nil || strings.EqualFold(locale, primary) {
			continue
		}
		keep, shown = append(keep, locale), append(shown, visible[locale])
	}
	rows, err := tx.Query(ctx, `INSERT INTO help_center_languages(workspace_id,locale,visible,position)
		SELECT $1, x.locale, x.visible, (SELECT COALESCE(max(position),0) FROM help_center_languages WHERE workspace_id=$1) + x.n
		FROM unnest($2::text[], $3::boolean[]) WITH ORDINALITY AS x(locale, visible, n)
		ON CONFLICT (workspace_id, locale) DO NOTHING RETURNING locale`, workspaceID, keep, shown)
	if err != nil {
		return nil, fmt.Errorf("import help center languages: %w", err)
	}
	added, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("import help center languages: %w", err)
	}
	if len(added) > 0 {
		if err := bumpKB(ctx, tx, workspaceID); err != nil {
			return nil, err
		}
	}
	return added, nil
}

// AdoptImportedPrimaryLanguage makes locale the help center's primary language when it is safe: the
// help center lists no other language and the knowledge base holds no article an import did not
// bring (articles are written in the source's primary language). It reports whether the primary
// language is locale afterwards and, when this call changed it, the language it replaced (previous ""
// when it changed nothing), which undo gives back.
func AdoptImportedPrimaryLanguage(ctx context.Context, tx pgx.Tx, workspaceID, locale string) (adopted bool, previous string, err error) {
	if domain.CheckLocale(locale) != nil {
		return false, "", nil
	}
	var current string
	if err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT locale FROM help_centers WHERE workspace_id=$1), 'en')`, workspaceID).Scan(&current); err != nil {
		return false, "", err
	}
	if strings.EqualFold(current, locale) {
		return true, "", nil
	}
	var blocked bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM help_center_languages WHERE workspace_id=$1)
		OR EXISTS(SELECT 1 FROM kb_articles a WHERE a.workspace_id=$1 AND a.source_meta IS NULL)
		OR EXISTS(SELECT 1 FROM kb_article_translations t WHERE t.workspace_id=$1 AND t.locale=$2)`, workspaceID, locale).Scan(&blocked); err != nil {
		return false, "", err
	}
	if blocked {
		return false, "", nil
	}
	if _, err := tx.Exec(ctx, `INSERT INTO help_centers(workspace_id,locale,content_version,updated_at) VALUES($1,$2,1,clock_timestamp())
		ON CONFLICT (workspace_id) DO UPDATE SET locale=EXCLUDED.locale, content_version=help_centers.content_version+1, updated_at=clock_timestamp()`, workspaceID, locale); err != nil {
		return false, "", fmt.Errorf("set the help center's primary language: %w", err)
	}
	return true, current, nil
}

// ImportKBImage records stored image bytes an import brought in, with the name and alt text the import
// found (an import never keeps an item: an image no article links goes with the purge). It does not count toward the cap
// (counted false, like imported attachments; a plan that counts imports counts stored bytes). The same
// bytes give the image that already holds them, and created false.
func ImportKBImage(ctx context.Context, tx pgx.Tx, storage attachments.Storage, workspaceID string, object attachments.Object, contentType string, upload domain.KBImageUpload) (string, bool, error) {
	upload, err := cleanKBImageUpload(domain.KBImageUpload{Name: upload.Name, Alt: upload.Alt})
	if err != nil {
		return "", false, err
	}
	existing, err := touchKBImage(ctx, tx, workspaceID, object.SHA256, upload)
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
	if err := tx.QueryRow(ctx, `INSERT INTO kb_media(workspace_id,storage_key,sha256,size,content_type,counted,name,alt)
		VALUES($1,$2,$3,$4,$5,false,$6,$7) RETURNING id::text`, workspaceID, object.Key, object.SHA256, object.Size, contentType, upload.Name, upload.Alt).Scan(&id); err != nil {
		return "", false, fmt.Errorf("import knowledge base image: %w", err)
	}
	return id, true, nil
}
