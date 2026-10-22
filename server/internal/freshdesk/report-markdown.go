package freshdesk

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

const markdownListLimit = 50

func (r *Report) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Freshdesk import: %s\n\n", r.Status)
	fmt.Fprintf(&b, "- Source: %s\n- Workspace: %s\n- Started: %s\n- Finished: %s\n",
		r.Domain, r.Workspace, r.StartedAt.UTC().Format(time.RFC3339), r.FinishedAt.UTC().Format(time.RFC3339))
	if r.Since != nil {
		fmt.Fprintf(&b, "- Only tickets updated since: %s\n", r.Since.UTC().Format(time.RFC3339))
	}
	if r.Interim {
		b.WriteString("- Interim: recent tickets only, newest first. Contacts, saved replies, the knowledge base, archived and older tickets, and the completeness checks come with the full import that follows; only its report can be OK.\n")
	}
	if r.DryRun {
		b.WriteString("- Dry run: nothing was written. Message, attachment and time entry counts are known only after an import.\n")
	}
	if c := r.StorageCap; c != nil {
		if c.WouldRefuse {
			fmt.Fprintf(&b, "- storage: this import would be refused: it needs %s of attachments and %s is free under the %s cap of the %s plan\n", domain.FormatBytes(c.NeededBytes), domain.FormatBytes(c.FreeBytes), domain.FormatBytes(c.LimitBytes), domain.PlanName(c.Plan))
		} else {
			fmt.Fprintf(&b, "- storage: fits the %s plan cap (needs %s, %s free of %s)\n", domain.PlanName(c.Plan), domain.FormatBytes(c.NeededBytes), domain.FormatBytes(c.FreeBytes), domain.FormatBytes(c.LimitBytes))
		}
	}
	if r.TrialExtendedTo != nil {
		fmt.Fprintf(&b, "- trial: extended to %s\n", r.TrialExtendedTo.UTC().Format(time.RFC3339))
	}
	if r.TrialExtensionError != "" {
		fmt.Fprintf(&b, "- trial: extension failed (%s); run the same import again to retry\n", r.TrialExtensionError)
	}
	b.WriteString("\n## Counts\n\n| Entity | In Freshdesk | In Colliedesk | Failed |\n|---|---:|---:|---:|\n")
	for _, row := range []struct {
		name  string
		count EntityCount
	}{{"Contacts", r.Contacts}, {"Tickets", r.Tickets.EntityCount}, {"Messages", r.Messages}, {"Attachments", r.Attachments.EntityCount}, {"Inline images", r.InlineImages.EntityCount}, {"Time entries", r.TimeEntries}, {"Saved replies", r.SavedReplies.EntityCount},
		{"Knowledge base collections", r.KnowledgeBase.Collections}, {"Knowledge base articles", r.KnowledgeBase.Articles},
		{"Knowledge base translations", r.KnowledgeBase.Translations}, {"Knowledge base images", r.KnowledgeBase.Images.EntityCount}} {
		fmt.Fprintf(&b, "| %s | %d | %d | %d |\n", row.name, row.count.Source, row.count.Imported, row.count.Failed)
	}
	fmt.Fprintf(&b, "\nAttachment bytes: %d in Freshdesk, %d stored. SHA-256 re-verified from storage: %d.\n",
		r.Attachments.SourceBytes, r.Attachments.ImportedBytes, r.Attachments.ChecksumVerified)
	fmt.Fprintf(&b, "Inline image bytes: %d stored. SHA-256 re-verified from storage: %d. Already gone in Freshdesk: %d (counted in the first column, listed below).\n",
		r.InlineImages.ImportedBytes, r.InlineImages.ChecksumVerified, r.InlineImages.Gone)
	fmt.Fprintf(&b, "Companies: %d, %d kept as contact organization. Agents: %d, %d matched to Colliedesk agents by email.\n",
		r.Companies.Source, r.Companies.Attached, r.Agents.Source, r.Agents.Matched)
	absentLabel := "Ticket ids that do not exist in Freshdesk"
	if len(r.Tickets.UnresolvedIDs) > 0 {
		absentLabel = "Ticket ids confirmed absent (none can be confirmed while the archive endpoint is refused)"
	}
	fmt.Fprintf(&b, "Archived tickets found by id probing: %d. %s: %d. Ids probed this run: %d (up to id %d).\n",
		r.Tickets.Archived, absentLabel, r.Tickets.AbsentIDs, r.Tickets.ProbedIDs, r.Tickets.HighestProbedID)
	if n := len(r.Tickets.UnresolvedIDs); n > 0 {
		fmt.Fprintf(&b, "Ticket ids unresolved: %d (listed below). Each may be deleted or archived in Freshdesk.\n", n)
	}
	if r.SavedReplies.NotAvailable {
		b.WriteString("Saved replies: Canned responses are not available on this Freshdesk account, so none were imported. This does not affect the ticket import.\n")
	}
	fmt.Fprintf(&b, "Saved replies: %d canned response folders; %d canned responses had no text and were skipped; %d files on canned responses were not imported.\n",
		r.SavedReplies.Folders, len(r.SavedReplies.EmptySkipped), r.SavedReplies.AttachmentsNotImported)
	writeKnowledgeBase(&b, r.KnowledgeBase)
	fmt.Fprintf(&b, "Rate limiting: %d pause(s) after X-Ratelimit-Remaining 0, %d HTTP 429 response(s), %s waited in total.\n",
		r.RateLimit.Pauses, r.RateLimit.Throttled, (time.Duration(r.RateLimit.WaitSeconds * float64(time.Second))).Round(time.Second))
	b.WriteString("\n## How the totals were verified\n\n")
	for _, row := range []struct {
		name string
		v    Verification
	}{{"Tickets", r.Verification.Tickets}, {"Contacts", r.Verification.Contacts}, {"Saved replies", r.Verification.SavedReplies}, {"Knowledge base", r.Verification.KnowledgeBase}} {
		verdict := "verified"
		if !row.v.Verified {
			verdict = "NOT verified"
		}
		fmt.Fprintf(&b, "- **%s: %s.** %s %s\n", row.name, verdict, row.v.Method, row.v.Detail)
	}
	writeList(&b, "Missing", r.Missing)
	if len(r.Failures) > 0 {
		b.WriteString("\n## Failures\n\n| Entity | Freshdesk id | Ticket | Error |\n|---|---|---|---|\n")
		for index, failure := range r.Failures {
			if index == markdownListLimit {
				fmt.Fprintf(&b, "\n%d more failures are listed in the JSON report.\n", len(r.Failures)-markdownListLimit)
				break
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", failure.Entity, failure.FreshdeskID, failure.TicketID, strings.ReplaceAll(failure.Error, "|", "/"))
		}
	}
	writeList(&b, "Images already gone in Freshdesk (not recoverable)", goneLines(r.InlineImages.GoneImages))
	writeList(&b, "Canned responses with no text, not imported (Freshdesk ids)", r.SavedReplies.EmptySkipped)
	writeList(&b, "Canned responses in personal folders, not imported (Freshdesk ids; run again with --include-personal-canned-responses to share them)", r.SavedReplies.PersonalNotImported)
	writeList(&b, "Canned responses whose files were not imported (Freshdesk ids)", r.SavedReplies.ResponsesWithAttachments)
	writeList(&b, "Freshdesk placeholders left as written in saved replies (canned response ids)", placeholderLines(r.SavedReplies.UnmappedPlaceholders))
	writeList(&b, "Knowledge base limits this import would pass (nothing is imported until they fit)", r.KnowledgeBase.Refused)
	writeList(&b, "Knowledge base articles imported as drafts, by reason (Freshdesk ids)", draftLines(r.KnowledgeBase.Drafts))
	writeList(&b, "Knowledge base articles changed in Freshdesk and in Colliedesk; the Colliedesk version was kept (Freshdesk ids)", r.KnowledgeBase.ChangedInBoth)
	writeList(&b, "Knowledge base articles made drafts again: Freshdesk no longer shows them to everyone (Freshdesk ids)", r.KnowledgeBase.Unpublished)
	writeList(&b, "Knowledge base articles deleted for good in Colliedesk, not imported again (Freshdesk ids)", r.KnowledgeBase.DeletedInColliedesk)
	writeList(&b, "Knowledge base images already gone in Freshdesk (the article keeps the link)", kbGoneLines(r.KnowledgeBase.Images.GoneImages))
	writeList(&b, "Ticket ids that could not be resolved (archive unavailable)", r.Tickets.UnresolvedIDs)
	writeList(&b, "Deleted in Freshdesk during the import (ticket ids)", r.Tickets.DeletedAtSource)
	writeList(&b, "In Freshdesk Trash, not imported (ticket ids)", r.Tickets.Trashed)
	writeList(&b, "In Freshdesk Spam, not imported (ticket ids)", r.Tickets.Spam)
	if len(r.Warnings) > 0 {
		b.WriteString("\n## Warnings\n\n")
		for _, warning := range r.Warnings {
			fmt.Fprintf(&b, "- **%s**: %s%s\n", warning.Code, warning.Message, idSuffix(warning.FreshdeskIDs))
		}
	}
	writeList(&b, "Not imported by design", r.NotImported)
	return b.String()
}

func writeList(b *strings.Builder, title string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "\n## %s\n\n", title)
	for index, item := range items {
		if index == markdownListLimit {
			fmt.Fprintf(b, "- … %d more in the JSON report\n", len(items)-markdownListLimit)
			return
		}
		fmt.Fprintf(b, "- %s\n", item)
	}
}

func idSuffix(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	if len(ids) > 20 {
		return fmt.Sprintf(" (ids: %s, … %d more in JSON)", strings.Join(ids[:20], ", "), len(ids)-20)
	}
	return " (ids: " + strings.Join(ids, ", ") + ")"
}

func placeholderLines(placeholders map[string][]string) []string {
	names := make([]string, 0, len(placeholders))
	for name := range placeholders {
		names = append(names, name)
	}
	sort.Strings(names)
	lines := make([]string, 0, len(names))
	for _, name := range names {
		lines = append(lines, "{{"+name+"}}"+idSuffix(placeholders[name]))
	}
	return lines
}

func goneLines(images []GoneImage) []string {
	lines := make([]string, 0, len(images))
	for _, image := range images {
		lines = append(lines, fmt.Sprintf("ticket %s: %s. The message keeps its Freshdesk link, which no longer works. %s", image.TicketID, image.Message, image.Detail))
	}
	return lines
}

// draftReasons explains each reason an article published in Freshdesk became a draft.
var draftReasons = map[string]string{
	draftInFreshdesk:    "a draft in Freshdesk",
	draftFolderVisible:  "in a folder not visible to all users (logged-in users, agents, companies, segments or bots only)",
	draftNotInPortal:    "its category is in no portal",
	draftPortalUnknown:  "its category names no portal while others do, so its portal is unknown",
	draftSeveralPortals: "its category is not in the help center's portal (choose one with --help-center-portal)",
}

func writeKnowledgeBase(b *strings.Builder, kb KnowledgeBaseCount) {
	if kb.NotAvailable {
		b.WriteString("Knowledge base: this Freshdesk account has none, so nothing was imported. This does not affect the ticket import.\n")
		return
	}
	drafts := 0
	for _, ids := range kb.Drafts {
		drafts += len(ids)
	}
	languages := "none"
	if len(kb.Languages) > 0 {
		languages = strings.Join(kb.Languages, ", ")
	}
	fmt.Fprintf(b, "Knowledge base: %d published, %d imported as drafts (reasons below; publish them in Colliedesk when they should be public). Translation languages: %s. Images already gone in Freshdesk: %d. The whole knowledge base is read on every run; --since applies to tickets only.\n",
		len(kb.Published), drafts, languages, kb.Images.Gone)
	for _, note := range kb.Notes {
		fmt.Fprintf(b, "Knowledge base: %s\n", note)
	}
}

func draftLines(drafts map[string][]string) []string {
	reasons := make([]string, 0, len(drafts))
	for reason := range drafts {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	lines := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		lines = append(lines, fmt.Sprintf("%s (%s): %d%s", reason, draftReasons[reason], len(drafts[reason]), idSuffix(drafts[reason])))
	}
	return lines
}

func kbGoneLines(images []KBGoneImage) []string {
	lines := make([]string, 0, len(images))
	for _, image := range images {
		lines = append(lines, fmt.Sprintf("article %s: %s", image.ArticleID, image.Detail))
	}
	return lines
}
