package metaconversions

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5"
)

// Event names, as Meta's standard events call them.
const (
	EventStartTrial = "StartTrial"
	EventSubscribe  = "Subscribe"
)

// maxUserAgent bounds what is kept of a browser's User-Agent header; real ones are far shorter.
const maxUserAgent = 1024

var currencyCode = regexp.MustCompile(`^[A-Za-z]{3}$`)

// StartTrial is a cloud sign-up that came from a Meta ad.
type StartTrial struct {
	WorkspaceID string
	// FBCLID is the click ID Meta added to the ad's link, unchanged (it is case-sensitive).
	FBCLID string
	// ClickIDSeenAt is when the browser first saw it: Meta's click time.
	ClickIDSeenAt time.Time
	// At is when the trial started.
	At time.Time
	// ClientIP and UserAgent are the sign-up request's client address and browser.
	ClientIP, UserAgent string
	// SourceURL is the page the trial started on.
	SourceURL string
}

// fbc is Meta's click parameter for a server that keeps no _fbc cookie: version fb, subdomain index 1, the
// first time the click ID was seen in milliseconds, and the click ID itself.
func fbc(fbclid string, seen time.Time) string {
	return "fb.1." + strconv.FormatInt(seen.UnixMilli(), 10) + "." + fbclid
}

// RecordStartTrial queues the StartTrial of a sign-up for the worker; once per workspace. It sends nothing
// itself, so the sign-up never waits on Meta.
func RecordStartTrial(ctx context.Context, db *database.DB, in StartTrial) error {
	userAgent := in.UserAgent
	if len(userAgent) > maxUserAgent {
		userAgent = userAgent[:maxUserAgent]
	}
	err := db.InWorkspace(ctx, in.WorkspaceID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO meta_conversion_events(workspace_id,event_name,event_time,fbc,client_ip,user_agent,event_source_url,next_attempt_at,created_at)
			VALUES($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''),$7,$3,$3) ON CONFLICT DO NOTHING`,
			in.WorkspaceID, EventStartTrial, in.At, fbc(in.FBCLID, in.ClickIDSeenAt), in.ClientIP, userAgent, in.SourceURL)
		return err
	})
	if err != nil {
		return fmt.Errorf("queue StartTrial: %w", err)
	}
	return nil
}

// RecordSubscribe queues the Subscribe of a workspace's first payment, with the price in minor units
// (cents) and its currency; once per workspace. Only a workspace whose sign-up came from a Meta ad has a
// StartTrial to take the click, address and browser from; any other gets nothing. A price that is not
// positive or a currency that is not a three-letter code leaves the value out.
func RecordSubscribe(ctx context.Context, db *database.DB, workspaceID string, at time.Time, sourceURL string, amountMinor int64, currency string) error {
	var value *float64
	var code *string
	if amountMinor > 0 && currencyCode.MatchString(currency) {
		major := float64(amountMinor) / 100
		upper := strings.ToUpper(currency)
		value, code = &major, &upper
	}
	err := db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO meta_conversion_events(workspace_id,event_name,event_time,fbc,client_ip,user_agent,event_source_url,value,currency,next_attempt_at,created_at)
			SELECT workspace_id, $2, $3, fbc, client_ip, user_agent, $4, $5, $6, $3, $3 FROM meta_conversion_events
			WHERE workspace_id=$1 AND event_name=$7 AND client_ip IS NOT NULL
			ON CONFLICT DO NOTHING`, workspaceID, EventSubscribe, at, sourceURL, value, code, EventStartTrial)
		return err
	})
	if err != nil {
		return fmt.Errorf("queue Subscribe: %w", err)
	}
	return nil
}
