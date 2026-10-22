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

// oldLinkPattern finds what may be a Freshdesk help center address in text: absolute (any host; the
// host is checked after) or a path, in any case, with its query and fragment. Whether it is one is
// decided by oldLink: the path must be the old portal's and a path must be a link's address.
var oldLinkPattern = regexp.MustCompile(`(?i)(?:https?://[a-z0-9.-]+(?::[0-9]+)?)?(?:/[a-z]{2,3}(?:-[a-z0-9]{2,8})?)?/support(?:[/?#][^\s()<>\[\]"'` + "`" + `]*)?`)

// solutionsPath is the knowledge base part of an old portal: on a host this run does not know, only
// these paths are surely Freshdesk's, so only they are listed.
var solutionsPath = regexp.MustCompile(`^(?i:(?:/[a-z]{2,3}(?:-[a-z0-9]{2,8})?)?/support/solutions(?:/|$))`)

// linkSite is the help center links are pointed at.
type linkSite struct {
	address string
	// open: the help center is on and its address is known, so links may point at it.
	open, origin bool
	// hosts are the lower-case hosts (with port when not the default) whose /support/ addresses are
	// this account's: the API host, the account's domain, the old help center's domain when named and
	// the help center's custom domain (an old portal's custom domain often becomes it).
	hosts map[string]bool
}

func (s linkSite) knows(host string) bool {
	host = strings.ToLower(host)
	return s.hosts[host] || s.hosts[strings.TrimSuffix(strings.TrimSuffix(host, ":443"), ":80")]
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
			WHERE i.workspace_id=$1 AND i.source=$2 AND i.entity='kb_article' AND a.deleted_at IS NULL AND strpos(lower(a.body_md), '/support') > 0
			ORDER BY length(i.source_id), i.source_id`},
		{entitySavedReply, `SELECT i.source_id, s.id::text, s.body, s.updated_at=s.created_at
			FROM import_items i JOIN saved_replies s ON s.workspace_id=i.workspace_id AND s.id=i.target_id
			WHERE i.workspace_id=$1 AND i.source=$2 AND i.entity='saved_reply' AND s.deleted_at IS NULL AND strpos(lower(s.body), '/support') > 0
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
	code := codeRanges(item.text)
	for _, match := range oldLinkPattern.FindAllStringIndex(item.text, -1) {
		start, end := match[0], match[1]
		raw := strings.TrimRight(item.text[start:end], ".,;:!?*")
		if code.contains(start) || wordContinues(item.text, start+len(raw)) {
			continue
		}
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
// address (with the old fragment), or reason says why there is none.
func (r *run) resolveLink(text string, start int, raw string, site linkSite, targets map[string]linkTarget) (address, reason string, ours bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", false
	}
	// A path counts only as a link's address: of a Markdown link, "text (url)", or a quoted attribute.
	if u.Host == "" && (start == 0 || !strings.ContainsRune(`("'`, rune(text[start-1]))) {
		return "", "", false
	}
	old, ok := helpcenter.ParseLegacyPath(u.Path)
	switch {
	case !ok:
		return "", "", false
	case u.Host != "" && !site.knows(u.Host):
		if solutionsPath.MatchString(u.Path) {
			return "", LinkUnknownHost, true
		}
		return "", "", false
	case !site.origin:
		return "", LinkNoServerAddress, true
	case !site.open:
		return "", LinkHelpCenterOff, true
	}
	switch old.Kind {
	case helpcenter.LegacyHome:
		address = site.address
	case helpcenter.LegacySearch:
		address = site.address + helpcenter.SearchPath(u.Query().Get("term"))
	case helpcenter.LegacyOther:
		return "", LinkNoNewPage, true
	default:
		target, found := targets[old.Entity()+":"+old.ID]
		if !found {
			return "", LinkNotImported, true
		}
		address, reason = target.address, target.reason
	}
	if address != "" && u.Fragment != "" {
		address += "#" + u.EscapedFragment()
	}
	return address, reason, true
}

// wordContinues reports whether the text at end goes on with the same word, as in /supporters or
// /support-team, or /support.html: then the match is not an old address.
func wordContinues(text string, end int) bool {
	if end >= len(text) {
		return false
	}
	isWord := func(c byte) bool {
		return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_'
	}
	return isWord(text[end]) || text[end] == '.' && end+1 < len(text) && isWord(text[end+1])
}

// textRanges are byte ranges [start, end) of a text.
type textRanges [][2]int

func (t textRanges) contains(offset int) bool {
	for _, r := range t {
		if offset >= r[0] && offset < r[1] {
			return true
		}
	}
	return false
}

// fenceOpen is a Markdown code fence line: up to three spaces, then three or more ` or ~.
var fenceOpen = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")

// codeRanges finds the code of a Markdown text: fenced blocks (to the end when never closed) and
// inline code spans. An address written there is an example of the old address, not a link.
func codeRanges(text string) textRanges {
	var ranges textRanges
	var fence string
	fenceStart, offset := 0, 0
	var prose []int // line starts outside fences, with their ends
	for _, line := range strings.SplitAfter(text, "\n") {
		marker := fenceOpen.FindStringSubmatch(line)
		switch {
		case fence == "" && marker != nil:
			fence, fenceStart = marker[1], offset
		case fence != "" && marker != nil && marker[1][0] == fence[0] && len(marker[1]) >= len(fence) && strings.TrimSpace(line[len(marker[0]):]) == "":
			ranges, fence = append(ranges, [2]int{fenceStart, offset + len(line)}), ""
		case fence == "":
			prose = append(prose, offset, offset+len(line))
		}
		offset += len(line)
	}
	if fence != "" {
		ranges = append(ranges, [2]int{fenceStart, len(text)})
	}
	for index := 0; index < len(prose); index += 2 {
		ranges = append(ranges, codeSpans(text, prose[index], prose[index+1])...)
	}
	return ranges
}

// codeSpans finds inline code in text[from:to]: a run of backticks up to the next run of the same
// length. A run with no partner is literal. A span does not cross lines here, which may miss a rare
// span over two lines; that link is then rewritten as any other.
func codeSpans(text string, from, to int) textRanges {
	var spans textRanges
	for index := from; index < to; {
		if text[index] != '`' {
			index++
			continue
		}
		run := index
		for run < to && text[run] == '`' {
			run++
		}
		width, closed := run-index, false
		for next := run; next < to; {
			if text[next] != '`' {
				next++
				continue
			}
			end := next
			for end < to && text[end] == '`' {
				end++
			}
			if end-next == width {
				spans, index, closed = append(spans, [2]int{index, end}), end, true
				break
			}
			next = end
		}
		if !closed {
			index = run
		}
	}
	return spans
}
