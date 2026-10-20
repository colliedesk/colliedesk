package freshdesk

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

const kbVerificationMethod = "Freshdesk's articles_count of every folder compared with the articles listed in it, and its sub_folders_count with the subfolders listed; " +
	"categories and folders directly in a category are listed only (Freshdesk reports no count for them); translations are the languages of /settings/helpdesk, listed folder by folder."

// zeroImageID stands in for an image id while the length of a text is checked before anything is stored.
const zeroImageID = "00000000-0000-0000-0000-000000000000"

// KnowledgeBaseLimitError means the knowledge base does not fit Colliedesk's limits; nothing was imported.
type KnowledgeBaseLimitError struct{ Problems []string }

func (e *KnowledgeBaseLimitError) Error() string {
	shown := e.Problems
	if len(shown) > 10 {
		shown = append(slices.Clone(shown[:10]), fmt.Sprintf("… %d more", len(e.Problems)-10))
	}
	return "the Freshdesk knowledge base does not fit Colliedesk's limits, so nothing was imported (a text is never cut): " + strings.Join(shown, "; ") +
		". Split or shorten those articles in Freshdesk, or delete knowledge base articles in Colliedesk, then run the import again"
}

// kbLimitProblems checks, before anything is written, every value the knowledge base tables refuse
// and the workspace limits, so the import never stops halfway through the knowledge base.
func (r *run) kbLimitProblems(ctx context.Context) ([]string, error) {
	kb := r.kb
	if kb == nil || len(kb.categories) == 0 {
		return nil, nil
	}
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	newCollections, newArticles := 0, 0
	for _, category := range kb.categories {
		key := strconv.FormatInt(category.ID, 10)
		if _, done, err := r.ledger.done(ctx, entityKBCategory, key); err != nil {
			return nil, err
		} else if !done {
			newCollections++
		}
		if _, err := domain.CleanKBText("name", category.Name, domain.MaxKBCollectionName); err != nil {
			add("category %s: %s", key, err.Error())
		}
		if err := domain.CheckKBDescription(deref(category.Description)); err != nil {
			add("category %s: %s", key, err.Error())
		}
	}
	for _, folderID := range kb.order {
		folder := kb.folders[folderID]
		key := strconv.FormatInt(folderID, 10)
		if _, done, err := r.ledger.done(ctx, entityKBFolder, key); err != nil {
			return nil, err
		} else if !done {
			newCollections++
		}
		_, name, flattened := kb.collectionPlacement(folder)
		if flattened {
			name = fitName(name)
		}
		if _, err := domain.CleanKBText("name", name, domain.MaxKBCollectionName); err != nil {
			add("folder %s: %s", key, err.Error())
		}
		if err := domain.CheckKBDescription(deref(folder.Description)); err != nil {
			add("folder %s: %s", key, err.Error())
		}
	}
	seen := map[int64]bool{}
	for _, article := range kb.articles {
		key := strconv.FormatInt(article.ID, 10)
		if !seen[article.ID] {
			seen[article.ID] = true
			if entry, found, err := r.ledger.get(ctx, entityKBArticle, key); err != nil {
				return nil, err
			} else if !found || entry.TargetID == nil {
				newArticles++
			}
		}
		if err := r.checkKBText(article.Title, article.Description); err != nil {
			add("article %s: %s", key, err.Error())
		}
	}
	for _, translation := range kb.translations {
		key := strconv.FormatInt(translation.ID, 10) + ":" + translation.language
		if err := domain.CheckLocale(translation.language); err != nil {
			add("translation %s: %s", key, err.Error())
		}
		if err := r.checkKBText(translation.Title, translation.Description); err != nil {
			add("translation %s: %s", key, err.Error())
		}
	}
	var articles, collections int
	if err := r.db.InWorkspace(ctx, r.workspaceID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM kb_articles WHERE workspace_id=$1), (SELECT count(*) FROM kb_collections WHERE workspace_id=$1)`,
			r.workspaceID).Scan(&articles, &collections)
	}); err != nil {
		return nil, fmt.Errorf("count knowledge base: %w", err)
	}
	if articles+newArticles > domain.MaxKBArticles {
		add("the workspace holds %d knowledge base articles (the trash counts) and the import adds %d, over the limit of %d", articles, newArticles, domain.MaxKBArticles)
	}
	if collections+newCollections > domain.MaxKBCollections {
		add("the workspace holds %d knowledge base collections (the trash counts) and the import adds %d, over the limit of %d", collections, newCollections, domain.MaxKBCollections)
	}
	return problems, nil
}

func (r *run) checkKBText(title, markup string) error {
	if _, err := domain.CleanKBText("title", title, domain.MaxKBArticleTitle); err != nil {
		return err
	}
	images := map[string]string{}
	for _, ref := range r.inlineImages(markup) {
		images[ref.key] = zeroImageID
	}
	body, _, err := r.kbMarkdown(markup, images)
	if err != nil {
		return fmt.Errorf("the HTML cannot be converted: %w", err)
	}
	return domain.CheckKBBody(body)
}

// sizeKnowledgeBase adds up the bytes of the article images a run would store: every image the
// ledger does not hold, read once (Freshdesk reports no size).
func (r *run) sizeKnowledgeBase(ctx context.Context) (int64, error) {
	if r.kb == nil {
		return 0, nil
	}
	var needed int64
	size := func(prefix, markup string) error {
		for _, ref := range r.inlineImages(markup) {
			if entry, found, err := r.ledger.get(ctx, entityKBImage, prefix+":"+ref.key); err != nil {
				return err
			} else if found && entry.Status == ledgerDone {
				continue
			}
			bytes, err := r.measureImage(ctx, ref)
			if err != nil {
				return err
			}
			needed += bytes
		}
		return nil
	}
	for _, article := range r.kb.articles {
		if err := size(strconv.FormatInt(article.ID, 10), article.Description); err != nil {
			return 0, err
		}
	}
	for _, translation := range r.kb.translations {
		if err := size(strconv.FormatInt(translation.ID, 10)+":"+translation.language, translation.Description); err != nil {
			return 0, err
		}
	}
	return needed, nil
}

// countKnowledgeBase compares what the listing found with the ledger, and Freshdesk's own folder
// counts with the lists.
func (r *run) countKnowledgeBase(ctx context.Context, report *Report, _, _ []string) error {
	kb := r.kb
	count := KnowledgeBaseCount{Published: []string{}, Drafts: map[string][]string{}, Languages: []string{}, Portals: []int64{}, Notes: []string{},
		ChangedInBoth: []string{}, Unpublished: []string{}, DeletedInColliedesk: []string{}, Refused: r.kbRefused}
	if count.Refused == nil {
		count.Refused = []string{}
	}
	count.Images.GoneImages = []KBGoneImage{}
	verification := Verification{Verified: true, Method: kbVerificationMethod}
	defer func() {
		report.KnowledgeBase = count
		report.Verification.KnowledgeBase = verification
	}()
	switch {
	case kb == nil:
		verification.Detail = "The knowledge base was not read."
		return nil
	case kb.notAvailable:
		count.NotAvailable = true
		verification.Detail = "This Freshdesk account has no knowledge base (the solution category list answered 404), so there was nothing to compare."
		return nil
	case kb.forbidden != nil:
		verification.Verified = false
		verification.Detail = "Freshdesk refused the knowledge base (" + kb.forbidden.Error() + "). The API key may lack the right to read solutions, so the articles could not be counted; nothing was imported from it."
		return nil
	}
	if state := r.kbState; state != nil {
		count.ChangedInBoth, count.DeletedInColliedesk = uniqueSorted(state.changedInBoth), uniqueSorted(state.gone)
		count.Unpublished = uniqueSorted(state.unpublished)
	}
	count.Languages = append(count.Languages, kb.languages...)
	if portals := kb.portals(); portals != nil {
		count.Portals = portals
	}
	count.Products = kb.products
	if kb.productsNote != "" {
		count.Notes = append(count.Notes, kb.productsNote)
	}
	report.Failures = append(report.Failures, kb.failures...)

	var mismatches []string
	collectionKeys := map[string][]string{entityKBCategory: {}, entityKBFolder: {}}
	for _, category := range kb.categories {
		collectionKeys[entityKBCategory] = append(collectionKeys[entityKBCategory], strconv.FormatInt(category.ID, 10))
	}
	for _, folderID := range kb.order {
		folder := kb.folders[folderID]
		collectionKeys[entityKBFolder] = append(collectionKeys[entityKBFolder], strconv.FormatInt(folderID, 10))
		if folder.unreadable {
			continue
		}
		if folder.ArticlesCount != nil && *folder.ArticlesCount != folder.listedArticles {
			mismatches = append(mismatches, fmt.Sprintf("folder %d reports %d articles, %d were listed", folderID, *folder.ArticlesCount, folder.listedArticles))
		}
		if folder.SubFoldersCount != nil && *folder.SubFoldersCount != len(folder.listedSubfolders) {
			mismatches = append(mismatches, fmt.Sprintf("folder %d reports %d subfolders, %d were listed", folderID, *folder.SubFoldersCount, len(folder.listedSubfolders)))
		}
	}
	var articleKeys, translationKeys []string
	images := 0
	for _, article := range kb.articles {
		key := strconv.FormatInt(article.ID, 10)
		articleKeys = append(articleKeys, key)
		if reason := kb.draftReason(article, r.opts.HelpCenterPortal); reason == "" {
			count.Published = append(count.Published, key)
		} else {
			count.Drafts[reason] = append(count.Drafts[reason], key)
		}
		images += len(r.inlineImages(article.Description))
	}
	for _, translation := range kb.translations {
		translationKeys = append(translationKeys, strconv.FormatInt(translation.ID, 10)+":"+translation.language)
		images += len(r.inlineImages(translation.Description))
	}
	articleKeys, translationKeys = uniqueSorted(articleKeys), uniqueSorted(translationKeys)
	count.Published = uniqueSorted(count.Published)
	for reason, ids := range count.Drafts {
		count.Drafts[reason] = uniqueSorted(ids)
	}
	count.Collections.Source = len(collectionKeys[entityKBCategory]) + len(collectionKeys[entityKBFolder])
	count.Articles.Source, count.Translations.Source = len(articleKeys), len(translationKeys)
	if r.opts.DryRun {
		count.Images.Source = images
	} else if err := r.countKBLedger(ctx, report, &count, collectionKeys, articleKeys, translationKeys); err != nil {
		return err
	}

	details := append(mismatches, kb.unverified...)
	if len(details) > 0 {
		verification.Verified = false
		verification.Detail = strings.Join(details, "; ") + "."
	} else {
		verification.Detail = fmt.Sprintf("%d categories and %d folders listed; every folder count matched its list (%d articles, %d translations in %d language(s)).",
			len(kb.categories), len(kb.order), len(articleKeys), len(translationKeys), len(kb.languages))
	}
	return nil
}

// countKBLedger fills the imported and failed counts from the ledger and adds its failures.
func (r *run) countKBLedger(ctx context.Context, report *Report, count *KnowledgeBaseCount, collections map[string][]string, articles, translations []string) error {
	return r.db.InWorkspace(ctx, r.workspaceID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT entity,source_id,status,error FROM import_items WHERE workspace_id=$1 AND source=$2 AND (
				(entity=$3 AND source_id=ANY($4)) OR (entity=$5 AND source_id=ANY($6)) OR (entity=$7 AND source_id=ANY($8))
				OR (entity=$9 AND source_id=ANY($10)) OR (entity=$11 AND split_part(source_id,':',1)=ANY($8)))
			ORDER BY entity,length(source_id),source_id`, r.workspaceID, r.ledger.source,
			entityKBCategory, collections[entityKBCategory], entityKBFolder, collections[entityKBFolder], entityKBArticle, articles,
			entityKBTranslation, translations, entityKBImage)
		if err != nil {
			return fmt.Errorf("reconcile knowledge base: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var entity, id, status, message string
			if err := rows.Scan(&entity, &id, &status, &message); err != nil {
				return fmt.Errorf("reconcile knowledge base: %w", err)
			}
			switch entity {
			case entityKBCategory, entityKBFolder:
				addStatus(&count.Collections, status)
			case entityKBArticle:
				addStatus(&count.Articles, status)
			case entityKBTranslation:
				addStatus(&count.Translations, status)
			case entityKBImage:
				count.Images.Source++
				addStatus(&count.Images.EntityCount, status)
				if status == ledgerSkipped {
					count.Images.GoneImages = append(count.Images.GoneImages, KBGoneImage{ArticleID: strings.SplitN(id, ":", 2)[0], Detail: message})
				}
			}
			if status == ledgerFailed {
				report.Failures = append(report.Failures, Failure{Entity: entity, FreshdeskID: id, Error: message})
			}
		}
		count.Images.Gone = len(count.Images.GoneImages)
		return rows.Err()
	})
}

// kbStatsTables are the knowledge base tables an undo reads.
var kbStatsTables = []string{"kb_articles", "kb_article_versions", "kb_collections", "kb_media", "kb_article_media", "kb_article_translations"}
