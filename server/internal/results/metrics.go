package results

import (
	"math"
	"sort"
	"time"
)

// Day and Settle are the units of the counting rules (docs/user/results.md): a resolution counts only
// when the conversation stayed resolved or closed for Settle.
const (
	Day    = 24 * time.Hour
	Settle = 7 * Day
	// MinSample is the smallest sample a median or p90 is shown for.
	MinSample = 5
)

// Window is a period the page shows, ending at the moment measured.
type Window struct {
	Key  string
	Days int
}

// Windows are the periods every snapshot holds: 30 days (the page's default) and 7.
var Windows = []Window{{Key: "30d", Days: 30}, {Key: "7d", Days: 7}}

// StatusChange is one row of conversation_status_changes, Colliedesk's own (source colliedesk).
type StatusChange struct {
	From, To string
	At       time.Time
}

// Stats are the source's ticket stats kept on an imported conversation (origin.stats).
type Stats struct {
	Status     string
	ResolvedAt *time.Time
}

// Conversation is what the rules read of one customer-started conversation: the caller loads only
// conversations whose first public message is incoming, and only events at or before the moment
// measured.
type Conversation struct {
	ID            string
	CreatedAt     time.Time
	FirstIncoming time.Time
	// FirstReply is the first outgoing message at or after FirstIncoming; nil while none.
	FirstReply *time.Time
	// Changes (live) are the status changes in order; Stats (baseline) the imported ticket stats, nil
	// when the import did not read them.
	Changes []StatusChange
	Stats   *Stats
	// HandleMinutes is the time logged on it (imported time entries), 0 when none.
	HandleMinutes int
}

// Side says which rules decide a resolution: Colliedesk's status history or the imported stats.
type Side int

const (
	Live Side = iota
	Baseline
)

// Input is everything one side's numbers are computed from.
type Input struct {
	Side Side
	// Measured is the moment the numbers describe; End is where the windows end (Measured for live,
	// the newest imported conversation for a baseline whose history stops before the import).
	Measured, End time.Time
	// HistorySince is when Colliedesk's status history became complete (live only).
	HistorySince time.Time
	// First is the oldest customer-started conversation of the side, if any.
	First         *time.Time
	Conversations []Conversation
	Drafts        []Draft
}

// Draft is an AI draft a person decided on: its outcome and conversation.
type Draft struct {
	ConversationID string
	Outcome        string
	DecidedAt      time.Time
}

// Range is a half-open period (From, To].
type Range struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

func (r Range) holds(at time.Time) bool { return at.After(r.From) && !at.After(r.To) }

// Numbers is one window of one side. Field names are the API's (contracts/openapi.yaml ResultsWindow).
type Numbers struct {
	Created         Range         `json:"created"`
	ResolvedCreated Range         `json:"resolved_created"`
	AvailableOn     *time.Time    `json:"available_on"`
	FirstResponse   FirstResponse `json:"first_response"`
	Resolution      *Resolution   `json:"resolution"`
	Reopened        *Reopened     `json:"reopened"`
	Volume          Volume        `json:"volume"`
	Handle          *Handle       `json:"handle"`
	AI              *AIDrafts     `json:"ai"`
	members         map[string][]string
}

type FirstResponse struct {
	MedianSeconds *int64 `json:"median_seconds"`
	P90Seconds    *int64 `json:"p90_seconds"`
	Answered      int    `json:"answered"`
	Waiting       int    `json:"waiting"`
}

type Resolution struct {
	MedianSeconds *int64 `json:"median_seconds"`
	Resolved      int    `json:"resolved"`
	Unresolved    int    `json:"unresolved"`
	// WithoutResolveTime counts imported tickets whose stats were not read or held no resolve time.
	WithoutResolveTime int `json:"without_resolve_time"`
}

type Reopened struct {
	Rate        *float64 `json:"rate"`
	Reopened    int      `json:"reopened"`
	Resolutions int      `json:"resolutions"`
}

type Volume struct {
	Count   int       `json:"count"`
	From    time.Time `json:"from"`
	Days    float64   `json:"days"`
	PerWeek *float64  `json:"per_week"`
}

type Handle struct {
	MedianMinutes *int64 `json:"median_minutes"`
	Conversations int    `json:"conversations"`
}

type AIDrafts struct {
	SentUnchanged int `json:"sent_unchanged"`
	SentEdited    int `json:"sent_edited"`
	Discarded     int `json:"discarded"`
	// ConversationsSentUnchanged have at least one draft sent nearly unchanged: hours saved count them.
	ConversationsSentUnchanged int `json:"conversations_sent_unchanged"`
}

// Metric names of results_members, after "<window>.".
const (
	MetricFirstResponse        = "first_response"
	MetricFirstResponseWaiting = "first_response_waiting"
	MetricResolution           = "resolution"
	MetricUnresolved           = "unresolved"
	MetricWithoutResolveTime   = "without_resolve_time"
	MetricReopened             = "reopened"
	MetricVolume               = "volume"
	MetricHandle               = "handle"
	MetricAISentUnchanged      = "ai_sent_unchanged"
	MetricAISentEdited         = "ai_sent_edited"
	MetricAIDiscarded          = "ai_discarded"
)

// Metrics are every metric name a snapshot's members may hold.
var Metrics = []string{MetricFirstResponse, MetricFirstResponseWaiting, MetricResolution, MetricUnresolved, MetricWithoutResolveTime,
	MetricReopened, MetricVolume, MetricHandle, MetricAISentUnchanged, MetricAISentEdited, MetricAIDiscarded}

// Compute applies the counting rules of docs/user/results.md to one side, for every window.
func Compute(in Input) map[string]*Numbers {
	out := make(map[string]*Numbers, len(Windows))
	for _, window := range Windows {
		out[window.Key] = computeWindow(in, window)
	}
	return out
}

func computeWindow(in Input, window Window) *Numbers {
	length := time.Duration(window.Days) * Day
	n := &Numbers{
		Created:         Range{From: in.End.Add(-length), To: in.End},
		ResolvedCreated: Range{From: in.End.Add(-length - Settle), To: in.End.Add(-Settle)},
		members:         map[string][]string{},
	}
	add := func(metric, id string) { n.members[metric] = append(n.members[metric], id) }

	var replies []int64
	for _, c := range in.Conversations {
		if !n.Created.holds(c.CreatedAt) {
			continue
		}
		n.Volume.Count++
		add(MetricVolume, c.ID)
		if c.FirstReply == nil {
			n.FirstResponse.Waiting++
			add(MetricFirstResponseWaiting, c.ID)
			continue
		}
		n.FirstResponse.Answered++
		add(MetricFirstResponse, c.ID)
		replies = append(replies, int64(c.FirstReply.Sub(c.FirstIncoming)/time.Second))
	}
	n.FirstResponse.MedianSeconds, n.FirstResponse.P90Seconds = median(replies), p90(replies)
	n.Volume.From = n.Created.From
	if in.First != nil && in.First.After(n.Volume.From) {
		n.Volume.From = *in.First
	}
	if days := n.Created.To.Sub(n.Volume.From).Hours() / 24; days >= 1 {
		n.Volume.Days = math.Round(days*10) / 10
		perWeek := math.Round(float64(n.Volume.Count)/days*7*10) / 10
		n.Volume.PerWeek = &perWeek
	} else if days > 0 {
		n.Volume.Days = math.Round(days*10) / 10
	}

	if in.Side == Live {
		// The resolution window needs status history from its first day.
		if needed := n.ResolvedCreated.From; in.HistorySince.After(needed) {
			available := in.HistorySince.Add(length + Settle)
			n.AvailableOn = &available
		}
		n.AI = drafts(in, n, length)
	}
	if n.AvailableOn != nil {
		return n
	}

	resolution := &Resolution{}
	var resolveTimes, handles []int64
	reopened := &Reopened{}
	settledBy := in.Measured.Add(-Settle)
	for _, c := range in.Conversations {
		if !n.ResolvedCreated.holds(c.CreatedAt) {
			continue
		}
		var resolvedAt *time.Time
		switch in.Side {
		case Live:
			var resolutions, reopens int
			resolvedAt, resolutions, reopens = liveResolution(c.Changes, settledBy)
			reopened.Resolutions += resolutions
			reopened.Reopened += reopens
			if reopens > 0 {
				add(MetricReopened, c.ID)
			}
		case Baseline:
			if c.Stats == nil || (resolvedStatus(c.Stats.Status) && c.Stats.ResolvedAt == nil) {
				resolution.WithoutResolveTime++
				add(MetricWithoutResolveTime, c.ID)
				continue
			}
			if resolvedStatus(c.Stats.Status) && !c.Stats.ResolvedAt.After(settledBy) && !c.Stats.ResolvedAt.Before(c.CreatedAt) {
				resolvedAt = c.Stats.ResolvedAt
			}
		}
		if resolvedAt == nil {
			resolution.Unresolved++
			add(MetricUnresolved, c.ID)
			continue
		}
		resolution.Resolved++
		add(MetricResolution, c.ID)
		resolveTimes = append(resolveTimes, int64(resolvedAt.Sub(c.CreatedAt)/time.Second))
		if c.HandleMinutes > 0 {
			handles = append(handles, int64(c.HandleMinutes))
			add(MetricHandle, c.ID)
		}
	}
	resolution.MedianSeconds = median(resolveTimes)
	n.Resolution = resolution
	switch in.Side {
	case Live:
		if reopened.Resolutions >= MinSample {
			rate := math.Round(float64(reopened.Reopened)/float64(reopened.Resolutions)*1000) / 1000
			reopened.Rate = &rate
		}
		n.Reopened = reopened
	case Baseline:
		n.Handle = &Handle{MedianMinutes: median(handles), Conversations: len(handles)}
	}
	return n
}

func drafts(in Input, n *Numbers, length time.Duration) *AIDrafts {
	decided := Range{From: in.Measured.Add(-length), To: in.Measured}
	ai := &AIDrafts{}
	unchanged := map[string]bool{}
	seen := map[string]map[string]bool{}
	for _, d := range in.Drafts {
		if !decided.holds(d.DecidedAt) {
			continue
		}
		var metric string
		switch d.Outcome {
		case "sent_unchanged":
			ai.SentUnchanged++
			metric = MetricAISentUnchanged
			unchanged[d.ConversationID] = true
		case "sent_edited":
			ai.SentEdited++
			metric = MetricAISentEdited
		case "discarded":
			ai.Discarded++
			metric = MetricAIDiscarded
		default:
			continue
		}
		if seen[metric] == nil {
			seen[metric] = map[string]bool{}
		}
		if !seen[metric][d.ConversationID] {
			seen[metric][d.ConversationID] = true
			n.members[metric] = append(n.members[metric], d.ConversationID)
		}
	}
	ai.ConversationsSentUnchanged = len(unchanged)
	return ai
}

func resolvedStatus(status string) bool { return status == "resolved" || status == "closed" }

// liveResolution reads a conversation's status history: every entry into resolved or closed at or
// before settledBy is a resolution; one left (to open or pending) within Settle was reopened, any
// other stuck. It returns the last resolution that stuck, and how many resolutions and reopens.
func liveResolution(changes []StatusChange, settledBy time.Time) (*time.Time, int, int) {
	var stuck *time.Time
	resolutions, reopens := 0, 0
	for i, change := range changes {
		if resolvedStatus(change.From) || !resolvedStatus(change.To) || change.At.After(settledBy) {
			continue
		}
		resolutions++
		var left *time.Time
		for _, next := range changes[i+1:] {
			if !resolvedStatus(next.To) {
				left = &next.At
				break
			}
		}
		if left != nil && left.Sub(change.At) <= Settle {
			reopens++
			continue
		}
		at := change.At
		stuck = &at
	}
	return stuck, resolutions, reopens
}

// median is the middle value (the mean of the two middle ones for an even count), nil below MinSample.
func median(values []int64) *int64 {
	if len(values) < MinSample {
		return nil
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	middle := len(sorted) / 2
	value := sorted[middle]
	if len(sorted)%2 == 0 {
		value = (sorted[middle-1] + sorted[middle]) / 2
	}
	return &value
}

// p90 is the nearest-rank 90th percentile: the smallest value at least 90% of the values are at or
// below. Nil below MinSample.
func p90(values []int64) *int64 {
	if len(values) < MinSample {
		return nil
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	rank := int(math.Ceil(0.9 * float64(len(sorted))))
	value := sorted[rank-1]
	return &value
}

// Members are the conversations each number counts, keyed "<window>.<metric>".
func Members(numbers map[string]*Numbers) map[string][]string {
	out := map[string][]string{}
	for key, n := range numbers {
		for metric, ids := range n.members {
			out[key+"."+metric] = ids
		}
	}
	return out
}
