package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ChannelInput is a change of a channel: each field set is changed, the others kept.
type ChannelInput struct {
	Name    *string
	Enabled *bool
	// Config is the type's own configuration (its schema in contracts/openapi.yaml).
	Config json.RawMessage
	// Settings replace the rules the channel stores over its type's defaults.
	Settings json.RawMessage
}

var channelNameControls = regexp.MustCompile(`[\x00-\x1f\x7f]`)

func validChannelName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 80 || channelNameControls.MatchString(name) {
		return "", &channels.FieldError{Fields: map[string]string{"name": "Use 1 to 80 characters, without line breaks."}, Message: "Check the channel name."}
	}
	return name, nil
}

func (s *Store) adapterOf(kind channels.Type) (channels.Adapter, error) {
	if s.channels == nil {
		return nil, channels.ErrNoRegistry
	}
	adapter, ok := s.channels.Adapter(kind)
	if !ok {
		return nil, channels.ErrUnknownType
	}
	return adapter, nil
}

// validSettings checks stored settings decode over the type's defaults.
func validSettings(definition channels.Definition, settings json.RawMessage) (json.RawMessage, error) {
	if len(settings) == 0 {
		return json.RawMessage(`{}`), nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(settings, &object); err != nil {
		return nil, &channels.FieldError{Fields: map[string]string{"settings": "Settings must be a JSON object."}, Message: "Check the channel settings."}
	}
	merged, err := definition.Defaults.Merge(settings)
	if err != nil {
		return nil, &channels.FieldError{Fields: map[string]string{"settings": err.Error()}, Message: "Check the channel settings."}
	}
	for key, scope := range merged.Scopes {
		known := false
		for _, declared := range definition.Scopes {
			known = known || declared == key
		}
		if !known {
			return nil, &channels.FieldError{Fields: map[string]string{"settings.scopes." + key: "This channel has no such kind of conversation."},
				Message: "Check the channel settings."}
		}
		if scope.Mode != "" && scope.Mode != channels.ModeAll && scope.Mode != channels.ModeCommands {
			return nil, &channels.FieldError{Fields: map[string]string{"settings.scopes." + key + ".mode": "Use all or commands."},
				Message: "Check the channel settings."}
		}
	}
	return settings, nil
}

// CreateChannel adds a channel of a type with its configuration; the type's Validate and Save run in
// the same transaction, so a refused configuration leaves no channel.
func (s *Store) CreateChannel(ctx context.Context, workspaceID string, kind channels.Type, input ChannelInput, actor ChannelActor) (channels.Channel, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (channels.Channel, error) {
		return s.CreateChannelTx(ctx, tx, workspaceID, kind, input, actor)
	})
}

// CreateChannelTx is CreateChannel inside tx.
func (s *Store) CreateChannelTx(ctx context.Context, tx pgx.Tx, workspaceID string, kind channels.Type, input ChannelInput, actor ChannelActor) (channels.Channel, error) {
	adapter, err := s.adapterOf(kind)
	if err != nil {
		return channels.Channel{}, err
	}
	definition := adapter.Definition()
	if definition.Singleton {
		if _, found, err := s.liveChannelOfType(ctx, tx, workspaceID, kind); err != nil {
			return channels.Channel{}, err
		} else if found {
			return channels.Channel{}, channels.ErrTypeLimit
		}
	}
	name := definition.Name
	if input.Name != nil {
		if name, err = validChannelName(*input.Name); err != nil {
			return channels.Channel{}, err
		}
	}
	settings, err := validSettings(definition, input.Settings)
	if err != nil {
		return channels.Channel{}, err
	}
	change, err := adapter.Config().Validate(ctx, tx, workspaceID, nil, input.Config)
	if err != nil {
		return channels.Channel{}, err
	}
	enabled := input.Enabled == nil || *input.Enabled
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO channels(workspace_id,type,singleton,name,enabled,settings,external_account,created_by_agent_id,disabled_at,disabled_by_agent_id)
		VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,'')::uuid,CASE WHEN NOT $5 THEN now() END,CASE WHEN NOT $5 THEN NULLIF($8,'')::uuid END) RETURNING id::text`,
		workspaceID, string(kind), definition.Singleton, name, enabled, settings, change.ExternalAccount, actor.AgentID).Scan(&id)
	if err != nil {
		return channels.Channel{}, channelWriteError(err)
	}
	channel, err := s.channelByID(ctx, tx, workspaceID, id)
	if err != nil {
		return channels.Channel{}, err
	}
	event, err := adapter.Config().Save(ctx, tx, channel, change)
	if err != nil {
		return channels.Channel{}, err
	}
	detail := change.Detail
	if detail == nil {
		detail = event.Detail
	}
	if err := insertChannelEvent(ctx, tx, channel, "added", actor, detail); err != nil {
		return channels.Channel{}, err
	}
	return channel, nil
}

// UpdateChannel renames a channel, turns it on or off, or changes its configuration or settings.
// Every route that changes a channel, its type's own included, comes here, so each change is
// validated, saved and recorded the same way.
func (s *Store) UpdateChannel(ctx context.Context, workspaceID, id string, input ChannelInput, actor ChannelActor) (channels.Channel, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (channels.Channel, error) {
		return s.UpdateChannelTx(ctx, tx, workspaceID, id, input, actor)
	})
}

// UpdateChannelTx is UpdateChannel inside tx.
func (s *Store) UpdateChannelTx(ctx context.Context, tx pgx.Tx, workspaceID, id string, input ChannelInput, actor ChannelActor) (channels.Channel, error) {
	if _, err := tx.Exec(ctx, `SELECT 1 FROM channels WHERE workspace_id=$1 AND id=$2::uuid FOR UPDATE`, workspaceID, id); err != nil {
		return channels.Channel{}, fmt.Errorf("lock channel: %w", err)
	}
	current, err := s.channelByID(ctx, tx, workspaceID, id)
	if err != nil {
		return channels.Channel{}, err
	}
	if !current.Live() {
		return channels.Channel{}, channels.ErrNotFound
	}
	adapter, err := s.adapterOf(current.Type)
	if err != nil {
		return channels.Channel{}, err
	}
	definition := adapter.Definition()
	if input.Name != nil {
		name, err := validChannelName(*input.Name)
		if err != nil {
			return channels.Channel{}, err
		}
		if name != current.Name {
			if _, err := tx.Exec(ctx, `UPDATE channels SET name=$3,updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspaceID, id, name); err != nil {
				return channels.Channel{}, fmt.Errorf("rename channel: %w", err)
			}
			renamed := current
			renamed.Name = name
			if err := insertChannelEvent(ctx, tx, renamed, "renamed", actor, map[string]any{"from": current.Name, "to": name}); err != nil {
				return channels.Channel{}, err
			}
		}
	}
	if input.Settings != nil {
		settings, err := validSettings(definition, input.Settings)
		if err != nil {
			return channels.Channel{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE channels SET settings=$3,updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspaceID, id, settings); err != nil {
			return channels.Channel{}, fmt.Errorf("save channel settings: %w", err)
		}
		if err := insertChannelEvent(ctx, tx, current, "config_changed", actor, map[string]any{"settings": true}); err != nil {
			return channels.Channel{}, err
		}
	}
	if input.Enabled != nil && *input.Enabled != current.Enabled {
		if _, err := tx.Exec(ctx, `UPDATE channels SET enabled=$3,disabled_at=CASE WHEN $3 THEN NULL ELSE now() END,
			disabled_by_agent_id=CASE WHEN $3 THEN NULL ELSE NULLIF($4,'')::uuid END,updated_at=now() WHERE workspace_id=$1 AND id=$2`,
			workspaceID, id, *input.Enabled, actor.AgentID); err != nil {
			return channels.Channel{}, fmt.Errorf("turn channel on or off: %w", err)
		}
		kind := "disabled"
		if *input.Enabled {
			kind = "enabled"
		}
		if err := insertChannelEvent(ctx, tx, current, kind, actor, nil); err != nil {
			return channels.Channel{}, err
		}
	}
	var change channels.ConfigChange
	if input.Config != nil {
		if change, err = adapter.Config().Validate(ctx, tx, workspaceID, &current, input.Config); err != nil {
			return channels.Channel{}, err
		}
		if change.ExternalAccount != nil {
			if _, err := tx.Exec(ctx, `UPDATE channels SET external_account=NULLIF($3,''),updated_at=now() WHERE workspace_id=$1 AND id=$2`,
				workspaceID, id, *change.ExternalAccount); err != nil {
				return channels.Channel{}, channelWriteError(err)
			}
		}
	}
	updated, err := s.channelByID(ctx, tx, workspaceID, id)
	if err != nil {
		return channels.Channel{}, err
	}
	// Save runs on every change, with no value when the configuration itself did not change, so a
	// type that keeps the channel's state in its own table (for older images) stays in step.
	event, err := adapter.Config().Save(ctx, tx, updated, change)
	if err != nil {
		return channels.Channel{}, err
	}
	if change.Value != nil {
		kind := event.Kind
		if kind == "" {
			kind = "config_changed"
		}
		detail := change.Detail
		if detail == nil {
			detail = event.Detail
		}
		if _, err := tx.Exec(ctx, `UPDATE channels SET updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspaceID, id); err != nil {
			return channels.Channel{}, fmt.Errorf("touch channel: %w", err)
		}
		if err := insertChannelEvent(ctx, tx, updated, kind, actor, detail); err != nil {
			return channels.Channel{}, err
		}
	}
	return s.channelByID(ctx, tx, workspaceID, id)
}

// RemoveChannel removes a channel: its type's rows and its secrets go, the row stays so its
// conversations keep its name. Adding the type again makes a new channel.
func (s *Store) RemoveChannel(ctx context.Context, workspaceID, id string, actor ChannelActor) error {
	after, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (func(context.Context), error) {
		return s.removeChannel(ctx, tx, workspaceID, id, actor)
	})
	if err == nil && after != nil {
		after(ctx)
	}
	return err
}

// RemoveChannelTx is RemoveChannel inside tx; what the type undoes outside (RemoveFollowUp) is the
// caller's to do.
func (s *Store) RemoveChannelTx(ctx context.Context, tx pgx.Tx, workspaceID, id string, actor ChannelActor) error {
	_, err := s.removeChannel(ctx, tx, workspaceID, id, actor)
	return err
}

func (s *Store) removeChannel(ctx context.Context, tx pgx.Tx, workspaceID, id string, actor ChannelActor) (func(context.Context), error) {
	if _, err := tx.Exec(ctx, `SELECT 1 FROM channels WHERE workspace_id=$1 AND id=$2::uuid FOR UPDATE`, workspaceID, id); err != nil {
		return nil, fmt.Errorf("lock channel: %w", err)
	}
	channel, err := s.channelByID(ctx, tx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	if !channel.Live() {
		return nil, channels.ErrNotFound
	}
	// What it holds is opened or discarded first. The row lock above keeps a message from being held
	// between this count and the removal (IngestInbound locks the channel FOR SHARE).
	var held int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM channel_inbound_events WHERE workspace_id=$1 AND channel_id=$2 AND state='held'`,
		workspaceID, id).Scan(&held); err != nil {
		return nil, fmt.Errorf("count held messages: %w", err)
	}
	if held > 0 {
		return nil, &channels.HeldError{Count: held}
	}
	adapter, err := s.adapterOf(channel.Type)
	if err != nil {
		return nil, err
	}
	// The row is marked removed first, so the type's own delete finds nothing left to do for it.
	if _, err := tx.Exec(ctx, `UPDATE channels SET enabled=false,disabled_at=COALESCE(disabled_at,now()),settings='{}',external_account=NULL,
		deleted_at=now(),deleted_by_agent_id=NULLIF($3,'')::uuid,updated_at=now() WHERE workspace_id=$1 AND id=$2`,
		workspaceID, id, actor.AgentID); err != nil {
		return nil, fmt.Errorf("remove channel: %w", err)
	}
	var after func(context.Context)
	if followUp, ok := adapter.Config().(channels.RemoveFollowUp); ok {
		if after, err = followUp.BeforeRemove(ctx, tx, channel); err != nil {
			return nil, err
		}
	}
	if err := adapter.Config().Remove(ctx, tx, channel); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM channel_credentials WHERE workspace_id=$1 AND channel_id=$2`, workspaceID, id); err != nil {
		return nil, fmt.Errorf("delete channel secrets: %w", err)
	}
	return after, insertChannelEvent(ctx, tx, channel, "removed", actor, nil)
}

// channelWriteError names the conflicts of a channel write.
func channelWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		switch pgErr.ConstraintName {
		case "channels_external_account_idx":
			return channels.ErrExternalAccount
		case "channels_singleton_idx":
			return channels.ErrTypeLimit
		}
	}
	return fmt.Errorf("save channel: %w", err)
}

// ChannelPublic is a channel as the API shows it.
type ChannelPublic struct {
	Channel  channels.Channel
	Config   any
	Summary  string
	Status   channels.Status
	Held     int
	Unsent   int
	LastSeen *string
}

// DescribeChannel reads what the API shows of a channel: its configuration, state and counts.
func (s *Store) DescribeChannel(ctx context.Context, tx pgx.Tx, channel channels.Channel) (ChannelPublic, error) {
	public := ChannelPublic{Channel: channel}
	adapter, err := s.adapterOf(channel.Type)
	if err != nil {
		return public, err
	}
	if channel.Live() {
		if public.Config, err = adapter.Config().Public(ctx, tx, channel); err != nil {
			return public, err
		}
		if public.Summary, err = adapter.Config().Summary(ctx, tx, channel); err != nil {
			return public, err
		}
	}
	switch {
	case !channel.Live():
		public.Status = channels.Status{State: channels.StateNotConnected, ReasonCode: "removed"}
	case !channel.Enabled:
		public.Status = channels.Status{State: channels.StateOff}
	default:
		if public.Status, err = adapter.Config().Status(ctx, tx, channel); err != nil {
			return public, err
		}
	}
	if err := tx.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM channel_inbound_events WHERE workspace_id=$1 AND channel_id=$2 AND state='held' AND hold_reason=$3),
		(SELECT count(*) FROM channel_outbox WHERE workspace_id=$1 AND channel_id=$2 AND status='failed' AND last_error_code=$4),
		(SELECT to_char(max(received_at) AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.MS"Z"') FROM channel_inbound_events
			WHERE workspace_id=$1 AND channel_id=$2 AND state IN ('processed','held'))`,
		channel.WorkspaceID, channel.ID, channels.HoldChannelOff, DeliveryCodeChannelOff).Scan(&public.Held, &public.Unsent, &public.LastSeen); err != nil {
		return public, fmt.Errorf("count channel messages: %w", err)
	}
	return public, nil
}

// AddChannelRow adds the live channel of a singleton type with its history entry, for a writer that
// fills the type's own table itself in the same transaction (a new workspace's hosted email). Every
// other channel is added with CreateChannel.
func AddChannelRow(ctx context.Context, tx pgx.Tx, workspaceID string, kind channels.Type, name string, detail map[string]any) (string, error) {
	var channel channels.Channel
	if err := tx.QueryRow(ctx, `INSERT INTO channels(workspace_id,type,singleton,name) VALUES($1,$2,true,$3) RETURNING id::text,workspace_id::text,type,name`,
		workspaceID, string(kind), name).Scan(&channel.ID, &channel.WorkspaceID, &channel.Type, &channel.Name); err != nil {
		return "", channelWriteError(err)
	}
	return channel.ID, insertChannelEvent(ctx, tx, channel, "added", ChannelActor{}, detail)
}

// ListChannels describes the workspace's live channels, oldest first.
func (s *Store) ListChannels(ctx context.Context, workspaceID string) ([]ChannelPublic, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]ChannelPublic, error) {
		rows, err := tx.Query(ctx, `SELECT `+channelRowColumns+` FROM channels WHERE workspace_id=$1 AND deleted_at IS NULL ORDER BY created_at,id`, workspaceID)
		if err != nil {
			return nil, fmt.Errorf("list channels: %w", err)
		}
		var list []channels.Channel
		for rows.Next() {
			channel, err := s.scanChannelRow(rows)
			if err != nil {
				rows.Close()
				return nil, err
			}
			list = append(list, channel)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		described := make([]ChannelPublic, 0, len(list))
		for _, channel := range list {
			if _, ok := s.channels.Adapter(channel.Type); !ok {
				continue // a type this server no longer runs is kept, not shown
			}
			public, err := s.DescribeChannel(ctx, tx, channel)
			if err != nil {
				return nil, err
			}
			described = append(described, public)
		}
		return described, nil
	})
}

// GetChannelPublic describes one live channel of the workspace.
func (s *Store) GetChannelPublic(ctx context.Context, workspaceID, id string) (ChannelPublic, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (ChannelPublic, error) {
		channel, err := s.channelByID(ctx, tx, workspaceID, id)
		if err != nil {
			return ChannelPublic{}, err
		}
		if !channel.Live() {
			return ChannelPublic{}, channels.ErrNotFound
		}
		return s.DescribeChannel(ctx, tx, channel)
	})
}

// ChannelEventPage is a page of channel history and the cursor of the next ("" on the last).
type ChannelEventPage struct {
	Items []ChannelEvent
	Next  string
}

// ListChannelEvents reads the workspace's channel history, newest first: of one channel, or of all.
func (s *Store) ListChannelEvents(ctx context.Context, workspaceID, channelID, cursor string, limit int) (ChannelEventPage, error) {
	var before *time.Time
	var beforeSeq int64
	if cursor != "" {
		at, seq, ok := decodeEventCursor(cursor)
		if !ok {
			return ChannelEventPage{}, ErrBadCursor
		}
		before, beforeSeq = &at, seq
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (ChannelEventPage, error) {
		rows, err := tx.Query(ctx, `SELECT e.id::text,e.channel_id::text,e.channel_type,e.channel_name,e.kind,e.actor_agent_id::text,
				COALESCE(NULLIF(a.display_name,''),a.email),t.label,e.detail,e.created_at,e.seq
			FROM channel_events e LEFT JOIN agents a ON a.workspace_id=e.workspace_id AND a.id=e.actor_agent_id
			LEFT JOIN sessions t ON t.workspace_id=e.workspace_id AND t.id=e.actor_token_id
			WHERE e.workspace_id=$1 AND ($2='' OR e.channel_id::text=$2) AND ($3::timestamptz IS NULL OR (e.created_at,e.seq)<($3,$4))
			ORDER BY e.created_at DESC,e.seq DESC LIMIT $5`, workspaceID, channelID, before, beforeSeq, limit+1)
		if err != nil {
			return ChannelEventPage{}, fmt.Errorf("list channel history: %w", err)
		}
		defer rows.Close()
		var page ChannelEventPage
		var lastSeq int64
		for rows.Next() {
			var event ChannelEvent
			var seq int64
			if err := rows.Scan(&event.ID, &event.ChannelID, &event.ChannelType, &event.ChannelName, &event.Kind, &event.ActorAgentID,
				&event.ActorName, &event.ActorTokenLabel, &event.Detail, &event.CreatedAt, &seq); err != nil {
				return ChannelEventPage{}, err
			}
			event.CreatedAt = event.CreatedAt.UTC()
			if len(page.Items) == limit {
				last := page.Items[len(page.Items)-1]
				page.Next = encodeEventCursor(last.CreatedAt, lastSeq)
				break
			}
			page.Items = append(page.Items, event)
			lastSeq = seq
		}
		return page, rows.Err()
	})
}

func encodeEventCursor(at time.Time, seq int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(at.UTC().Format(time.RFC3339Nano) + "|" + strconv.FormatInt(seq, 10)))
}

func decodeEventCursor(cursor string) (time.Time, int64, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, 0, false
	}
	at, seq, found := strings.Cut(string(raw), "|")
	if !found {
		return time.Time{}, 0, false
	}
	when, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return time.Time{}, 0, false
	}
	number, err := strconv.ParseInt(seq, 10, 64)
	return when, number, err == nil
}

// CountChannelsOfType counts the workspace's live channels of each type.
func (s *Store) CountChannelsOfType(ctx context.Context, workspaceID string) (map[channels.Type]int, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (map[channels.Type]int, error) {
		rows, err := tx.Query(ctx, `SELECT type,count(*) FROM channels WHERE workspace_id=$1 AND deleted_at IS NULL GROUP BY type`, workspaceID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		counts := map[channels.Type]int{}
		for rows.Next() {
			var kind string
			var n int
			if err := rows.Scan(&kind, &n); err != nil {
				return nil, err
			}
			counts[channels.Type(kind)] = n
		}
		return counts, rows.Err()
	})
}
