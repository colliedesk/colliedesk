package domain

import (
	"errors"
	"time"
)

// KBBulkUndoWindow is how long after a bulk action on articles it can be undone.
const KBBulkUndoWindow = 10 * time.Minute

var (
	// ErrKBBulkOperationNotFound means no bulk action of this workspace has the id; another
	// workspace's id is answered the same way.
	ErrKBBulkOperationNotFound = errors.New("bulk action not found")
	// ErrKBBulkUndone means the bulk action was undone already.
	ErrKBBulkUndone = errors.New("this bulk action was undone already")
	// ErrKBBulkExpired means the undo window of the bulk action has passed.
	ErrKBBulkExpired = errors.New("a bulk action can be undone for 10 minutes after it ran, and that time has passed")
)
