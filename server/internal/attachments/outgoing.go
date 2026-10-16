package attachments

// Limits for the files an agent attaches to one reply or note. A reply is emailed with its files
// base64-encoded, so the total must stay below what mail relays accept.
const (
	MaxOutgoingFiles = 10
	MaxOutgoingBytes = 10 << 20
)
