package zalo

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/jackc/pgx/v5"
)

// MaxWebhookBody bounds a delivery, read whole before its signature is checked.
const MaxWebhookBody = 1 << 20

// SignatureHeader carries a delivery's signature: "mac=" and the hex of sha256(app_id, body, timestamp,
// OA secret key), the timestamp read from the body.
const SignatureHeader = "X-ZEvent-Signature"

// Sign is a delivery's signature as Zalo computes it.
func Sign(appID string, body []byte, timestamp, oaSecretKey string) string {
	digest := sha256.New()
	digest.Write([]byte(appID))
	digest.Write(body)
	digest.Write([]byte(timestamp))
	digest.Write([]byte(oaSecretKey))
	return hex.EncodeToString(digest.Sum(nil))
}

// webhook takes the app's deliveries for every OA it serves. One of an OA no channel is answers 200 and
// is dropped, as Zalo sends the app's events of every OA that authorized it here; one whose signature
// does not match is refused and noted on the channel, which then says so.
func (a *Adapter) webhook(kit channels.RouteKit) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, MaxWebhookBody+1))
		if err != nil || len(body) > MaxWebhookBody {
			kit.WriteError(w, http.StatusRequestEntityTooLarge, "too_large", "A delivery is at most 1 MiB.")
			return
		}
		e, err := decode(body)
		if err != nil {
			kit.WriteError(w, http.StatusBadRequest, "invalid_request", "The delivery is not a Zalo event.")
			return
		}
		ok := func() {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
		oa := e.oa()
		if oa == "" {
			ok()
			return
		}
		channel, found, err := kit.FindChannel(r.Context(), oa)
		if err != nil {
			kit.WriteError(w, http.StatusInternalServerError, "internal_error", "Try again.")
			return
		}
		if !found {
			ok()
			return
		}
		var appID string
		if err := kit.InWorkspace(r.Context(), channel, func(tx pgx.Tx) error {
			return tx.QueryRow(r.Context(), `SELECT app_id FROM zalo_channels WHERE workspace_id=$1 AND channel_id=$2`, channel.WorkspaceID, channel.ID).Scan(&appID)
		}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			kit.WriteError(w, http.StatusInternalServerError, "internal_error", "Try again.")
			return
		}
		secret, found, err := kit.Credential(r.Context(), channel, CredentialOASecretKey)
		if err != nil {
			kit.WriteError(w, http.StatusInternalServerError, "internal_error", "Try again.")
			return
		}
		presented := strings.TrimPrefix(r.Header.Get(SignatureHeader), "mac=")
		if !found || appID == "" || e.AppID != appID || e.Timestamp == "" ||
			subtle.ConstantTimeCompare([]byte(presented), []byte(Sign(appID, body, e.Timestamp, secret))) != 1 {
			// Only a delivery of this channel's app is noted, at most once a minute, so the channel says its
			// key may be wrong; it is answered as one of an OA no channel is, so nobody learns which OAs are here.
			if appID != "" && e.AppID == appID {
				now := a.config.Now().UTC()
				_ = kit.InWorkspace(r.Context(), channel, func(tx pgx.Tx) error {
					_, err := tx.Exec(r.Context(), `UPDATE zalo_channels SET signature_failed_at=$3 WHERE workspace_id=$1 AND channel_id=$2
						AND (signature_failed_at IS NULL OR signature_failed_at<$3::timestamptz-interval '1 minute')`, channel.WorkspaceID, channel.ID, now)
					return err
				})
			}
			ok()
			return
		}
		// A delivery signed right shows the key works again.
		_ = kit.InWorkspace(r.Context(), channel, func(tx pgx.Tx) error {
			_, err := tx.Exec(r.Context(), `UPDATE zalo_channels SET signature_failed_at=NULL WHERE workspace_id=$1 AND channel_id=$2 AND signature_failed_at IS NOT NULL`,
				channel.WorkspaceID, channel.ID)
			return err
		})
		if !e.isMessage() {
			ok()
			return
		}
		if _, err := kit.Receive(r.Context(), channel, []channels.Raw{{ExternalEventID: e.id(), JSON: json.RawMessage(body), ReceivedAt: time.Now().UTC()}}); err != nil {
			kit.WriteError(w, http.StatusInternalServerError, "internal_error", "Try again.")
			return
		}
		ok()
		background := context.WithoutCancel(r.Context())
		a.config.Background(func() {
			ctx, cancel := context.WithTimeout(background, 15*time.Second)
			defer cancel()
			a.lookupName(ctx, kit, channel, e)
		})
	}
}
