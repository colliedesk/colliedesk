package freshdesk

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The Freshdesk knowledge base ("solutions"): categories hold folders, folders hold articles and,
// with Freshdesk's Flexible Hierarchy, other folders. The structs follow the public API v2 docs.

type fdSolutionCategory struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	// VisibleInPortals is documented as "allowed only if the account is configured with multiple
	// portals", so it is nil on an account with one portal.
	VisibleInPortals *[]int64  `json:"visible_in_portals"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type fdSolutionFolder struct {
	ID              int64   `json:"id"`
	Name            string  `json:"name"`
	Description     *string `json:"description"`
	ParentFolderID  *int64  `json:"parent_folder_id"`
	ArticlesCount   *int    `json:"articles_count"`
	SubFoldersCount *int    `json:"sub_folders_count"`
	// Visibility: 1 all users, 2 logged-in users, 3 agents, 4 selected companies, 5 bots, 6 and 7
	// selected contact or company segments.
	Visibility        int       `json:"visibility"`
	CompanyIDs        []int64   `json:"company_ids"`
	ContactSegmentIDs []int64   `json:"contact_segment_ids"`
	CompanySegmentIDs []int64   `json:"company_segment_ids"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// fdSolutionArticle: description is the HTML, status 1 draft and 2 published.
type fdSolutionArticle struct {
	ID          int64          `json:"id"`
	Type        int            `json:"type"`
	CategoryID  int64          `json:"category_id"`
	FolderID    int64          `json:"folder_id"`
	AgentID     *int64         `json:"agent_id"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Status      int            `json:"status"`
	Tags        []string       `json:"tags"`
	SEOData     map[string]any `json:"seo_data"`
	Hits        int64          `json:"hits"`
	ThumbsUp    int64          `json:"thumbs_up"`
	ThumbsDown  int64          `json:"thumbs_down"`
	Hierarchy   []any          `json:"hierarchy"`
	// Attachments are not in the documented article attributes; they are read in case the API sends them.
	Attachments []fdAttachment `json:"attachments"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type fdHelpdeskSettings struct {
	PrimaryLanguage    string   `json:"primary_language"`
	SupportedLanguages []string `json:"supported_languages"`
	PortalLanguages    []string `json:"portal_languages"`
}

const (
	entityKBCategory    = "kb_category"
	entityKBFolder      = "kb_folder"
	entityKBArticle     = "kb_article"
	entityKBTranslation = "kb_translation"
	// entityKBImage is an image of an article's HTML that Freshdesk hosts, stored as a kb_images row;
	// its source id is <article id>:<image key> (or <article id>:<language>:<image key>).
	entityKBImage = "kb_image"
)

// Why an article published in Freshdesk is imported as a draft. The strictest reading wins: an
// internal article made public is a leak, a public one kept as a draft costs one click.
const (
	draftInFreshdesk     = "draft_in_freshdesk"
	draftFolderVisible   = "folder_not_visible_to_all_users"
	draftNotInPortal     = "category_not_in_any_portal"
	draftPortalUnknown   = "category_portal_unknown"
	draftSeveralPortals  = "category_in_another_portal"
	visibilityAllUsers   = 1
	kbCollectionLevelMax = 3
)

type kbFolder struct {
	fdSolutionFolder
	categoryID int64
	// listedSubfolders and listedArticles are what the list endpoints returned for this folder.
	listedSubfolders map[int64]bool
	listedArticles   int
	// unreadable is set when a list of this folder failed; its counts are not compared.
	unreadable bool
}

type kbArticle struct {
	fdSolutionArticle
	folder *kbFolder
}

type kbTranslation struct {
	fdSolutionArticle
	language string
}

// knowledgeBase is what the listing found. The listing writes nothing and runs before any write, so
// the limits and the storage cap can be checked for the whole import first.
type knowledgeBase struct {
	notAvailable bool
	// forbidden is the 403 on the category list: the key may only lack the right, so it is UNVERIFIED.
	forbidden  error
	categories []fdSolutionCategory
	folders    map[int64]*kbFolder
	// order holds the folder ids with every parent before its children.
	order        []int64
	articles     []kbArticle
	translations []kbTranslation
	settings     *fdHelpdeskSettings
	languages    []string
	products     *int
	productsNote string
	// failures are lists that could not be read (entity, Freshdesk id, error).
	failures []Failure
	// unverified explains why the counts could not all be compared.
	unverified []string
	listed     bool
}

func (r *run) solutionsPath(parts ...string) string {
	return r.endpoint("/api/v2/solutions/"+strings.Join(parts, "/"), pageQuery())
}

// listSolutions reads the whole knowledge base. A 404 on the category list means the account has no
// knowledge base, a 403 that the key may not read it; any other failing list is recorded and the
// rest still read. Only cancellation stops it.
func (r *run) listSolutions(ctx context.Context) error {
	kb := &knowledgeBase{folders: map[int64]*kbFolder{}, listed: true}
	r.kb = kb
	err := listAll(ctx, r.client, r.solutionsPath("categories"), func(category fdSolutionCategory) error {
		kb.categories = append(kb.categories, category)
		return nil
	})
	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case errors.Is(err, errNotFound):
		kb.notAvailable = true
		return nil
	case errors.Is(err, errForbidden):
		kb.forbidden = err
		return nil
	case err != nil:
		kb.failures = append(kb.failures, Failure{Entity: entityKBCategory, FreshdeskID: "all", Error: "list solution categories: " + err.Error()})
		return nil
	}
	if len(kb.categories) == 0 {
		return nil
	}
	for _, category := range kb.categories {
		id := strconv.FormatInt(category.ID, 10)
		err := listAll(ctx, r.client, r.solutionsPath("categories", id, "folders"), func(folder fdSolutionFolder) error {
			kb.addFolder(folder, category.ID)
			return nil
		})
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			kb.failures = append(kb.failures, Failure{Entity: entityKBCategory, FreshdeskID: id, Error: "list folders: " + err.Error()})
		}
	}
	// Subfolders, breadth first. The category list may already hold nested folders (Flexible
	// Hierarchy): folders are kept once by id and placed by parent_folder_id.
	for queue := slices.Clone(kb.orderedIDs()); len(queue) > 0; queue = queue[1:] {
		folder := kb.folders[queue[0]]
		id := strconv.FormatInt(folder.ID, 10)
		err := listAll(ctx, r.client, r.solutionsPath("folders", id, "subfolders"), func(child fdSolutionFolder) error {
			if child.ParentFolderID == nil {
				child.ParentFolderID = &folder.ID
			}
			if kb.addFolder(child, folder.categoryID) {
				queue = append(queue, child.ID)
			}
			folder.listedSubfolders[child.ID] = true
			return nil
		})
		switch {
		case ctx.Err() != nil:
			return ctx.Err()
		case errors.Is(err, errNotFound):
			// An account without Flexible Hierarchy has no subfolders endpoint.
		case err != nil:
			folder.unreadable = true
			kb.failures = append(kb.failures, Failure{Entity: entityKBFolder, FreshdeskID: id, Error: "list subfolders: " + err.Error()})
		}
	}
	kb.order = kb.orderedIDs()
	for _, folderID := range kb.order {
		folder := kb.folders[folderID]
		id := strconv.FormatInt(folderID, 10)
		err := listAll(ctx, r.client, r.solutionsPath("folders", id, "articles"), func(article fdSolutionArticle) error {
			folder.listedArticles++
			kb.articles = append(kb.articles, kbArticle{fdSolutionArticle: article, folder: folder})
			return nil
		})
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			folder.unreadable = true
			kb.failures = append(kb.failures, Failure{Entity: entityKBFolder, FreshdeskID: id, Error: "list articles: " + err.Error()})
		}
	}
	return r.listTranslations(ctx, kb)
}

// addFolder keeps a folder once; it reports whether the folder was new.
func (kb *knowledgeBase) addFolder(folder fdSolutionFolder, categoryID int64) bool {
	if existing, ok := kb.folders[folder.ID]; ok {
		if existing.ParentFolderID == nil && folder.ParentFolderID != nil {
			existing.ParentFolderID = folder.ParentFolderID
		}
		return false
	}
	kb.folders[folder.ID] = &kbFolder{fdSolutionFolder: folder, categoryID: categoryID, listedSubfolders: map[int64]bool{}}
	return true
}

// parent returns the folder a folder sits in, or nil for a folder directly in its category (or one
// whose parent was never listed).
func (kb *knowledgeBase) parent(folder *kbFolder) *kbFolder {
	if folder.ParentFolderID == nil || *folder.ParentFolderID == folder.ID {
		return nil
	}
	return kb.folders[*folder.ParentFolderID]
}

// path returns the folders from the top of the category down to folder. A loop in parent ids stops it.
func (kb *knowledgeBase) path(folder *kbFolder) []*kbFolder {
	var chain []*kbFolder
	seen := map[int64]bool{}
	for current := folder; current != nil && !seen[current.ID]; current = kb.parent(current) {
		seen[current.ID] = true
		chain = append([]*kbFolder{current}, chain...)
	}
	return chain
}

// orderedIDs lists the folders by depth, then by category and id, so a parent comes before its children.
func (kb *knowledgeBase) orderedIDs() []int64 {
	ids := make([]int64, 0, len(kb.folders))
	for id := range kb.folders {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b int64) int {
		da, db := len(kb.path(kb.folders[a])), len(kb.path(kb.folders[b]))
		if da != db {
			return da - db
		}
		if ca, cb := kb.folders[a].categoryID, kb.folders[b].categoryID; ca != cb {
			return int(ca - cb)
		}
		return int(a - b)
	})
	return ids
}

// listTranslations reads the languages and, for every language other than the primary one, the
// translated articles of every folder. A 404 on a folder means nothing in it is translated.
func (r *run) listTranslations(ctx context.Context, kb *knowledgeBase) error {
	var settings fdHelpdeskSettings
	if _, err := r.client.getJSON(ctx, r.endpoint("/api/v2/settings/helpdesk", nil), &settings); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		kb.unverified = append(kb.unverified, "The languages could not be read from /settings/helpdesk ("+err.Error()+"), so translations could not be counted.")
	} else {
		kb.settings = &settings
		for _, language := range settings.SupportedLanguages {
			if language != settings.PrimaryLanguage && !slices.Contains(kb.languages, language) {
				kb.languages = append(kb.languages, language)
			}
		}
	}
	for _, language := range kb.languages {
		for _, folderID := range kb.order {
			id := strconv.FormatInt(folderID, 10)
			err := listAll(ctx, r.client, r.solutionsPath("folders", id, "articles", language), func(article fdSolutionArticle) error {
				kb.translations = append(kb.translations, kbTranslation{fdSolutionArticle: article, language: language})
				return nil
			})
			switch {
			case ctx.Err() != nil:
				return ctx.Err()
			case errors.Is(err, errNotFound):
			case err != nil:
				kb.unverified = append(kb.unverified, fmt.Sprintf("The %s translations of folder %s could not be listed: %s", language, id, err.Error()))
			}
		}
	}
	return r.countProducts(ctx, kb)
}

// countProducts reads the products, which is where Freshdesk gives a product its own portal. It only
// adds a note: the portal rule reads visible_in_portals.
func (r *run) countProducts(ctx context.Context, kb *knowledgeBase) error {
	var count int
	err := listAll(ctx, r.client, r.endpoint("/api/v2/products", pageQuery()), func(map[string]any) error {
		count++
		return nil
	})
	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case err != nil:
		kb.productsNote = "The products could not be read (" + err.Error() + "); the portals were judged from visible_in_portals alone."
	default:
		kb.products = &count
		if count > 0 && kb.portals() == nil {
			kb.productsNote = fmt.Sprintf("The account has %d product(s) but no category names a portal; it was taken as one portal. Check in Freshdesk which portal shows which category.", count)
		}
	}
	return nil
}

// portals returns the portal ids any category names, sorted; nil when no category has
// visible_in_portals, which the docs give only to accounts with several portals.
func (kb *knowledgeBase) portals() []int64 {
	var ids []int64
	found := false
	for _, category := range kb.categories {
		if category.VisibleInPortals == nil {
			continue
		}
		found = true
		for _, id := range *category.VisibleInPortals {
			if !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
	}
	if !found {
		return nil
	}
	slices.Sort(ids)
	if ids == nil {
		ids = []int64{}
	}
	return ids
}

func (kb *knowledgeBase) category(id int64) *fdSolutionCategory {
	for index := range kb.categories {
		if kb.categories[index].ID == id {
			return &kb.categories[index]
		}
	}
	return nil
}

// checkPortal refuses a --help-center-portal the account does not have.
func (kb *knowledgeBase) checkPortal(chosen int64) error {
	if chosen == 0 || len(kb.categories) == 0 {
		return nil
	}
	portals := kb.portals()
	if !slices.Contains(portals, chosen) {
		names := make([]string, len(portals))
		for index, id := range portals {
			names[index] = strconv.FormatInt(id, 10)
		}
		return fmt.Errorf("--help-center-portal %d is not a portal of this Freshdesk account; the categories name the portals: %s", chosen, strings.Join(names, ", "))
	}
	return nil
}

// draftReason returns why an article is imported as a draft, or "" when it is published: published in
// Freshdesk, in folders all visible to all users, in a category shown in the help center's portal.
func (kb *knowledgeBase) draftReason(article kbArticle, chosenPortal int64) string {
	if article.Status != 2 {
		return draftInFreshdesk
	}
	for _, folder := range kb.path(article.folder) {
		if folder.Visibility != visibilityAllUsers {
			return draftFolderVisible
		}
	}
	portals := kb.portals()
	category := kb.category(article.folder.categoryID)
	switch {
	case category == nil:
		return draftPortalUnknown
	case portals == nil:
		// No category names a portal: the account has one portal and every category is in it.
		return ""
	case category.VisibleInPortals == nil:
		return draftPortalUnknown
	case len(*category.VisibleInPortals) == 0:
		return draftNotInPortal
	case len(portals) == 1:
		return ""
	case chosenPortal != 0 && slices.Contains(*category.VisibleInPortals, chosenPortal):
		return ""
	}
	return draftSeveralPortals
}

// collectionPlacement says where a folder's collection goes: under its category (parent nil) for a
// folder in a category, under the parent folder's collection one level down.
// Freshdesk can nest deeper; such a folder becomes a level-3 collection named after the folders from
// level 3 down to it ("Advanced / Deep") under the level-2 collection.
func (kb *knowledgeBase) collectionPlacement(folder *kbFolder) (parent *kbFolder, name string, flattened bool) {
	chain := kb.path(folder)
	if len(chain) < kbCollectionLevelMax {
		if len(chain) == 2 {
			parent = chain[0]
		}
		return parent, folder.Name, false
	}
	names := make([]string, 0, len(chain)-1)
	for _, step := range chain[1:] {
		names = append(names, strings.TrimSpace(step.Name))
	}
	return chain[0], strings.Join(names, folderSeparator), true
}

// pathNames is the names of the folders from the top of the category down to folder.
func (kb *knowledgeBase) pathNames(folder *kbFolder) []string {
	var names []string
	for _, step := range kb.path(folder) {
		names = append(names, step.Name)
	}
	return names
}
