package helpcenter

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// A team on <name>.freshdesk.com with no custom domain cannot answer 301 from the old addresses. It
// pastes this snippet into its Freshdesk portal's head instead: a script that sends a visitor of an old
// help page to the new one. It needs the portal customization of Freshdesk's Pro and Enterprise plans,
// and works as long as the Freshdesk account does. docs/internals/help-center.md, "Old Freshdesk
// addresses".

// maxSnippetMapBytes bounds the id map embedded in the snippet. A larger knowledge base is not mapped:
// the snippet then sends every old address, by its id, to the help center, which holds the whole mapping.
const maxSnippetMapBytes = 32 * 1024

// LegacyRedirects are the pages an import made public, by Freshdesk id: the path below the help center's
// address of each article, folder and category (a collection path).
type LegacyRedirects struct {
	Articles, Folders, Categories map[string]string
}

// errHelpCenterClosed means the workspace has no open help center to send visitors to.
var errHelpCenterClosed = errors.New("the help center is off")

// IsClosed reports whether err is the closed help center of LegacyRedirects.
func IsClosed(err error) bool { return errors.Is(err, errHelpCenterClosed) }

// LegacyRedirects finds the public pages the imported Freshdesk items of the ledger source became. An
// item that is a draft, in the trash, or a collection with nothing published is left out: its old address
// goes to the help center's search.
func (s *Site) LegacyRedirects(ctx context.Context, workspaceID, accountSource string) (LegacyRedirects, error) {
	site, open, err := s.config.Store.OpenHelpCenterWorkspace(ctx, workspaceID)
	if err != nil {
		return LegacyRedirects{}, err
	}
	if !open {
		return LegacyRedirects{}, errHelpCenterClosed
	}
	items, err := s.config.Store.HelpCenterLegacyItems(ctx, workspaceID, accountSource)
	if err != nil {
		return LegacyRedirects{}, err
	}
	redirects := LegacyRedirects{Articles: map[string]string{}, Folders: map[string]string{}, Categories: map[string]string{}}
	var t tree
	for _, item := range items {
		switch {
		case item.Number > 0:
			redirects.Articles[item.SourceID] = ArticlePath(item.Number, item.Slug)
		case item.CollectionID != "":
			if t.byID == nil {
				if t, err = s.loadTree(ctx, s.newView(site, "")); err != nil {
					return LegacyRedirects{}, err
				}
			}
			n := t.byID[item.CollectionID]
			if n == nil || n.total == 0 {
				continue
			}
			if item.Entity == "kb_folder" {
				redirects.Folders[item.SourceID] = n.path()
			} else {
				redirects.Categories[item.SourceID] = n.path()
			}
		}
	}
	return redirects, nil
}

// FreshdeskHeadSnippet is the HTML to paste into the head of a Freshdesk portal: a script that sends a
// visitor of an old help center page to its new address on address (the help center's SiteURL). The
// target always starts with address and a slash, and the rest is a path of the imported mapping, the
// digits of an id, or a fixed path, so no old address can send a visitor anywhere else. Only the
// help pages (home, solutions, search) move; sign-in and tickets stay. mapped is false when the
// knowledge base was too large to embed.
//
// There is no meta refresh: it cannot tell a help page from the sign-in page, and would send every
// visitor of the portal away.
func FreshdeskHeadSnippet(address string, redirects LegacyRedirects) (snippet string, mapped bool) {
	ids := map[string]map[string]string{"a": redirects.Articles, "f": redirects.Folders, "c": redirects.Categories}
	for kind, paths := range ids {
		if paths == nil {
			ids[kind] = map[string]string{}
		}
	}
	embedded, _ := json.Marshal(ids)
	mapped = len(embedded) <= maxSnippetMapBytes
	if !mapped {
		embedded = []byte(`{"a":{},"f":{},"c":{}}`)
	}
	target, _ := json.Marshal(address)
	return strings.NewReplacer("__TARGET__", string(target), "__IDS__", string(embedded)).Replace(redirectSnippet), mapped
}

const redirectSnippet = `<script>
(function () {
  var target = __TARGET__, ids = __IDS__;
  var path = location.pathname.replace(/^\/[A-Za-z]{2,3}(?:-[A-Za-z0-9]{2,8})?(?=\/support(?:\/|$))/, "");
  var to = null, m;
  if ((m = /^\/support\/solutions\/(articles|folders)\/([1-9][0-9]{0,18})(?:-[^\/]*)?\/?$/.exec(path))) {
    to = ids[m[1] === "articles" ? "a" : "f"][m[2]] || "/support/solutions/" + m[1] + "/" + m[2];
  } else if ((m = /^\/support\/solutions\/([1-9][0-9]{0,18})(?:-[^\/]*)?\/?$/.exec(path))) {
    to = ids.c[m[1]] || "/support/solutions/" + m[1];
  } else if (/^\/support\/search\/?$/.test(path)) {
    to = "/support/search" + location.search;
  } else if (/^\/support(?:\/(?:home|solutions))?\/?$/.test(path)) {
    to = "/";
  }
  if (to !== null) location.replace(target + to);
})();
</script>
`
