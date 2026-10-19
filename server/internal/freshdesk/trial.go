package freshdesk

import (
	"context"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/importkit"
)

// extendTrial gives a workspace that ran a real import its one trial extension (importkit.ExtendTrial).
func (r *run) extendTrial(ctx context.Context) (*time.Time, error) {
	return importkit.ExtendTrial(ctx, r.db, r.workspaceID, r.now())
}

func (r *run) now() time.Time {
	if r.opts.Now != nil {
		return r.opts.Now()
	}
	return time.Now()
}
