package domain

import (
	"errors"
	"time"
)

// ErrAIDraftNotReady refuses a reply that names an AI draft which is not this conversation's, not
// written yet, or already sent, discarded or replaced.
var ErrAIDraftNotReady = errors.New("that AI draft is not ready to send from this conversation")

// AIDraftFile is a file left with a written reply draft: only the team sees it until a person sends
// the draft with it.
type AIDraftFile struct {
	ID          string    `json:"id"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	CreatedAt   time.Time `json:"created_at"`
}
