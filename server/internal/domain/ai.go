package domain

import "errors"

// ErrAIDraftNotReady refuses a reply that names an AI draft which is not this conversation's, not
// written yet, or already sent, discarded or replaced.
var ErrAIDraftNotReady = errors.New("that AI draft is not ready to send from this conversation")
