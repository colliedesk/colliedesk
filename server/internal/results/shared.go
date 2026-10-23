package results

import (
	"strings"
	"time"
)

// Shared is what a share link shows to someone without a seat (POST /api/v1/results/shared): totals,
// medians and sample sizes only. It carries no snapshot id (so no list of conversations), no ledger
// source (it names an account) and no compute error. A test pins every key it may hold.
type Shared struct {
	WorkspaceName string                 `json:"workspace_name"`
	ComputedAt    *time.Time             `json:"computed_at"`
	BaselineFrom  *string                `json:"baseline_from"`
	Current       *SharedSide            `json:"current"`
	Baseline      *SharedSide            `json:"baseline"`
	HoursSaved    map[string]*HoursSaved `json:"hours_saved"`
}

// SharedSide is one side's windows, as the page shows them.
type SharedSide struct {
	MeasuredAt time.Time           `json:"measured_at"`
	Windows    map[string]*Numbers `json:"windows"`
}

// SharedFrom keeps of a page what a share link may show. A baseline from a failed import is not shown,
// as on the page.
func SharedFrom(page Page, workspaceName string) Shared {
	shared := Shared{WorkspaceName: workspaceName, HoursSaved: map[string]*HoursSaved{}}
	if page.Current != nil {
		shared.ComputedAt = &page.Current.ComputedAt
		shared.Current = &SharedSide{MeasuredAt: page.Current.MeasuredAt, Windows: page.Current.Windows}
	}
	if page.Baseline != nil && page.BaselineState == "ready" && page.Baseline.ImportStatus != "FAILED" {
		from := "freshdesk"
		if strings.HasPrefix(page.Baseline.Source, "helpscout:") {
			from = "helpscout"
		}
		shared.BaselineFrom = &from
		shared.Baseline = &SharedSide{MeasuredAt: page.Baseline.MeasuredAt, Windows: page.Baseline.Windows}
		shared.HoursSaved = page.HoursSaved
	}
	for _, window := range Windows {
		if _, ok := shared.HoursSaved[window.Key]; !ok {
			shared.HoursSaved[window.Key] = nil
		}
	}
	return shared
}
