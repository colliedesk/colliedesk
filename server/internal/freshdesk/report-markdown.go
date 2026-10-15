package freshdesk

import (
	"fmt"
	"strings"
	"time"
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
	if r.DryRun {
		b.WriteString("- Dry run: nothing was written. Message, attachment and time entry counts are known only after an import.\n")
	}
	b.WriteString("\n## Counts\n\n| Entity | In Freshdesk | In Colliedesk | Failed |\n|---|---:|---:|---:|\n")
	for _, row := range []struct {
		name  string
		count EntityCount
	}{{"Contacts", r.Contacts}, {"Tickets", r.Tickets.EntityCount}, {"Messages", r.Messages}, {"Attachments", r.Attachments.EntityCount}, {"Time entries", r.TimeEntries}} {
		fmt.Fprintf(&b, "| %s | %d | %d | %d |\n", row.name, row.count.Source, row.count.Imported, row.count.Failed)
	}
	fmt.Fprintf(&b, "\nAttachment bytes: %d in Freshdesk, %d stored. SHA-256 re-verified from storage: %d.\n",
		r.Attachments.SourceBytes, r.Attachments.ImportedBytes, r.Attachments.ChecksumVerified)
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
	fmt.Fprintf(&b, "Rate limiting: %d pause(s) after X-Ratelimit-Remaining 0, %d HTTP 429 response(s), %s waited in total.\n",
		r.RateLimit.Pauses, r.RateLimit.Throttled, (time.Duration(r.RateLimit.WaitSeconds * float64(time.Second))).Round(time.Second))
	b.WriteString("\n## How the totals were verified\n\n")
	for _, row := range []struct {
		name string
		v    Verification
	}{{"Tickets", r.Verification.Tickets}, {"Contacts", r.Verification.Contacts}} {
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
