package freshdesk

import (
	"fmt"
	"strings"
)

// LinksReport is the report's section on the old Freshdesk help center: the addresses visitors and
// search engines know, where each leads now, and the Freshdesk links inside imported articles and
// saved replies. Its status is its own (OK or UNVERIFIED); links are not data, so it never changes
// the import's status.
type LinksReport struct {
	Status string `json:"status"`
	// Detail says why the section is UNVERIFIED, one sentence each.
	Detail []string `json:"detail"`
	// HelpCenter is the help center's address the old addresses and links lead to.
	HelpCenter   string       `json:"help_center"`
	OldAddresses OldAddresses `json:"old_addresses"`
	Rewritten    LinkRewrites `json:"rewritten"`
	Left         []LeftLink   `json:"left"`
}

// OldAddresses counts the old portal's public addresses: its home and solutions pages, every category
// and folder every visitor could see and every public article, each also below the language prefix of
// each portal language (Languages). Redirected counts those that lead to a public page now (none while
// the help center is off); Unmapped lists, once per path without a language, those that do not: they
// go to the help center search.
type OldAddresses struct {
	Found      int               `json:"found"`
	Redirected int               `json:"redirected"`
	Languages  []string          `json:"languages"`
	Unmapped   []UnmappedAddress `json:"unmapped"`
}

// UnmappedAddress is an old address that leads to no public page, and why (a Link* reason).
type UnmappedAddress struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// LinkRewrites counts what this run pointed at the new help center: articles, translations and saved
// replies changed, and links in them. A rerun finds them already pointing there and counts nothing.
type LinkRewrites struct {
	Articles     int `json:"articles"`
	Translations int `json:"translations"`
	SavedReplies int `json:"saved_replies"`
	Links        int `json:"links"`
}

// LeftLink is a Freshdesk help center link left as written in an imported article (entity
// kb_article), translation (kb_translation, Freshdesk id "<article id>:<language>") or saved reply
// (saved_reply), by the item's Freshdesk id.
type LeftLink struct {
	Entity      string `json:"entity"`
	FreshdeskID string `json:"freshdesk_id"`
	URL         string `json:"url"`
	Reason      string `json:"reason"`
}

// Why an old address or link does not lead to a page.
const (
	// LinkNotImported: no article, folder or category of this account is in Colliedesk under that id
	// (never imported, failed, deleted for good, or the address names no item).
	LinkNotImported = "not_imported"
	// LinkNotPublic: it is a draft, in the trash, or a collection with nothing published in it.
	LinkNotPublic = "not_public"
	// LinkChangedInColliedesk: the article or saved reply holding the link was changed in Colliedesk
	// after the import, so the import leaves its text alone.
	LinkChangedInColliedesk = "changed_in_colliedesk"
	// LinkHelpCenterOff: the help center is off, so no link is rewritten.
	LinkHelpCenterOff = "help_center_off"
	// LinkNoServerAddress: the run did not know the server's address (APP_ORIGIN), so no link is rewritten.
	LinkNoServerAddress = "no_server_address"
	// LinkNoNewPage: a page of the old portal the help center has no counterpart for (sign-in,
	// tickets, the contact form).
	LinkNoNewPage = "no_new_page"
	// LinkUnknownHost: an old help center path (/support/solutions/...) on a host this run does not
	// know as the account's: the old portal's own custom domain, unless it was named
	// (Options.OldHelpCenterDomain) or claimed for the help center.
	LinkUnknownHost = "unknown_host"
	// LinkWriteFailed: the new text could not be written (for example it would pass the length limit).
	LinkWriteFailed = "write_failed"
)

var linkReasons = map[string]string{
	LinkNotImported:         "nothing of this account is in Colliedesk under that id",
	LinkNotPublic:           "a draft, in the trash, or a collection with nothing published",
	LinkChangedInColliedesk: "changed in Colliedesk after the import, so left as it is",
	LinkHelpCenterOff:       "the help center is off",
	LinkNoServerAddress:     "the server's address (APP_ORIGIN) was not set for this run",
	LinkWriteFailed:         "the new text could not be saved",
	LinkNoNewPage:           "a page of the old portal the help center has no counterpart for",
	LinkUnknownHost:         "a host this import does not know as the old help center's",
}

// finish sets the status and its reasons. open is whether the help center serves the addresses.
func (l *LinksReport) finish(open bool, origin bool) {
	l.Status, l.Detail = StatusOK, []string{}
	switch {
	case !origin:
		l.Detail = append(l.Detail, "The server's address (APP_ORIGIN) was not set for this run, so no link was rewritten: set APP_ORIGIN as the server has it, then run the import again.")
	case !open:
		l.Detail = append(l.Detail, "The help center is off, so no old address redirects and no link was rewritten: turn the help center on, then run the import again.")
	}
	if n := len(l.OldAddresses.Unmapped); n > 0 {
		l.Detail = append(l.Detail, fmt.Sprintf("%d old address(es) lead to no public page; visitors get the help center search with the old address's words.", n))
	}
	if n := len(l.Left); n > 0 {
		l.Detail = append(l.Detail, fmt.Sprintf("%d Freshdesk link(s) were left as written; they stop working when the Freshdesk account closes.", n))
	}
	unknown := 0
	for _, link := range l.Left {
		if link.Reason == LinkUnknownHost {
			unknown++
		}
	}
	if unknown > 0 {
		l.Detail = append(l.Detail, fmt.Sprintf("%d of them point at /support/solutions/ on another host: if that host was your old help center, give it as the old help center address and run the import again.", unknown))
	}
	if len(l.Detail) > 0 {
		l.Status = StatusUnverified
	}
}

func writeLinks(b *strings.Builder, l *LinksReport) {
	if l == nil {
		return
	}
	fmt.Fprintf(b, "\n## Old help center links: %s\n\n", l.Status)
	if l.HelpCenter != "" {
		fmt.Fprintf(b, "Help center: %s\n", l.HelpCenter)
	}
	languages := ""
	if len(l.OldAddresses.Languages) > 0 {
		languages = ", each also below /" + strings.Join(l.OldAddresses.Languages, "/, /") + "/"
	}
	fmt.Fprintf(b, "%d old addresses of the Freshdesk help center%s: %d redirect to a public page.\n", l.OldAddresses.Found, languages, l.OldAddresses.Redirected)
	fmt.Fprintf(b, "Links pointed at the help center by this run: %d, in %d article(s), %d translation(s) and %d saved reply(ies). Left as written: %d.\n",
		l.Rewritten.Links, l.Rewritten.Articles, l.Rewritten.Translations, l.Rewritten.SavedReplies, len(l.Left))
	for _, detail := range l.Detail {
		fmt.Fprintf(b, "- %s\n", detail)
	}
	unmapped := make([]string, 0, len(l.OldAddresses.Unmapped))
	for _, address := range l.OldAddresses.Unmapped {
		unmapped = append(unmapped, fmt.Sprintf("%s: %s", address.Path, linkReasons[address.Reason]))
	}
	writeList(b, "Old help center addresses that lead to no public page (they go to the search)", unmapped)
	left := make([]string, 0, len(l.Left))
	for _, link := range l.Left {
		left = append(left, fmt.Sprintf("%s %s: %s (%s)", link.Entity, link.FreshdeskID, link.URL, linkReasons[link.Reason]))
	}
	writeList(b, "Freshdesk links left as written", left)
}
