// Package polar is the boundary to Polar (polar.sh): webhook signature checks, the two API calls
// the product needs (checkout and customer portal) and the environment configuration.
package polar

import (
	"net/http"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/standardwebhooks"
)

// WebhookTolerance is how far a webhook timestamp may be from now, in either direction.
const WebhookTolerance = standardwebhooks.Tolerance

// ErrInvalidSignature covers every reason a webhook is rejected, so the caller cannot leak which
// check failed.
var ErrInvalidSignature = standardwebhooks.ErrInvalidSignature

// VerifyWebhook checks the Standard Webhooks signature Polar sends in the webhook-id,
// webhook-timestamp and webhook-signature headers. Polar secrets start with "whsec_".
func VerifyWebhook(secret string, header http.Header, body []byte, now time.Time) error {
	return standardwebhooks.Verify(secret, header.Get("webhook-id"), header.Get("webhook-timestamp"), header.Get("webhook-signature"), body, now)
}
