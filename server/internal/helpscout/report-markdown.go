package helpscout

import (
	"fmt"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

const markdownListLimit = 50

// Markdown renders the report for a person; the JSON file holds the same data in full.
func (r *Report) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Help Scout import: %s\n\n", r.Status)
	fmt.Fprintf(&b, "- Source: inbox %d (%s), ledger source `%s`\n- Workspace: %s\n- Started: %s\n- Finished: %s\n",
		r.Mailbox.ID, r.Mailbox.Name, r.Source, r.Workspace, r.StartedAt.UTC().Format(time.RFC3339), r.FinishedAt.UTC().Format(time.RFC3339))
	if r.Since != nil {
		fmt.Fprintf(&b, "- Only conversations modified since: %s\n", r.Since.UTC().Format(time.RFC3339))
	}
	if r.DryRun {
		b.WriteString("- Dry run: nothing was written. Message, attachment, image and tag counts are known only after an import.\n")
	}
	if c := r.StorageCap; c != nil {
		verdict := "fits"
		if c.WouldRefuse {
			verdict = "would be refused by"
		}
		fmt.Fprintf(&b, "- storage: this import %s the %s cap of the %s plan (needs %s, %s free)\n", verdict, domain.FormatBytes(c.LimitBytes), domain.PlanName(c.Plan), domain.FormatBytes(c.NeededBytes), domain.FormatBytes(c.FreeBytes))
	}
	if r.TrialExtendedTo != nil {
		fmt.Fprintf(&b, "- trial: extended to %s\n", r.TrialExtendedTo.UTC().Format(time.RFC3339))
	}
	if r.TrialExtensionError != "" {
		fmt.Fprintf(&b, "- trial: extension failed (%s); run the same import again to retry\n", r.TrialExtensionError)
	}
	b.WriteString("\n## Counts\n\n| Entity | In Help Scout | In Colliedesk | Failed |\n|---|---:|---:|---:|\n")
	for _, row := range []struct {
		name  string
		count EntityCount
	}{{"Conversations", r.Conversations.EntityCount}, {"Contacts", r.Contacts}, {"Messages", r.Messages.EntityCount}, {"Attachments", r.Attachments.EntityCount},
		{"Inline images", r.InlineImages.EntityCount}, {"Tags on conversations", r.Tags}} {
		fmt.Fprintf(&b, "| %s | %d | %d | %d |\n", row.name, row.count.Source, row.count.Imported, row.count.Failed)
	}
	fmt.Fprintf(&b, "\nAttachment bytes stored: %d. SHA-256 re-verified from storage: %d attachments, %d inline images (%d bytes). Images already gone in Help Scout: %d.\n",
		r.Attachments.ImportedBytes, r.Attachments.ChecksumVerified, r.InlineImages.ChecksumVerified, r.InlineImages.ImportedBytes, r.InlineImages.Gone)
	fmt.Fprintf(&b, "Users: %d, %d matched to Colliedesk agents by email. Line items not imported: %d.\n", r.Agents.Source, r.Agents.Matched, r.Messages.LineItems)
	fmt.Fprintf(&b, "Rate limiting: %d pause(s) after X-RateLimit-Remaining-Minute 0, %d HTTP 429 response(s), %s waited in total. Access token requested again: %d time(s).\n",
		r.RateLimit.Pauses, r.RateLimit.Throttled, (time.Duration(r.RateLimit.WaitSeconds * float64(time.Second))).Round(time.Second), r.RateLimit.TokenRefreshes)
	verdict := "verified"
	if !r.Verification.Conversations.Verified {
		verdict = "NOT verified"
	}
	fmt.Fprintf(&b, "\n## How the totals were verified\n\n- **Conversations: %s.** %s %s\n", verdict, r.Verification.Conversations.Method, r.Verification.Conversations.Detail)
	writeList(&b, "Missing", r.Missing)
	if len(r.Failures) > 0 {
		b.WriteString("\n## Failures\n\n| Entity | Help Scout id | Conversation | Error |\n|---|---|---|---|\n")
		for i, f := range r.Failures {
			if i == markdownListLimit {
				fmt.Fprintf(&b, "\n… %d more in the JSON report.\n", len(r.Failures)-markdownListLimit)
				break
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", f.Entity, f.HelpScoutID, f.ConversationID, strings.ReplaceAll(f.Error, "|", "/"))
		}
	}
	gone := make([]string, 0, len(r.InlineImages.GoneImages))
	for _, image := range r.InlineImages.GoneImages {
		gone = append(gone, fmt.Sprintf("conversation %s, thread %s: %s", image.ConversationID, image.ThreadID, image.Detail))
	}
	writeList(&b, "Images already gone in Help Scout (not recoverable)", gone)
	writeList(&b, "Spam conversations, not imported", r.Conversations.Spam)
	writeList(&b, "Deleted conversations, not imported", r.Conversations.DeletedAtSource)
	writeList(&b, "Draft conversations, not imported", r.Conversations.Drafts)
	writeList(&b, "Draft threads that were never sent, not imported", r.Messages.DraftThreads)
	writeList(&b, "Attachments marked as a virus by Help Scout, not imported", r.Attachments.Virus)
	writeList(&b, "Users without a Colliedesk agent", r.Agents.Unmatched)
	for _, w := range r.Warnings {
		writeList(&b, "Warning "+w.Code+": "+w.Message, w.HelpScoutIDs)
	}
	writeList(&b, "Not imported", r.NotImported)
	return b.String()
}

func writeList(b *strings.Builder, title string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "\n## %s\n\n", title)
	for i, item := range items {
		if i == markdownListLimit {
			fmt.Fprintf(b, "- … %d more in the JSON report\n", len(items)-markdownListLimit)
			return
		}
		fmt.Fprintf(b, "- %s\n", item)
	}
}
