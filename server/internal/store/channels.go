package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5"
)

// WithChannels returns a store that routes what arrives and every reply through the channel types
// of the registry. A store without one refuses to queue a reply (channels.ErrNoRegistry): skipping it
// would look like a workspace with no channel to send through.
func (s *Store) WithChannels(registry *channels.Registry) *Store {
	copy := *s
	copy.channels = registry
	return &copy
}

// Channels is the registry the store routes through; nil when none was given.
func (s *Store) Channels() *channels.Registry { return s.channels }

const channelRowColumns = `id::text,workspace_id::text,type,name,enabled,settings,COALESCE(external_account,''),created_at,updated_at,disabled_at,deleted_at,is_default`

// scanChannelRow reads a channel and resolves its settings over its type's defaults. A type this
// server does not run keeps empty defaults.
func (s *Store) scanChannelRow(row rowScanner) (channels.Channel, error) {
	var channel channels.Channel
	var kind string
	var settings []byte
	if err := row.Scan(&channel.ID, &channel.WorkspaceID, &kind, &channel.Name, &channel.Enabled, &settings, &channel.ExternalAccount,
		&channel.CreatedAt, &channel.UpdatedAt, &channel.DisabledAt, &channel.DeletedAt, &channel.IsDefault); err != nil {
		return channels.Channel{}, err
	}
	channel.Type = channels.Type(kind)
	channel.CreatedAt, channel.UpdatedAt = channel.CreatedAt.UTC(), channel.UpdatedAt.UTC()
	for _, at := range []*time.Time{channel.DisabledAt, channel.DeletedAt} {
		if at != nil {
			*at = at.UTC()
		}
	}
	var defaults channels.Settings
	if adapter, ok := s.channels.Adapter(channel.Type); ok {
		defaults = adapter.Definition().Defaults
	}
	merged, err := defaults.Merge(settings)
	if err != nil {
		return channels.Channel{}, fmt.Errorf("decode settings of channel %s: %w", channel.ID, err)
	}
	channel.Settings = merged
	return channel, nil
}

// channelByID reads a channel of the workspace, removed or not.
func (s *Store) channelByID(ctx context.Context, tx pgx.Tx, workspaceID, id string) (channels.Channel, error) {
	channel, err := s.scanChannelRow(tx.QueryRow(ctx, `SELECT `+channelRowColumns+` FROM channels WHERE workspace_id=$1 AND id=$2::uuid`, workspaceID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return channels.Channel{}, channels.ErrNotFound
	}
	return channel, err
}

// defaultChannelOfType reads the workspace's default channel of a type (is_default); found is false
// when the type has no channel. It answers what names no channel: the routes of one email channel and
// one web form, a hosted address no channel claims, and a new conversation started without a channel.
func (s *Store) defaultChannelOfType(ctx context.Context, tx pgx.Tx, workspaceID string, kind channels.Type) (channels.Channel, bool, error) {
	channel, err := s.scanChannelRow(tx.QueryRow(ctx, `SELECT `+channelRowColumns+` FROM channels
		WHERE workspace_id=$1 AND type=$2 AND is_default`, workspaceID, string(kind)))
	if errors.Is(err, pgx.ErrNoRows) {
		return channels.Channel{}, false, nil
	}
	if err != nil {
		return channels.Channel{}, false, fmt.Errorf("load %s channel: %w", kind, err)
	}
	return channel, true, nil
}

// DefaultChannelOfType is defaultChannelOfType in its own transaction.
func (s *Store) DefaultChannelOfType(ctx context.Context, workspaceID string, kind channels.Type) (channels.Channel, bool, error) {
	type found struct {
		channel channels.Channel
		ok      bool
	}
	got, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (found, error) {
		channel, ok, err := s.defaultChannelOfType(ctx, tx, workspaceID, kind)
		return found{channel, ok}, err
	})
	return got.channel, got.ok, err
}

// GetChannel reads a channel of the workspace, removed or not.
func (s *Store) GetChannel(ctx context.Context, workspaceID, id string) (channels.Channel, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (channels.Channel, error) {
		return s.channelByID(ctx, tx, workspaceID, id)
	})
}

// ChannelForExternalAccount finds the live channel of a type that is an account outside. Accounts are
// unique on the whole server, so this is the one lookup that crosses workspaces.
func (s *Store) ChannelForExternalAccount(ctx context.Context, kind channels.Type, account string) (channels.Channel, bool, error) {
	var workspaceID, id string
	err := s.db.Privileged(ctx, database.PrivilegeChannelRoute, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT workspace_id::text,id::text FROM channels WHERE type=$1 AND external_account=$2 AND deleted_at IS NULL`,
			string(kind), account).Scan(&workspaceID, &id)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return channels.Channel{}, false, nil
	}
	if err != nil {
		return channels.Channel{}, false, fmt.Errorf("route channel account: %w", err)
	}
	channel, err := s.GetChannel(ctx, workspaceID, id)
	return channel, err == nil, err
}

// ChannelByPublicID finds the live channel of a type that a public address names by its id (a web
// form's own submit address). Channel ids are random, so one form's address tells nothing of another's.
func (s *Store) ChannelByPublicID(ctx context.Context, kind channels.Type, id string) (channels.Channel, bool, error) {
	var workspaceID string
	err := s.db.Privileged(ctx, database.PrivilegeChannelRoute, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT workspace_id::text FROM channels WHERE type=$1 AND id::text=$2 AND deleted_at IS NULL`, string(kind), id).Scan(&workspaceID)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return channels.Channel{}, false, nil
	}
	if err != nil {
		return channels.Channel{}, false, fmt.Errorf("route channel id: %w", err)
	}
	channel, err := s.GetChannel(ctx, workspaceID, id)
	return channel, err == nil, err
}

// lockChannelType serializes the changes that add, remove or move the default of a workspace's
// channels of one type: the first channel of a type has no row yet to lock.
func lockChannelType(ctx context.Context, tx pgx.Tx, workspaceID string, kind channels.Type) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text||':channels:'||$2,0))`, workspaceID, string(kind)); err != nil {
		return fmt.Errorf("lock %s channels: %w", kind, err)
	}
	return nil
}

// ChannelActor is who changes a channel: an agent, through a session or an agent token.
type ChannelActor struct {
	AgentID, TokenID string
}

// asActor is the same person and token as the actor of a message.
func (a ChannelActor) asActor() Actor { return Actor{AgentID: a.AgentID, TokenID: a.TokenID} }

// insertChannelEvent records a change in the channel's history.
func insertChannelEvent(ctx context.Context, tx pgx.Tx, channel channels.Channel, kind string, actor ChannelActor, detail map[string]any) error {
	if detail == nil {
		detail = map[string]any{}
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("encode channel event: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO channel_events(workspace_id,channel_id,channel_type,channel_name,kind,actor_agent_id,actor_token_id,detail)
		VALUES($1,$2,$3,$4,$5,NULLIF($6,'')::uuid,NULLIF($7,'')::uuid,$8)`,
		channel.WorkspaceID, channel.ID, string(channel.Type), channel.Name, kind, actor.AgentID, actor.TokenID, encoded); err != nil {
		return fmt.Errorf("record channel event: %w", err)
	}
	return nil
}

// ChannelEvent is one entry of a channel's history.
type ChannelEvent struct {
	ID, ChannelID, ChannelType, ChannelName, Kind string
	ActorAgentID, ActorName, ActorTokenLabel      *string
	Detail                                        json.RawMessage
	CreatedAt                                     time.Time
}

// Objects is the attachment storage the store verifies against; nil when none was given.
func (s *Store) Objects() attachments.Storage { return s.objects }

// InWorkspace runs fn in one transaction scoped to the workspace, for callers that read several
// things of a channel together.
func (s *Store) InWorkspace(ctx context.Context, workspaceID string, fn func(pgx.Tx) error) error {
	return s.db.InWorkspace(ctx, workspaceID, fn)
}
