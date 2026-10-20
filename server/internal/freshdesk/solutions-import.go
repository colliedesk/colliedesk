package freshdesk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/mailimage"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

// kbState is what the knowledge base step found while writing, for the report.
type kbState struct {
	// collections maps a category ("c<id>") or folder ("f<id>") to its Colliedesk collection.
	collections   map[string]string
	changedInBoth []string
	gone          []string
}

func (r *run) kbWriteState() *kbState {
	if r.kbState == nil {
		r.kbState = &kbState{collections: map[string]string{}}
	}
	return r.kbState
}

// importKnowledgeBase writes the listed knowledge base: collections first, then articles, then
// their translations. Item problems go to the ledger; only cancellation and ledger errors stop it.
func (r *run) importKnowledgeBase(ctx context.Context) error {
	kb := r.kb
	if kb == nil || len(kb.categories) == 0 {
		return nil
	}
	state := r.kbWriteState()
	for _, category := range kb.categories {
		key := strconv.FormatInt(category.ID, 10)
		meta := map[string]any{"freshdesk_id": category.ID, "kind": "category", "visible_in_portals": category.VisibleInPortals}
		id, err := r.ensureCollection(ctx, entityKBCategory, key, nil, category.Name, deref(category.Description), meta, category.CreatedAt, category.UpdatedAt)
		if err != nil {
			return err
		}
		if id != "" {
			state.collections["c"+key] = id
		}
	}
	for _, folderID := range kb.order {
		folder := kb.folders[folderID]
		key := strconv.FormatInt(folderID, 10)
		parent, name, flattened := kb.collectionPlacement(folder)
		parentKey := "c" + strconv.FormatInt(folder.categoryID, 10)
		if parent != nil {
			parentKey = "f" + strconv.FormatInt(parent.ID, 10)
		}
		parentID, ok := state.collections[parentKey]
		if !ok {
			r.itemFailed(ctx, entityKBFolder, key, "", errors.New("the collection it belongs in was not imported; see its failure"))
			continue
		}
		if flattened {
			r.warn("kb_folder_flattened", "Colliedesk collections nest three levels deep. This Freshdesk folder sits deeper, so it became a collection named after the folders from the third level down (\"Advanced / Deep\") beside them.", key)
		}
		meta := map[string]any{"freshdesk_id": folder.ID, "kind": "folder", "category_id": folder.categoryID, "parent_folder_id": folder.ParentFolderID,
			"visibility": folder.Visibility, "company_ids": folder.CompanyIDs, "contact_segment_ids": folder.ContactSegmentIDs, "company_segment_ids": folder.CompanySegmentIDs,
			"name": folder.Name}
		id, err := r.ensureCollection(ctx, entityKBFolder, key, &parentID, fitName(name), deref(folder.Description), meta, folder.CreatedAt, folder.UpdatedAt)
		if err != nil {
			return err
		}
		if id != "" {
			state.collections["f"+key] = id
		}
	}
	for index, article := range kb.articles {
		if err := r.importKBArticle(ctx, article); err != nil {
			return err
		}
		r.progress.tick("importing knowledge base articles", index+1, len(kb.articles))
	}
	for _, translation := range kb.translations {
		if err := r.importKBTranslation(ctx, translation); err != nil {
			return err
		}
	}
	return r.internalErr
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// fitName keeps a flattened folder name within the collection limit; the full name stays in source_meta.
func fitName(name string) string {
	if runes := []rune(name); len(runes) > domain.MaxKBCollectionName {
		return strings.TrimSpace(string(runes[:domain.MaxKBCollectionName-1])) + "…"
	}
	return name
}

// ensureCollection returns the collection of a category or folder, creating it on the first run. An
// imported collection is never renamed again, so an agent's change survives a rerun. It returns ""
// when the collection could not be created (recorded in the ledger).
func (r *run) ensureCollection(ctx context.Context, entity, key string, parentID *string, name, description string, meta map[string]any, created, updated time.Time) (string, error) {
	target, done, err := r.ledger.done(ctx, entity, key)
	if err != nil {
		return "", err
	}
	if done {
		var exists bool
		if err := r.db.InWorkspace(ctx, r.workspaceID, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM kb_collections WHERE workspace_id=$1 AND id=$2)`, r.workspaceID, target).Scan(&exists)
		}); err != nil {
			return "", fmt.Errorf("read knowledge base collection: %w", err)
		}
		if exists {
			return target, nil
		}
	}
	if r.opts.DryRun {
		return "", nil
	}
	encoded, err := json.Marshal(meta)
	if err != nil {
		return "", err
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin collection import: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	id, err := store.ImportKBCollection(ctx, tx, r.workspaceID, store.KBImportCollection{ParentID: parentID, Name: name, Description: description,
		SlugSuffix: key, SourceMeta: encoded, CreatedAt: created.UTC(), UpdatedAt: updated.UTC()})
	if err != nil {
		_ = tx.Rollback(ctx)
		r.itemFailed(ctx, entity, key, "", err)
		return "", r.internalErr
	}
	if err := r.ledger.mark(ctx, tx, ledgerMark{entity: entity, sourceID: key, targetID: &id, status: ledgerDone}); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit collection import: %w", err)
	}
	return id, nil
}

// kbPendingImage is an image stored in this run whose row is written with its article.
type kbPendingImage struct {
	ledgerID, key, contentType string
	object                     attachments.Object
}

// fetchKBImages brings the Freshdesk-hosted images of one HTML over: those the ledger already holds
// give their image id, the others are downloaded and stored. An image Freshdesk no longer has (404,
// 410) keeps its link and is listed; any other failure is recorded and counted, and the link stays.
func (r *run) fetchKBImages(ctx context.Context, prefix, markup string) (map[string]string, []kbPendingImage, int, error) {
	done, failed := map[string]string{}, 0
	var pending []kbPendingImage
	for _, ref := range r.inlineImages(markup) {
		ledgerID := prefix + ":" + ref.key
		entry, found, err := r.ledger.get(ctx, entityKBImage, ledgerID)
		if err != nil {
			return nil, nil, 0, err
		}
		if found && entry.Status == ledgerDone && entry.TargetID != nil {
			done[ref.key] = *entry.TargetID
			continue
		}
		if err := r.checkDiskSpace(0); err != nil {
			return nil, nil, 0, err
		}
		data, contentType, err := r.fetchImage(ctx, ref.url)
		switch {
		case ctx.Err() != nil:
			return nil, nil, 0, ctx.Err()
		case errors.Is(err, errNotFound), errors.Is(err, errGone):
			detail := fmt.Sprintf("download image %s: %s; the article keeps the Freshdesk link", redact(ref.url), err.Error())
			if err := r.ledger.mark(ctx, nil, ledgerMark{entity: entityKBImage, sourceID: ledgerID, status: ledgerSkipped, errText: detail}); err != nil {
				return nil, nil, 0, err
			}
			continue
		case err != nil:
			r.itemFailed(ctx, entityKBImage, ledgerID, "", fmt.Errorf("download image %s: %w; the article keeps the Freshdesk link", redact(ref.url), err))
			failed++
			continue
		}
		if err := r.checkDiskSpace(int64(len(data))); err != nil {
			return nil, nil, 0, err
		}
		object, err := r.opts.Storage.Put(ctx, r.workspaceID, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			r.itemFailed(ctx, entityKBImage, ledgerID, "", fmt.Errorf("store image: %w", err))
			failed++
			continue
		}
		pending = append(pending, kbPendingImage{ledgerID: ledgerID, key: ref.key, contentType: contentType, object: object})
	}
	return done, pending, failed, r.internalErr
}

// saveKBImages writes the rows of the images stored for one HTML in tx and adds them to done.
func (r *run) saveKBImages(ctx context.Context, tx pgx.Tx, pending []kbPendingImage, done map[string]string) error {
	for _, image := range pending {
		id, created, err := store.ImportKBImage(ctx, tx, r.opts.Storage, r.workspaceID, image.object, image.contentType)
		if err != nil {
			return err
		}
		if err := r.ledger.mark(ctx, tx, ledgerMark{entity: entityKBImage, sourceID: image.ledgerID, targetID: &id, status: ledgerDone, meta: &ticketMeta{Created: created}}); err != nil {
			return err
		}
		done[image.key] = id
	}
	return nil
}

// kbMarkdown points the stored images of an HTML at their knowledge base address and converts it.
func (r *run) kbMarkdown(markup string, images map[string]string) (string, []string, error) {
	rewritten := mailimage.MapSources(markup, func(src string) (string, bool) {
		u, ok := r.hostedImageURL(src)
		if !ok {
			return "", false
		}
		id, found := images[imageKey(u)]
		return domain.KBImagePath + id, found
	})
	return articleMarkdown(rewritten)
}

func (r *run) articleMeta(article kbArticle, draftReason string, dropped []string) ([]byte, error) {
	meta := map[string]any{"freshdesk_id": article.ID, "category_id": article.CategoryID, "folder_id": article.FolderID, "agent_id": article.AgentID,
		"type": article.Type, "status": article.Status, "tags": article.Tags, "seo_data": article.SEOData, "hits": article.Hits,
		"thumbs_up": article.ThumbsUp, "thumbs_down": article.ThumbsDown, "hierarchy": article.Hierarchy}
	if draftReason != "" {
		meta["draft_reason"] = draftReason
	}
	if len(dropped) > 0 {
		meta["markup_dropped"] = dropped
	}
	return json.Marshal(meta)
}

// importKBArticle writes one article: on the first run, again when Freshdesk changed it or an image
// failed last time, and never over a change made in Colliedesk.
func (r *run) importKBArticle(ctx context.Context, article kbArticle) error {
	state := r.kbWriteState()
	key := strconv.FormatInt(article.ID, 10)
	reason := r.kb.draftReason(article, r.opts.HelpCenterPortal)
	entry, found, err := r.ledger.get(ctx, entityKBArticle, key)
	if err != nil {
		return err
	}
	updated := article.UpdatedAt.UTC()
	if found && entry.Status == ledgerDone && entry.SourceUpdatedAt != nil && !updated.After(*entry.SourceUpdatedAt) {
		return nil
	}
	collectionID, ok := state.collections["f"+strconv.FormatInt(article.folder.ID, 10)]
	if !ok {
		r.itemFailed(ctx, entityKBArticle, key, "", errors.New("the collection of its folder was not imported; see its failure"))
		return r.internalErr
	}
	var existing string
	if found && entry.TargetID != nil {
		existing = *entry.TargetID
		var imported bool
		if err := r.db.InWorkspace(ctx, r.workspaceID, func(tx pgx.Tx) (err error) {
			imported, err = store.KBArticleImportedLast(ctx, tx, r.workspaceID, existing)
			return err
		}); err != nil {
			return fmt.Errorf("read knowledge base article: %w", err)
		}
		if !imported {
			return r.keepKBArticle(ctx, key, existing)
		}
	}
	images, pending, failed, err := r.fetchKBImages(ctx, key, article.Description)
	if err != nil {
		return err
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return fmt.Errorf("begin article import: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	fail := func(cause error) error {
		_ = tx.Rollback(ctx)
		r.itemFailed(ctx, entityKBArticle, key, "", cause)
		return r.internalErr
	}
	if err := r.saveKBImages(ctx, tx, pending, images); err != nil {
		return fail(err)
	}
	body, dropped, err := r.kbMarkdown(article.Description, images)
	if err != nil {
		return fail(fmt.Errorf("convert the HTML to Markdown: %w", err))
	}
	if len(dropped) > 0 {
		r.warn("kb_article_markup_dropped", "This article's HTML holds what Markdown cannot show ("+strings.Join(dropped, ", ")+"). It was left out of the article; the original HTML is kept with it and in the export.", key)
	}
	meta, err := r.articleMeta(article, reason, dropped)
	if err != nil {
		return fail(err)
	}
	status := domain.KBPublished
	if reason != "" {
		status = domain.KBDraft
	}
	var author *string
	if article.AgentID != nil {
		if id, matched := r.agentTargets[*article.AgentID]; matched {
			author = &id
		}
	}
	input := store.KBImportArticle{CollectionID: collectionID, Title: article.Title, BodyMD: body, Status: status, AuthorAgentID: author,
		SourceMeta: meta, SourceHTML: article.Description, CreatedAt: article.CreatedAt.UTC(), UpdatedAt: updated}
	target := existing
	if existing == "" {
		target, err = store.ImportKBArticle(ctx, tx, r.workspaceID, input)
	} else {
		var outcome string
		outcome, err = store.ReimportKBArticle(ctx, tx, r.workspaceID, existing, input)
		switch {
		case err == nil && outcome == store.KBKept:
			_ = tx.Rollback(ctx)
			return r.keepKBArticle(ctx, key, existing)
		case err == nil && outcome == store.KBGone:
			_ = tx.Rollback(ctx)
			state.gone = append(state.gone, key)
			return nil
		}
	}
	if err != nil {
		return fail(err)
	}
	ledgerStatus, errText := ledgerDone, ""
	if failed > 0 {
		ledgerStatus, errText = ledgerFailed, fmt.Sprintf("%d image(s) of this article failed; see their entries. The article keeps their Freshdesk links until a rerun brings them over", failed)
	}
	if err := r.ledger.mark(ctx, tx, ledgerMark{entity: entityKBArticle, sourceID: key, targetID: &target, status: ledgerStatus, errText: errText, sourceUpdatedAt: &updated}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit article import: %w", err)
	}
	return nil
}

// keepKBArticle leaves an article someone changed in Colliedesk after the import as it is. Images of
// it that failed are no longer awaited.
func (r *run) keepKBArticle(ctx context.Context, key, target string) error {
	state := r.kbWriteState()
	state.changedInBoth = append(state.changedInBoth, key)
	r.warn("kb_article_changed_in_both", "This article changed in Freshdesk, or had images that failed, and was also changed in Colliedesk after the import (an edit, a restore or a delete). The Colliedesk version was kept; compare it with Freshdesk by hand.", key)
	return r.db.InWorkspace(ctx, r.workspaceID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM import_items WHERE workspace_id=$1 AND source=$2 AND entity=$3 AND status='failed'
			AND (source_id LIKE $4 || ':%')`, r.workspaceID, r.ledger.source, entityKBImage, key); err != nil {
			return fmt.Errorf("write import ledger: %w", err)
		}
		return r.ledger.mark(ctx, tx, ledgerMark{entity: entityKBArticle, sourceID: key, targetID: &target, status: ledgerDone})
	})
}

// importKBTranslation stores one language of an imported article. Translations are kept and exported,
// not shown yet; nobody edits them in Colliedesk, so a newer Freshdesk version always replaces them.
func (r *run) importKBTranslation(ctx context.Context, translation kbTranslation) error {
	articleKey := strconv.FormatInt(translation.ID, 10)
	key := articleKey + ":" + translation.language
	entry, found, err := r.ledger.get(ctx, entityKBTranslation, key)
	if err != nil {
		return err
	}
	updated := translation.UpdatedAt.UTC()
	if found && entry.Status == ledgerDone && entry.SourceUpdatedAt != nil && !updated.After(*entry.SourceUpdatedAt) {
		return nil
	}
	articleID, done, err := r.ledger.done(ctx, entityKBArticle, articleKey)
	if err != nil {
		return err
	}
	if !done {
		if failedArticle, ok, err := r.ledger.get(ctx, entityKBArticle, articleKey); err != nil {
			return err
		} else if ok && failedArticle.TargetID != nil {
			articleID, done = *failedArticle.TargetID, true
		}
	}
	if !done {
		r.itemFailed(ctx, entityKBTranslation, key, "", errors.New("its article was not imported; see its failure"))
		return r.internalErr
	}
	images, pending, failed, err := r.fetchKBImages(ctx, key, translation.Description)
	if err != nil {
		return err
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return fmt.Errorf("begin translation import: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	fail := func(cause error) error {
		_ = tx.Rollback(ctx)
		r.itemFailed(ctx, entityKBTranslation, key, "", cause)
		return r.internalErr
	}
	if err := r.saveKBImages(ctx, tx, pending, images); err != nil {
		return fail(err)
	}
	body, dropped, err := r.kbMarkdown(translation.Description, images)
	if err != nil {
		return fail(fmt.Errorf("convert the HTML to Markdown: %w", err))
	}
	if len(dropped) > 0 {
		r.warn("kb_article_markup_dropped", "This article's HTML holds what Markdown cannot show ("+strings.Join(dropped, ", ")+"). It was left out of the article; the original HTML is kept with it and in the export.", key)
	}
	portal := r.kb.settings != nil && slices.Contains(r.kb.settings.PortalLanguages, translation.language)
	meta, err := json.Marshal(map[string]any{"freshdesk_id": translation.ID, "status": translation.Status, "tags": translation.Tags, "seo_data": translation.SEOData,
		"hits": translation.Hits, "thumbs_up": translation.ThumbsUp, "thumbs_down": translation.ThumbsDown, "portal_language": portal, "markup_dropped": dropped})
	if err != nil {
		return fail(err)
	}
	if err := store.PutKBArticleTranslation(ctx, tx, r.workspaceID, articleID, store.KBImportTranslation{Locale: translation.language, Title: translation.Title,
		BodyMD: body, SourceMeta: meta, SourceHTML: translation.Description, CreatedAt: translation.CreatedAt.UTC(), UpdatedAt: updated}); err != nil {
		return fail(err)
	}
	ledgerStatus, errText := ledgerDone, ""
	if failed > 0 {
		ledgerStatus, errText = ledgerFailed, fmt.Sprintf("%d image(s) of this translation failed; see their entries", failed)
	}
	if err := r.ledger.mark(ctx, tx, ledgerMark{entity: entityKBTranslation, sourceID: key, targetID: &articleID, status: ledgerStatus, errText: errText, sourceUpdatedAt: &updated}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit translation import: %w", err)
	}
	return nil
}
