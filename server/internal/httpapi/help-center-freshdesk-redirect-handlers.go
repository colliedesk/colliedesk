package httpapi

import (
	"net/http"

	"github.com/colliedesk/colliedesk/server/internal/helpcenter"
	"github.com/colliedesk/colliedesk/server/internal/importjobs"
)

// freshdeskKeepWeeks is how long after the switch the team should keep its Freshdesk account and the
// snippet: Freshdesk suspends a cancelled account 24 hours later and deletes it after 14 days.
const freshdeskKeepWeeks = 4

// getFreshdeskRedirect answers what a team that moved from <name>.freshdesk.com should do about its old
// addresses, and, when it can paste code into its Freshdesk portal (the Pro and Enterprise plans, asked
// with ?plan=), the snippet that redirects them. The snippet is built from the help center's own address
// and the pages the import made public; nothing is read from the request.
func (a *API) getFreshdeskRedirect(response http.ResponseWriter, request *http.Request) {
	plan := request.URL.Query().Get("plan")
	switch plan {
	case "", "growth", "pro", "enterprise":
	default:
		writeError(response, http.StatusBadRequest, "invalid_request", "plan is growth, pro or enterprise: the Freshdesk plan of the account.")
		return
	}
	who := principal(request)
	ctx := request.Context()
	address, ok := a.helpCenterHome(response, request)
	if !ok {
		return
	}
	view := map[string]any{"state": "no_import", "freshdesk_domain": nil, "address": address, "keep_weeks": freshdeskKeepWeeks, "snippet": nil, "mapped": nil}
	jobs, err := a.imports.List(ctx, who.Workspace.ID, 100)
	if err != nil {
		writeImportError(response, err)
		return
	}
	var job *importjobs.Job
	for index := range jobs {
		if jobs[index].Kind == importjobs.KindImport && jobs[index].State == importjobs.StateFinished {
			job = &jobs[index]
			break
		}
	}
	if job == nil {
		writeJSON(response, http.StatusOK, view)
		return
	}
	view["freshdesk_domain"] = job.Domain
	center, err := a.store.GetHelpCenter(ctx, who.Workspace.ID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	switch {
	case job.Options.OldHelpCenterDomain != "":
		view["state"] = "own_domain"
	case !center.Enabled:
		view["state"] = "help_center_off"
	case plan == "":
		view["state"] = "ask_plan"
	case plan == "growth":
		view["state"] = "growth"
	default:
		redirects := helpcenter.LegacyRedirects{}
		if job.AccountSource != nil {
			if redirects, err = a.helpCenter.LegacyRedirects(ctx, who.Workspace.ID, *job.AccountSource); err != nil {
				if helpcenter.IsClosed(err) {
					view["state"] = "help_center_off"
					break
				}
				writeInternalError(response, err)
				return
			}
		}
		snippet, embedded := helpcenter.FreshdeskHeadSnippet(address, redirects)
		view["state"], view["snippet"] = "ready", snippet
		view["mapped"] = map[string]any{"articles": len(redirects.Articles), "folders": len(redirects.Folders), "categories": len(redirects.Categories), "embedded": embedded}
	}
	writeJSON(response, http.StatusOK, view)
}
