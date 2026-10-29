package store

import (
	"context"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/jackc/pgx/v5"
)

// Setup step keys and states, as the API returns them.
const (
	SetupEmail      = "email"
	SetupImport     = "import"
	SetupInvite     = "invite"
	SetupFirstReply = "first_reply"
)

// SetupStep is one step of getting started. Detail is a reason code for a step that needs attention.
type SetupStep struct {
	Key    string  `json:"key"`
	State  string  `json:"state"`
	Detail *string `json:"detail"`
}

// SetupProgress is where a workspace stands on getting started, the goal's step first.
type SetupProgress struct {
	Steps       []SetupStep `json:"steps"`
	Complete    bool        `json:"complete"`
	Recommended *string     `json:"recommended"`
}

// setupFirstStep is the step each sign-up goal starts with; any other goal keeps the usual order.
var setupFirstStep = map[string]string{"leave_helpdesk": SetupImport, "shared_inbox": SetupEmail, "lower_cost": SetupInvite}

// SetupProgress reads the workspace's setup steps. A reply counts once an outgoing message was
// delivered; a note, a queued reply and a failed one do not.
func (s *Store) SetupProgress(ctx context.Context, workspaceID string, now time.Time) (SetupProgress, error) {
	goal, err := s.OnboardingGoal(ctx, workspaceID)
	if err != nil {
		return SetupProgress{}, err
	}
	emailState, emailDetail, err := s.setupEmail(ctx, workspaceID)
	if err != nil {
		return SetupProgress{}, err
	}
	var importState, invite, firstReply string
	err = s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT
			CASE
				WHEN EXISTS (SELECT 1 FROM import_jobs i WHERE i.workspace_id=$1 AND i.kind='import' AND i.state='finished'
					AND NOT EXISTS (SELECT 1 FROM import_jobs u WHERE u.workspace_id=$1 AND u.kind='undo' AND u.undo_of=i.id AND u.state='finished')) THEN 'done'
				WHEN EXISTS (SELECT 1 FROM import_jobs WHERE workspace_id=$1 AND kind='import' AND state IN ('previewing','previewed','queued','running','retry_wait','waiting_key')) THEN 'running'
				ELSE 'none' END,
			CASE
				WHEN (SELECT count(*) FROM agents WHERE workspace_id=$1 AND deactivated_at IS NULL) > 1 THEN 'joined'
				WHEN EXISTS (SELECT 1 FROM pairing_links WHERE workspace_id=$1 AND kind='invite' AND used_at IS NULL AND revoked_at IS NULL AND expires_at>$2) THEN 'invited'
				ELSE 'alone' END,
			CASE WHEN EXISTS (SELECT 1 FROM messages m JOIN channel_outbox o ON o.workspace_id=m.workspace_id AND o.message_id=m.id
				WHERE m.workspace_id=$1 AND m.kind='outgoing' AND o.status='sent') THEN 'sent' ELSE 'none' END`,
			workspaceID, now).Scan(&importState, &invite, &firstReply)
	})
	if err != nil {
		return SetupProgress{}, fmt.Errorf("read setup progress: %w", err)
	}
	steps := map[string]SetupStep{
		SetupEmail:      {Key: SetupEmail, State: emailState, Detail: emailDetail},
		SetupImport:     {Key: SetupImport, State: importState},
		SetupInvite:     {Key: SetupInvite, State: invite},
		SetupFirstReply: {Key: SetupFirstReply, State: firstReply},
	}
	order := []string{SetupEmail, SetupImport, SetupInvite}
	progress := SetupProgress{Steps: make([]SetupStep, 0, 4)}
	if goal != nil {
		if lead, ok := setupFirstStep[*goal]; ok {
			order = []string{lead}
			for _, key := range []string{SetupEmail, SetupImport, SetupInvite} {
				if key != lead {
					order = append(order, key)
				}
			}
			if !setupDone(steps[lead]) {
				progress.Recommended = &lead
			}
		}
	}
	progress.Complete = true
	for _, key := range append(order, SetupFirstReply) {
		progress.Steps = append(progress.Steps, steps[key])
		progress.Complete = progress.Complete && setupDone(steps[key])
	}
	return progress, nil
}

func setupDone(step SetupStep) bool {
	switch step.Key {
	case SetupEmail:
		return step.State == channels.StateWorking
	case SetupImport:
		return step.State == "done"
	case SetupInvite:
		return step.State == "joined"
	}
	return step.State == "sent"
}

// setupEmail summarizes the workspace's live email channels by their best state: working, then waiting
// for the first message, then needing attention (off or broken); none without a channel.
func (s *Store) setupEmail(ctx context.Context, workspaceID string) (string, *string, error) {
	list, err := s.ListChannels(ctx, workspaceID, false)
	if err != nil {
		return "", nil, fmt.Errorf("read email channels: %w", err)
	}
	state, rank := "none", 0
	var detail *string
	for _, channel := range list {
		if channel.Channel.Type != "email" {
			continue
		}
		next, nextRank := channels.StateNeedsAttention, 1
		switch channel.Status.State {
		case channels.StateWorking:
			next, nextRank = channels.StateWorking, 3
		case channels.StateWaiting:
			next, nextRank = channels.StateWaiting, 2
		}
		if nextRank > rank {
			state, rank, detail = next, nextRank, nil
			if next == channels.StateNeedsAttention {
				code := channel.Status.ReasonCode
				if code == "" {
					code = channel.Status.State
				}
				detail = &code
			}
		}
	}
	return state, detail, nil
}
