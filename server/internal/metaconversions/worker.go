package metaconversions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5"
)

const (
	// batch is how many due events one pass claims.
	batch = 20
	// lease keeps a claimed event from a second server while this one sends it.
	lease = 2 * time.Minute
	// maxEventAge: Meta refuses a whole request when an event_time is more than 7 days old, so an event
	// still unsent an hour before that is given up.
	maxEventAge = 7*24*time.Hour - time.Hour
	// contextAge: Meta keeps its own click cookie 90 days; after that the sign-up's address and browser
	// are of no more use and are cleared.
	contextAge = 90 * 24 * time.Hour
	// maxBackoff caps the wait between two tries of one event.
	maxBackoff = 6 * time.Hour
	// maxError bounds the reason kept of a failed try.
	maxError = 300
)

// WorkerOptions are optional; the zero value uses the system clock, a 10 second HTTP client and slog.Default.
type WorkerOptions struct {
	Now    func() time.Time
	Client *http.Client
	Logger *slog.Logger
}

// Worker sends the queued events, retrying a failed one later with the same event_id.
type Worker struct {
	db      *database.DB
	config  Config
	options WorkerOptions
}

// NewWorker returns a worker for config. A config that is not Enabled sends nothing.
func NewWorker(db *database.DB, config Config, options WorkerOptions) *Worker {
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	if options.Client == nil {
		options.Client = &http.Client{Timeout: 10 * time.Second}
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	return &Worker{db: db, config: config, options: options}
}

// Run sends due events now and then every interval, until ctx ends.
func (w *Worker) Run(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		if _, err := w.RunOnce(ctx); err != nil && ctx.Err() == nil {
			w.options.Logger.Error("meta conversions", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

type claimed struct{ workspaceID, eventName string }

// RunOnce sends every due event and says how many Meta accepted. A failure of one event is logged, kept
// with its reason for the next try, and does not stop the others.
func (w *Worker) RunOnce(ctx context.Context) (int, error) {
	if !w.config.Enabled() {
		return 0, nil
	}
	now := w.options.Now()
	due, err := w.claim(ctx, now)
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, event := range due {
		if w.send(ctx, event, now) {
			sent++
		}
	}
	return sent, nil
}

// claim gives up the events Meta would refuse as too old, clears the address and browser nobody needs any
// more, and leases the due events. Only workspace ids and event names leave the transaction.
func (w *Worker) claim(ctx context.Context, now time.Time) ([]claimed, error) {
	var due []claimed
	err := w.db.Privileged(ctx, database.PrivilegeMetaConversionClaim, func(tx pgx.Tx) error {
		abandoned, err := tx.Exec(ctx, `UPDATE meta_conversion_events SET abandoned_at=$1, client_ip=NULL, user_agent=NULL
			WHERE sent_at IS NULL AND abandoned_at IS NULL AND event_time < $2`, now, now.Add(-maxEventAge))
		if err != nil {
			return fmt.Errorf("give up old events: %w", err)
		}
		if count := abandoned.RowsAffected(); count > 0 {
			w.options.Logger.Warn("meta conversions given up: older than Meta accepts", "events", count)
		}
		if _, err := tx.Exec(ctx, `UPDATE meta_conversion_events SET client_ip=NULL, user_agent=NULL
			WHERE (client_ip IS NOT NULL OR user_agent IS NOT NULL) AND created_at < $1 AND (sent_at IS NOT NULL OR abandoned_at IS NOT NULL)`, now.Add(-contextAge)); err != nil {
			return fmt.Errorf("clear old sign-up addresses: %w", err)
		}
		rows, err := tx.Query(ctx, `UPDATE meta_conversion_events SET attempts=attempts+1, next_attempt_at=$2
			WHERE (workspace_id, event_name) IN (SELECT workspace_id, event_name FROM meta_conversion_events
				WHERE sent_at IS NULL AND abandoned_at IS NULL AND next_attempt_at <= $1
				ORDER BY next_attempt_at LIMIT $3 FOR UPDATE SKIP LOCKED)
			RETURNING workspace_id::text, event_name`, now, now.Add(lease), batch)
		if err != nil {
			return fmt.Errorf("claim due events: %w", err)
		}
		due, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (claimed, error) {
			var event claimed
			return event, row.Scan(&event.workspaceID, &event.eventName)
		})
		return err
	})
	return due, err
}

// serverEvent is one event of the request, in Meta's field names. Nothing else about the workspace or its
// people is ever added: no email, name, company or workspace name.
type serverEvent struct {
	EventName      string      `json:"event_name"`
	EventTime      int64       `json:"event_time"`
	EventID        string      `json:"event_id"`
	ActionSource   string      `json:"action_source"`
	EventSourceURL string      `json:"event_source_url"`
	UserData       userData    `json:"user_data"`
	CustomData     *customData `json:"custom_data,omitempty"`
}

type userData struct {
	FBC             string `json:"fbc"`
	ClientIPAddress string `json:"client_ip_address,omitempty"`
	ClientUserAgent string `json:"client_user_agent,omitempty"`
}

type customData struct {
	Value    float64 `json:"value"`
	Currency string  `json:"currency"`
}

type request struct {
	Data          []serverEvent `json:"data"`
	AccessToken   string        `json:"access_token"`
	TestEventCode string        `json:"test_event_code,omitempty"`
}

// send reads one claimed event in its workspace, posts it and settles it: sent, or due again after a
// backoff with the reason. The event_id is the workspace id, so Meta counts a retry once.
func (w *Worker) send(ctx context.Context, event claimed, now time.Time) bool {
	ctx = context.WithoutCancel(ctx)
	var body serverEvent
	var attempts int
	found := false
	err := w.db.InWorkspace(ctx, event.workspaceID, func(tx pgx.Tx) error {
		var eventTime time.Time
		var ip, agent *string
		var value *float64
		var currency *string
		err := tx.QueryRow(ctx, `SELECT event_time, fbc, client_ip, user_agent, event_source_url, value::float8, currency, attempts
			FROM meta_conversion_events WHERE workspace_id=$1 AND event_name=$2 AND sent_at IS NULL AND abandoned_at IS NULL`,
			event.workspaceID, event.eventName).Scan(&eventTime, &body.UserData.FBC, &ip, &agent, &body.EventSourceURL, &value, &currency, &attempts)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		body.EventName, body.EventTime, body.EventID, body.ActionSource = event.eventName, eventTime.Unix(), event.workspaceID, "website"
		if ip != nil {
			body.UserData.ClientIPAddress = *ip
		}
		if agent != nil {
			body.UserData.ClientUserAgent = *agent
		}
		if value != nil && currency != nil {
			body.CustomData = &customData{Value: *value, Currency: *currency}
		}
		return nil
	})
	if err != nil || !found {
		if err != nil {
			w.options.Logger.Error("meta conversion cannot be read", "workspace", event.workspaceID, "event", event.eventName, "error", err)
		}
		return false
	}
	failure := w.post(ctx, body)
	if failure != "" {
		w.options.Logger.Warn("meta conversion not sent; retried later", "workspace", event.workspaceID, "event", event.eventName, "attempts", attempts, "error", failure)
	}
	if err := w.settle(ctx, event, now, attempts, failure); err != nil {
		w.options.Logger.Error("meta conversion cannot be settled", "workspace", event.workspaceID, "event", event.eventName, "error", err)
	}
	return failure == ""
}

// post sends one event and returns why it failed, or "" when Meta accepted it. The token travels in the
// body, never in the address, so no log or proxy line carries it.
func (w *Worker) post(ctx context.Context, event serverEvent) string {
	payload, err := json.Marshal(request{Data: []serverEvent{event}, AccessToken: w.config.AccessToken, TestEventCode: w.config.TestEventCode})
	if err != nil {
		return "encode: " + err.Error()
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, w.config.endpoint(), bytes.NewReader(payload))
	if err != nil {
		return "request: " + err.Error()
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := w.options.Client.Do(httpRequest)
	if err != nil {
		return truncate("request: " + err.Error())
	}
	defer response.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return ""
	}
	var graphError struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(answer, &graphError)
	return truncate(fmt.Sprintf("HTTP %d: %s", response.StatusCode, graphError.Error.Message))
}

// settle records the outcome. A sent Subscribe is the last event of a workspace, so its sign-up's address
// and browser are cleared from both rows.
func (w *Worker) settle(ctx context.Context, event claimed, now time.Time, attempts int, failure string) error {
	return w.db.InWorkspace(ctx, event.workspaceID, func(tx pgx.Tx) error {
		if failure != "" {
			_, err := tx.Exec(ctx, `UPDATE meta_conversion_events SET next_attempt_at=$3, last_error=$4 WHERE workspace_id=$1 AND event_name=$2`,
				event.workspaceID, event.eventName, now.Add(backoff(attempts)), failure)
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE meta_conversion_events SET sent_at=$3, last_error=NULL WHERE workspace_id=$1 AND event_name=$2`,
			event.workspaceID, event.eventName, now); err != nil {
			return err
		}
		if event.eventName != EventSubscribe {
			return nil
		}
		_, err := tx.Exec(ctx, `UPDATE meta_conversion_events SET client_ip=NULL, user_agent=NULL WHERE workspace_id=$1`, event.workspaceID)
		return err
	})
}

// backoff is the wait after the given number of tries: 1 minute, doubling, at most 6 hours.
func backoff(attempts int) time.Duration {
	wait := time.Minute
	for i := 1; i < attempts && wait < maxBackoff; i++ {
		wait *= 2
	}
	return min(wait, maxBackoff)
}

func truncate(reason string) string {
	if len(reason) > maxError {
		return reason[:maxError]
	}
	return reason
}
