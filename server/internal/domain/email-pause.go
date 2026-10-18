package domain

import "time"

// A workspace's email sending is paused when its replies draw complaints above these limits. The
// minimum count keeps one complaint on a handful of replies from stopping a small team; the rate is the
// ceiling mailbox providers publish for bulk senders.
const (
	// ComplaintPauseWindow is how far back complaints and sent replies are counted.
	ComplaintPauseWindow = 7 * 24 * time.Hour
	// ComplaintPauseMinComplaints is the fewest complaints in the window that can pause sending.
	ComplaintPauseMinComplaints = 3
	// ComplaintPauseRatePerThousand is the complaint rate in the window that must be exceeded, in
	// complaints per thousand replies sent: 3 is 0.3%.
	ComplaintPauseRatePerThousand = 3
)

// EmailPausedDeliveryNote is what a deferred reply shows while its workspace's sending is paused.
const EmailPausedDeliveryNote = "Email sending is paused after spam complaints; contact support."

// ComplaintRateExceeded reports whether complaints on sent replies in the window call for a pause:
// at least ComplaintPauseMinComplaints of them, and a rate above ComplaintPauseRatePerThousand.
func ComplaintRateExceeded(complaints, sent int) bool {
	return complaints >= ComplaintPauseMinComplaints && complaints*1000 > sent*ComplaintPauseRatePerThousand
}

// EmailPause says why and since when a workspace's email sending is paused.
type EmailPause struct {
	PausedAt time.Time `json:"paused_at"`
	Reason   string    `json:"reason"`
}
