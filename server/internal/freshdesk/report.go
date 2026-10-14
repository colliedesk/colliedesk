package freshdesk

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Report statuses. A partial import is FAILED: every missing item is listed.
const (
	StatusOK     = "OK"
	StatusFailed = "FAILED"
	StatusDryRun = "DRY_RUN"
)

type EntityCount struct {
	Source   int `json:"source"`
	Imported int `json:"imported"`
	Failed   int `json:"failed"`
}

type TicketCount struct {
	EntityCount
	DeletedAtSource []string `json:"deleted_at_source"`
	Trashed         []string `json:"trashed"`
	Spam            []string `json:"spam"`
	AbsentIDs       int      `json:"absent_ids"`
	Archived        int      `json:"archived"`
}

type AttachmentCount struct {
	EntityCount
	SourceBytes        int64    `json:"source_bytes"`
	ImportedBytes      int64    `json:"imported_bytes"`
	ChecksumVerified   int      `json:"checksum_verified"`
	ChecksumMismatches []string `json:"checksum_mismatches"`
}

type CompanyCount struct {
	Source     int      `json:"source"`
	Attached   int      `json:"attached_to_contacts"`
	Unattached []string `json:"unattached"`
}

type AgentCount struct {
	Source    int      `json:"source"`
	Matched   int      `json:"matched"`
	Unmatched []string `json:"unmatched"`
}

type Failure struct {
	Entity      string `json:"entity"`
	FreshdeskID string `json:"freshdesk_id"`
	TicketID    string `json:"ticket_id,omitempty"`
	Error       string `json:"error"`
}

type Warning struct {
	Code         string   `json:"code"`
	Message      string   `json:"message"`
	FreshdeskIDs []string `json:"freshdesk_ids"`
}

// Report is the reconcile report of one import run: source counts versus
// what CollieDesk holds, so a customer can prove nothing was lost.
type Report struct {
	Status      string          `json:"status"`
	Domain      string          `json:"domain"`
	Workspace   string          `json:"workspace"`
	DryRun      bool            `json:"dry_run"`
	Since       *time.Time      `json:"since"`
	StartedAt   time.Time       `json:"started_at"`
	FinishedAt  time.Time       `json:"finished_at"`
	Contacts    EntityCount     `json:"contacts"`
	Companies   CompanyCount    `json:"companies"`
	Agents      AgentCount      `json:"agents"`
	Tickets     TicketCount     `json:"tickets"`
	Messages    EntityCount     `json:"messages"`
	Attachments AttachmentCount `json:"attachments"`
	TimeEntries EntityCount     `json:"time_entries"`
	Missing     []string        `json:"missing"`
	Failures    []Failure       `json:"failures"`
	Warnings    []Warning       `json:"warnings"`
	NotImported []string        `json:"not_imported"`
}

// notImported lists Freshdesk data this importer does not carry over, so the
// report never implies more than it delivers.
var notImported = []string{
	"Custom ticket and contact fields",
	"Contact phone numbers, other emails, addresses and social handles",
	"Companies other than their name (domains, notes, custom fields)",
	"Satisfaction ratings, SLA policies, groups, products, canned responses, automations",
	"Solution (knowledge base) articles",
	"Inline images inside message HTML are kept as links to Freshdesk, not copied (see warnings)",
	"Tickets in Freshdesk Trash or Spam (listed by id in this report)",
	"Time entries of archived tickets (the Freshdesk API documents no endpoint for them)",
}

func (r *Report) JSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// WriteFiles writes <prefix>.json and <prefix>.md into dir.
func (r *Report) WriteFiles(dir string) (string, string, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", "", fmt.Errorf("create report directory: %w", err)
	}
	prefix := filepath.Join(dir, fmt.Sprintf("freshdesk-import-%s-%s", r.Workspace, r.StartedAt.UTC().Format("20060102T150405Z")))
	data, err := r.JSON()
	if err != nil {
		return "", "", fmt.Errorf("encode report: %w", err)
	}
	if err := os.WriteFile(prefix+".json", append(data, '\n'), 0o640); err != nil {
		return "", "", fmt.Errorf("write report: %w", err)
	}
	if err := os.WriteFile(prefix+".md", []byte(r.Markdown()), 0o640); err != nil {
		return "", "", fmt.Errorf("write report: %w", err)
	}
	return prefix + ".json", prefix + ".md", nil
}

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
	b.WriteString("\n## Counts\n\n| Entity | In Freshdesk | In CollieDesk | Failed |\n|---|---:|---:|---:|\n")
	for _, row := range []struct {
		name  string
		count EntityCount
	}{{"Contacts", r.Contacts}, {"Tickets", r.Tickets.EntityCount}, {"Messages", r.Messages}, {"Attachments", r.Attachments.EntityCount}, {"Time entries", r.TimeEntries}} {
		fmt.Fprintf(&b, "| %s | %d | %d | %d |\n", row.name, row.count.Source, row.count.Imported, row.count.Failed)
	}
	fmt.Fprintf(&b, "\nAttachment bytes: %d in Freshdesk, %d stored. SHA-256 re-verified from storage: %d.\n",
		r.Attachments.SourceBytes, r.Attachments.ImportedBytes, r.Attachments.ChecksumVerified)
	fmt.Fprintf(&b, "Companies: %d, %d kept as contact organization. Agents: %d, %d matched to CollieDesk agents by email.\n",
		r.Companies.Source, r.Companies.Attached, r.Agents.Source, r.Agents.Matched)
	fmt.Fprintf(&b, "Archived tickets found by id probing: %d. Ticket ids that do not exist in Freshdesk: %d.\n", r.Tickets.Archived, r.Tickets.AbsentIDs)
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
