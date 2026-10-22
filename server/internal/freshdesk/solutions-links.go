package freshdesk

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"regexp"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/helpcenter"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

// Old Freshdesk help center links in imported articles and saved replies are pointed at the new help
// center (docs/internals/freshdesk-import.md, "Old help center links"). The help center's own
// redirect (helpcenter/legacy.go) reads the same addresses with the same parser.

// oldLinkPattern finds a Freshdesk help center address in text: absolute (any host; the host is
// checked after) or a path that a Markdown link or "text (url)" holds. The query and fragment come with it.
var oldLinkPattern = regexp.MustCompile(`(?:(?i:https?)://[A-Za-z0-9.-]+(?::[0-9]+)?)?(?:/[a-z]{2,3}(?:-[A-Za-z0-9]{2,8})?)?/support/(?:solutions|home)[^\s()<>\[\]"'` + "`" + `]*`)

// linkSite is the help center links are pointed at.
type linkSite struct {
	address string
	// open: the help center is on and its address is known, so links may point at it.
	open, origin bool
	// hosts are the lower-case hosts (with port) whose /support/ addresses are this account's: the
	// API host, the account's domain and the help center's custom domain (an old portal's custom domain
	// often becomes it).
	hosts map[string]bool
}

// linkTarget is where a Freshdesk item's old address leads: an address, or the reason it leads nowhere.
type linkTarget struct{ address, reason string }

// rewriteFreshdeskLinks points the old help center links of the imported articles and saved replies
// that nobody changed in Colliedesk at their new addresses, and builds the links report. It runs after
// every article and saved reply is written, so a link to an item imported later is found too.
func (r *run) rewriteFreshdeskLinks(ctx context.Context) error {
	site, err := r.linkSite(ctx)
	if err != nil {
		return err
	}
	targets, err := r.linkTargets(ctx, site.address)
	if err != nil {
		return err
	}
	report := &LinksReport{HelpCenter: site.address, OldAddresses: r.oldAddresses(targets, site.open), Left: []LeftLink{}}
	found := false
	for _, item := range []struct{ entity, sql string }{
		{entityKBArticle, `SELECT i.source_id, a.id::text, a.body_md, COALESCE((SELECT v.kind='import' FROM kb_article_versions v
				WHERE v.workspace_id=a.workspace_id AND v.article_id=a.id ORDER BY v.version DESC LIMIT 1), false)
			FROM import_items i JOIN kb_articles a ON a.workspace_id=i.workspace_id AND a.id=i.target_id
			WHERE i.workspace_id=$1 AND i.source=$2 AND i.entity='kb_article' AND a.deleted_at IS NULL AND strpos(a.body_md, '/support/') > 0
			ORDER BY length(i.source_id), i.source_id`},
		{entitySavedReply, `SELECT i.source_id, s.id::text, s.body, s.updated_at=s.created_at
			FROM import_items i JOIN saved_replies s ON s.workspace_id=i.workspace_id AND s.id=i.target_id
			WHERE i.workspace_id=$1 AND i.source=$2 AND i.entity='saved_reply' AND s.deleted_at IS NULL AND strpos(s.body, '/support/') > 0
			ORDER BY length(i.source_id), i.source_id`},
	} {
		items, err := r.linkedItems(ctx, item.sql)
		if err != nil {
			return err
		}
		for _, linked := range items {
			links, err := r.rewriteItem(ctx, item.entity, linked, site, targets, report)
			if err != nil {
				return err
			}
			found = found || links > 0
		}
	}
	report.finish(site.open, site.origin)
	if found || r.kb != nil && len(r.kb.categories) > 0 {
		r.links = report
	}
	return nil
}

// linkedItem is an imported article or saved reply whose text may hold old links; unchanged is true
// while nobody changed it in Colliedesk since the import.
type linkedItem struct {
	sourceID, id, text string
	unchanged          bool
}

func (r *run) linkedItems(ctx context.Context, sql string) ([]linkedItem, error) {
	var items []linkedItem
	err := r.db.InWorkspace(ctx, r.workspaceID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, sql, r.workspaceID, r.ledger.source)
		if err != nil {
			return err
		}
		items, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (linkedItem, error) {
			var item linkedItem
			return item, row.Scan(&item.sourceID, &item.id, &item.text, &item.unchanged)
		})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("read imported links: %w", err)
	}
	return items, nil
}

// rewriteItem rewrites the old links of one item it may change and records the others. It returns
// how many old links of this account the item holds.
func (r *run) rewriteItem(ctx context.Context, entity string, item linkedItem, site linkSite, targets map[string]linkTarget, report *LinksReport) (int, error) {
	var text strings.Builder
	var left []LeftLink
	links, rewritten, last := 0, 0, 0
	for _, match := range oldLinkPattern.FindAllStringIndex(item.text, -1) {
		start, end := match[0], match[1]
		raw := strings.TrimRight(item.text[start:end], ".,;:!?*")
		end = start + len(raw)
		address, reason, ours := r.resolveLink(item.text, start, raw, site, targets)
		if !ours {
			continue
		}
		links++
		if !item.unchanged {
			address, reason = "", LinkChangedInColliedesk
		}
		if address == "" {
			left = append(left, LeftLink{Entity: entity, FreshdeskID: item.sourceID, URL: raw, Reason: reason})
			continue
		}
		text.WriteString(item.text[last:start])
		text.WriteString(address)
		last, rewritten = end, rewritten+1
	}
	if rewritten > 0 {
		text.WriteString(item.text[last:])
		written, err := r.writeLinks(ctx, entity, item, text.String())
		switch {
		case err != nil:
			log.Printf("freshdesk import: rewrite links of %s %s: %v", entity, item.sourceID, err)
			left = append(left, LeftLink{Entity: entity, FreshdeskID: item.sourceID, URL: "(all links of this item)", Reason: LinkWriteFailed})
		case !written:
			// Changed in Colliedesk between the read and the write.
			left = append(left, LeftLink{Entity: entity, FreshdeskID: item.sourceID, URL: "(all links of this item)", Reason: LinkChangedInColliedesk})
		case entity == entityKBArticle:
			report.Rewritten.Articles++
			report.Rewritten.Links += rewritten
		default:
			report.Rewritten.SavedReplies++
			report.Rewritten.Links += rewritten
		}
	}
	report.Left = append(report.Left, left...)
	return links, nil
}

func (r *run) writeLinks(ctx context.Context, entity string, item linkedItem, text string) (bool, error) {
	var written bool
	err := r.db.InWorkspace(ctx, r.workspaceID, func(tx pgx.Tx) (err error) {
		if entity == entityKBArticle {
			written, err = store.RewriteImportedKBArticleBody(ctx, tx, r.workspaceID, item.id, item.text, text)
		} else {
			written, err = store.RewriteImportedSavedReplyBody(ctx, tx, r.workspaceID, item.id, item.text, text)
		}
		return err
	})
	return written, err
}

// resolveLink reads raw, found at start of text, as an old help center address of this account. ours
// is false for any other link, which is neither rewritten nor listed. Otherwise address is the new
// address, or reason says why there is none.
func (r *run) resolveLink(text string, start int, raw string, site linkSite, targets map[string]linkTarget) (address, reason string, ours bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", false
	}
	// A path counts only as the address of a Markdown link or of "text (url)".
	if u.Host == "" && (start == 0 || text[start-1] != '(') || u.Host != "" && !site.hosts[strings.ToLower(u.Host)] {
		return "", "", false
	}
	old, ok := helpcenter.ParseLegacyPath(u.Path)
	switch {
	case !ok:
		return "", "", false
	case !site.open:
		return "", LinkHelpCenterOff, true
	case old.Kind == helpcenter.LegacyHome:
		return site.address, "", true
	}
	target, found := targets[old.Entity()+":"+old.ID]
	if old.Entity() == "" || !found {
		return "", LinkNotImported, true
	}
	return target.address, target.reason, true
}
