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

// GetWebForm returns the settings of the workspace's default web form (its oldest live one); a
// workspace that never set one up has it off. The channel's on or off is the truth.
func (s *Store) GetWebForm(ctx context.Context, workspaceID string) (domain.WebForm, error) {
	form, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.WebForm, error) {
		live, found, err := s.defaultChannelOfType(ctx, tx, workspaceID, domain.ChannelWebForm)
		if err != nil || !found {
			return domain.WebForm{AllowedOrigins: []string{}}, err
		}
		return readWebForm(ctx, tx, workspaceID, live.ID)
	})
	if err != nil {
		return domain.WebForm{}, fmt.Errorf("get web form: %w", err)
	}
	return form, nil
}

func readWebForm(ctx context.Context, tx pgx.Tx, workspaceID, channelID string) (domain.WebForm, error) {
	form := domain.WebForm{AllowedOrigins: []string{}}
	err := tx.QueryRow(ctx, `SELECT f.channel_id::text,c.enabled,f.allowed_origins FROM web_form_channels f
		JOIN channels c ON c.workspace_id=f.workspace_id AND c.id=f.channel_id AND c.deleted_at IS NULL WHERE f.workspace_id=$1 AND f.channel_id::text=$2`, workspaceID, channelID).
		Scan(&form.ChannelID, &form.Enabled, &form.AllowedOrigins)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return domain.WebForm{}, err
	}
	return form, nil
}

// SetWebForm turns the default form on or off and replaces its allowed origins, through the channel
// frame: the first save adds a web form channel. Turning it off keeps the origins.
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
		live, found, err := s.defaultChannelOfType(ctx, tx, workspaceID, domain.ChannelWebForm)
		if err != nil {
			return domain.WebForm{}, err
		}
		input := ChannelInput{Enabled: &enabled, Config: patch}
		if found {
			live, err = s.UpdateChannelTx(ctx, tx, workspaceID, live.ID, input, actor)
		} else {
			live, err = s.CreateChannelTx(ctx, tx, workspaceID, domain.ChannelWebForm, input, actor)
		}
		if err != nil {
			return domain.WebForm{}, err
		}
		return readWebForm(ctx, tx, workspaceID, live.ID)
	})
	if err != nil {
		return domain.WebForm{}, fmt.Errorf("save web form: %w", err)
	}
	return form, nil
}

// OpenForm is a public form that takes submissions now: its workspace, its channel and the origins a
// browser may post from.
type OpenForm struct {
	WorkspaceID, ChannelID string
	Origins                []string
}

// OpenWebForm resolves the default form of the workspace with this slug, the address forms had before
// a workspace could have several. ok is false when the slug is unknown, the form is off or the
// workspace is locked, which callers must answer identically.
func (s *Store) OpenWebForm(ctx context.Context, slug string) (OpenForm, bool, error) {
	workspaceID, found, err := s.WorkspaceForMailSlug(ctx, slug)
	if err != nil || !found {
		return OpenForm{}, false, err
	}
	return s.openWebForm(ctx, workspaceID, "")
}

// OpenWebFormChannel resolves a form by its channel id, its own address. ok is false as for OpenWebForm.
func (s *Store) OpenWebFormChannel(ctx context.Context, channelID string) (OpenForm, bool, error) {
	channel, found, err := s.ChannelByPublicID(ctx, domain.ChannelWebForm, channelID)
	if err != nil || !found {
		return OpenForm{}, false, err
	}
	return s.openWebForm(ctx, channel.WorkspaceID, channel.ID)
}

// openWebForm reads a form of the workspace (channelID, or the default one when empty).
func (s *Store) openWebForm(ctx context.Context, workspaceID, channelID string) (OpenForm, bool, error) {
	type state struct {
		form   domain.WebForm
		locked bool
	}
	got, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (state, error) {
		if channelID == "" {
			live, found, err := s.defaultChannelOfType(ctx, tx, workspaceID, domain.ChannelWebForm)
			if err != nil || !found {
				return state{}, err
			}
			channelID = live.ID
		}
		form, err := readWebForm(ctx, tx, workspaceID, channelID)
		if err != nil || !form.Enabled || !s.limits {
			return state{form: form}, err
		}
		access, err := workspaceAccess(ctx, tx, workspaceID, s.now())
		return state{form: form, locked: access.Locked}, err
	})
	if err != nil {
		return OpenForm{}, false, fmt.Errorf("open web form: %w", err)
	}
	if !got.form.Enabled || got.locked {
		return OpenForm{}, false, nil
	}
	return OpenForm{WorkspaceID: workspaceID, ChannelID: got.form.ChannelID, Origins: got.form.AllowedOrigins}, true, nil
}
