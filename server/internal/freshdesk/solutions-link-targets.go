package freshdesk

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/helpcenter"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

// Where the old help center addresses of an imported account lead now: the help center links point at
// (linkSite), the new address of every imported knowledge base item (linkTargets) and the old portal's
// public addresses (oldAddresses).

// linkSite reads the help center's address, whether it is on, and the hosts of this account's links.
func (r *run) linkSite(ctx context.Context) (linkSite, error) {
	site := linkSite{origin: r.opts.Origin != "", hosts: map[string]bool{strings.ToLower(r.client.base.Host): true}}
	if r.accountDomain != "" {
		site.hosts[r.accountDomain] = true
	}
	_, open, err := store.NewWithDB(r.db).OpenHelpCenterWorkspace(ctx, r.workspaceID)
	if err != nil {
		return site, err
	}
	var slug, custom, claimed string
	if err := r.db.InWorkspace(ctx, r.workspaceID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT w.slug, COALESCE((SELECT hostname FROM help_center_domains d WHERE d.workspace_id=w.id AND d.status='active'), ''),
				COALESCE((SELECT hostname FROM help_center_domains d WHERE d.workspace_id=w.id), '')
			FROM workspaces w WHERE w.id=$1`, r.workspaceID).Scan(&slug, &custom, &claimed)
	}); err != nil {
		return site, fmt.Errorf("read the help center address: %w", err)
	}
	if claimed != "" {
		site.hosts[claimed] = true
	}
	if site.origin {
		site.address = helpcenter.SiteURL(r.opts.Origin, r.opts.HelpCenterDomain, slug, custom)
	}
	site.open = open && site.origin
	return site, nil
}

// linkTargets maps "<entity>:<Freshdesk id>" of every knowledge base item this account's ledger
// holds to where its old address leads: the new address of a published article or of a collection
// with something published in or below it, else the reason. address is the help center's.
func (r *run) linkTargets(ctx context.Context, address string) (map[string]linkTarget, error) {
	data := store.NewWithDB(r.db)
	collections, err := data.HelpCenterCollections(ctx, r.workspaceID)
	if err != nil {
		return nil, err
	}
	paths := helpcenter.PublicCollectionPaths(collections)
	targets := map[string]linkTarget{}
	err = r.db.InWorkspace(ctx, r.workspaceID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT i.entity, i.source_id, i.status='done', a.number, a.slug, a.status='published' AND a.deleted_at IS NULL, c.id::text
			FROM import_items i
			LEFT JOIN kb_articles a ON i.entity='kb_article' AND a.workspace_id=i.workspace_id AND a.id=i.target_id
			LEFT JOIN kb_collections c ON i.entity IN ('kb_category','kb_folder') AND c.workspace_id=i.workspace_id AND c.id=i.target_id
			WHERE i.workspace_id=$1 AND i.source=$2 AND i.entity IN ('kb_category','kb_folder','kb_article')`, r.workspaceID, r.ledger.source)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var entity, sourceID string
			var done bool
			var number *int64
			var slug, collection *string
			var published *bool
			if err := rows.Scan(&entity, &sourceID, &done, &number, &slug, &published, &collection); err != nil {
				return err
			}
			target := linkTarget{reason: LinkNotImported}
			switch {
			case !done:
			case number != nil && published != nil && *published:
				target = linkTarget{address: address + helpcenter.ArticlePath(*number, *slug)}
			case number != nil:
				target.reason = LinkNotPublic
			case collection != nil && paths[*collection] != "":
				target = linkTarget{address: address + paths[*collection]}
			case collection != nil:
				target.reason = LinkNotPublic
			}
			targets[entity+":"+sourceID] = target
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("read the imported knowledge base: %w", err)
	}
	return targets, nil
}

// oldAddresses counts the old portal's public addresses and where each leads (OldAddresses).
func (r *run) oldAddresses(targets map[string]linkTarget, open bool) OldAddresses {
	result := OldAddresses{Languages: []string{}, Unmapped: []UnmappedAddress{}}
	paths := []string{"/support/home", "/support/solutions"}
	entities := []string{"", ""}
	kb := r.kb
	if kb != nil {
		if kb.settings != nil && len(kb.settings.PortalLanguages) > 0 {
			for _, language := range append([]string{kb.settings.PrimaryLanguage}, kb.settings.PortalLanguages...) {
				if language != "" && !slices.Contains(result.Languages, language) {
					result.Languages = append(result.Languages, language)
				}
			}
		}
		publicCategories := map[int64]bool{}
		var folders []string
		for _, id := range kb.order {
			folder := kb.folders[id]
			if kb.draftReason(kbArticle{fdSolutionArticle: fdSolutionArticle{Status: 2}, folder: folder}, r.opts.HelpCenterPortal) == "" {
				publicCategories[folder.categoryID] = true
				folders = append(folders, strconv.FormatInt(id, 10))
			}
		}
		for _, category := range kb.categories {
			if publicCategories[category.ID] {
				paths, entities = append(paths, "/support/solutions/"+strconv.FormatInt(category.ID, 10)), append(entities, entityKBCategory)
			}
		}
		for _, id := range folders {
			paths, entities = append(paths, "/support/solutions/folders/"+id), append(entities, entityKBFolder)
		}
		for _, article := range kb.articles {
			if kb.draftReason(article, r.opts.HelpCenterPortal) == "" {
				paths, entities = append(paths, "/support/solutions/articles/"+strconv.FormatInt(article.ID, 10)), append(entities, entityKBArticle)
			}
		}
	}
	copies := 1 + len(result.Languages)
	for index, path := range paths {
		result.Found += copies
		reason := ""
		if entity := entities[index]; entity != "" {
			id := path[strings.LastIndex(path, "/")+1:]
			target, found := targets[entity+":"+id]
			switch {
			case !found:
				reason = LinkNotImported
			case target.address == "":
				reason = target.reason
			}
		}
		if reason != "" {
			result.Unmapped = append(result.Unmapped, UnmappedAddress{Path: path, Reason: reason})
		} else if open {
			result.Redirected += copies
		}
	}
	return result
}
