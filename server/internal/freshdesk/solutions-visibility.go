package freshdesk

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// Freshdesk folder visibility codes that Colliedesk has a visibility for. Every other code (2
// logged-in users, 4 selected companies, 6 and 7 contact or company segments) limits a folder to some
// customers, which Colliedesk cannot yet, so it is kept for agents only.
const (
	visibilityAllUsers = 1
	visibilityAgents   = 3
	visibilityBots     = 5
)

// folderVisibility maps a Freshdesk folder visibility code; limited is true for a code that keeps the
// folder to some customers.
func folderVisibility(code int) (visibility string, limited bool) {
	switch code {
	case visibilityAllUsers:
		return domain.KBVisibleEveryone, false
	case visibilityAgents:
		return domain.KBVisibleAgents, false
	case visibilityBots:
		return domain.KBVisibleHelpChat, false
	}
	return domain.KBVisibleAgents, true
}

// collectionVisibility is the visibility of folder's collection: the folder's own, and for a folder
// flattened beside its parents (collectionPlacement) the most restrictive of it and the folders it no
// longer sits in. The collections above it carry the rest.
func (kb *knowledgeBase) collectionVisibility(folder *kbFolder) string {
	chain := kb.path(folder)
	if len(chain) < kbCollectionLevelMax {
		return mostRestrictive(chain[len(chain)-1:])
	}
	return mostRestrictive(chain[1:])
}

func mostRestrictive(folders []*kbFolder) string {
	visibility := domain.KBVisibleEveryone
	for _, folder := range folders {
		if own, _ := folderVisibility(folder.Visibility); domain.KBVisibilityRank(own) > domain.KBVisibilityRank(visibility) {
			visibility = own
		}
	}
	return visibility
}

// publicInFreshdesk is whether the old portal showed article to everyone: published, in a category of
// the help center's portal and in folders all visible to all users.
func (kb *knowledgeBase) publicInFreshdesk(article kbArticle, chosenPortal int64) bool {
	return kb.draftReason(article, chosenPortal) == "" && mostRestrictive(kb.path(article.folder)) == domain.KBVisibleEveryone
}

// restrictKBCollection gives an imported collection the visibility of its folder in Freshdesk when that
// is more restrictive than the collection's now. An import never shows a collection to more people.
func (r *run) restrictKBCollection(ctx context.Context, key, target, visibility string) error {
	if r.opts.DryRun || visibility == domain.KBVisibleEveryone {
		return nil
	}
	var restricted bool
	err := r.db.InWorkspace(ctx, r.workspaceID, func(tx pgx.Tx) (err error) {
		restricted, err = store.RestrictImportedKBCollection(ctx, tx, r.workspaceID, target, visibility)
		return err
	})
	if err != nil {
		r.itemFailed(ctx, entityKBFolder, key, target, fmt.Errorf("restrict the collection to %s: %w", visibility, err))
		return r.internalErr
	}
	if restricted {
		r.warn("kb_collection_restricted", "Freshdesk now shows this folder to fewer people, so its collection was restricted to match. An import never shows a collection to more people; change it in Colliedesk if it should be.", key)
	}
	return nil
}
