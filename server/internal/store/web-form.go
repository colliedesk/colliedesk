package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// WebFormSubmission is a validated, normalized submission of the public form.
type WebFormSubmission struct {
	Email   string
	Message string
	Source  string
}

// GetWebForm returns the workspace's web form settings; a workspace that never set it up has it off.
// The channel's on or off is the truth.
func (s *Store) GetWebForm(ctx context.Context, workspaceID string) (domain.WebForm, error) {
	form, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.WebForm, error) {
		return readWebForm(ctx, tx, workspaceID)
	})
	if err != nil {
		return domain.WebForm{}, fmt.Errorf("get web form: %w", err)
	}
	return form, nil
}

func readWebForm(ctx context.Context, tx pgx.Tx, workspaceID string) (domain.WebForm, error) {
	form := domain.WebForm{AllowedOrigins: []string{}}
	err := tx.QueryRow(ctx, `SELECT c.enabled,f.allowed_origins FROM web_form_channels f
		JOIN channels c ON c.workspace_id=f.workspace_id AND c.id=f.channel_id AND c.deleted_at IS NULL WHERE f.workspace_id=$1`, workspaceID).
		Scan(&form.Enabled, &form.AllowedOrigins)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return domain.WebForm{}, err
	}
	return form, nil
}

// SetWebForm turns the form on or off and replaces its allowed origins, through the channel frame:
// the first save adds the web form channel. Turning it off keeps the origins.
func (s *Store) SetWebForm(ctx context.Context, workspaceID string, enabled bool, origins []string) (domain.WebForm, error) {
	return s.SetWebFormAs(ctx, workspaceID, enabled, origins, ChannelActor{})
}

// SetWebFormAs is SetWebForm recording who made the change.
func (s *Store) SetWebFormAs(ctx context.Context, workspaceID string, enabled bool, origins []string, actor ChannelActor) (domain.WebForm, error) {
	if origins == nil {
		origins = []string{}
	}
	patch, err := json.Marshal(map[string][]string{"allowed_origins": origins})
	if err != nil {
		return domain.WebForm{}, err
	}
	form, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.WebForm, error) {
		live, found, err := s.liveChannelOfType(ctx, tx, workspaceID, domain.ChannelWebForm)
		if err != nil {
			return domain.WebForm{}, err
		}
		input := ChannelInput{Enabled: &enabled, Config: patch}
		if found {
			_, err = s.UpdateChannelTx(ctx, tx, workspaceID, live.ID, input, actor)
		} else {
			_, err = s.CreateChannelTx(ctx, tx, workspaceID, domain.ChannelWebForm, input, actor)
		}
		if err != nil {
			return domain.WebForm{}, err
		}
		return readWebForm(ctx, tx, workspaceID)
	})
	if err != nil {
		return domain.WebForm{}, fmt.Errorf("save web form: %w", err)
	}
	return form, nil
}

// OpenWebForm resolves the workspace of a public form by its slug. ok is false when the slug is
// unknown, the form is off or the workspace is locked, which callers must answer identically.
func (s *Store) OpenWebForm(ctx context.Context, slug string) (workspaceID string, origins []string, ok bool, err error) {
	workspaceID, found, err := s.WorkspaceForMailSlug(ctx, slug)
	if err != nil || !found {
		return "", nil, false, err
	}
	type state struct {
		form   domain.WebForm
		locked bool
	}
	got, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (state, error) {
		form, err := readWebForm(ctx, tx, workspaceID)
		if err != nil || !form.Enabled || !s.limits {
			return state{form: form}, err
		}
		access, err := workspaceAccess(ctx, tx, workspaceID, s.now())
		return state{form: form, locked: access.Locked}, err
	})
	if err != nil {
		return "", nil, false, fmt.Errorf("open web form: %w", err)
	}
	if !got.form.Enabled || got.locked {
		return "", nil, false, nil
	}
	return workspaceID, got.form.AllowedOrigins, true, nil
}
