package results

import (
	"strings"
	"testing"
	"time"
)

func unverifiedPage() Page {
	window := &Numbers{FirstResponse: FirstResponse{MedianSeconds: ptr[int64](600), Answered: 9}, Volume: Volume{Count: 9}}
	return Page{
		Current:       &Snapshot{ID: "current", Data: Data{Windows: map[string]*Numbers{"30d": window, "7d": window}}},
		Baseline:      &Snapshot{ID: "baseline", Data: Data{Source: "freshdesk:account:7", ImportStatus: "UNVERIFIED", Windows: map[string]*Numbers{"30d": window, "7d": window}}},
		BaselineState: "ready", HoursSaved: map[string]*HoursSaved{},
	}
}

// Numbers from an unverified import are labelled wherever they leave the page: the share link and the
// weekly email. A failed import's are not shown at all.
func TestAnUnverifiedBaselineIsLabelledWhereverItIsShown(t *testing.T) {
	shared := SharedFrom(unverifiedPage(), "Acme")
	if shared.BaselineImportStatus == nil || *shared.BaselineImportStatus != "UNVERIFIED" || shared.Baseline == nil {
		t.Fatalf("shared = %+v", shared)
	}
	_, body := WeeklyText("Acme", unverifiedPage(), "UTC", "https://desk.example", time.Date(2026, 10, 26, 8, 0, 0, 0, time.UTC))
	if !strings.Contains(body, "UNVERIFIED import") {
		t.Fatalf("weekly email does not label the baseline:\n%s", body)
	}
	failed := unverifiedPage()
	failed.Baseline.ImportStatus = "FAILED"
	if shared := SharedFrom(failed, "Acme"); shared.Baseline != nil || shared.BaselineImportStatus != nil {
		t.Fatalf("a failed import's numbers are shared: %+v", shared)
	}
	if subject, body := WeeklyText("Acme", failed, "UTC", "https://desk.example", time.Now()); strings.Contains(body, "Freshdesk:") || strings.Contains(subject, "Freshdesk") {
		t.Fatalf("a failed import's numbers are emailed:\n%s\n%s", subject, body)
	}
}
