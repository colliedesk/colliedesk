// Package pipeline runs what channels receive through their adapters into the store: the one path
// by which a channel's message becomes a conversation. Sync types are processed in the request,
// Async ones are journaled first and processed by Run. It knows no channel type.
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

const (
	maxAttempts  = 8
	batchSize    = 50
	maxRawObject = 32 << 20
)

// Pipeline is the ingest pipeline over a store that has the channel registry.
type Pipeline struct {
	store  *store.Store
	now    func() time.Time
	logger *slog.Logger
}

// New returns the pipeline of st. now defaults to the wall clock in UTC.
func New(st *store.Store, now func() time.Time) *Pipeline {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Pipeline{store: st, now: now, logger: slog.Default()}
}

func (p *Pipeline) adapter(kind channels.Type) (channels.Adapter, error) {
	adapter, ok := p.store.Channels().Adapter(kind)
	if !ok {
		return nil, channels.ErrUnknownType
	}
	return adapter, nil
}

// Receive runs raw events of a channel through its type. A Sync type is parsed and stored now and
// its error returned (the sender retries); an Async type's events are journaled and answered
// accepted, or duplicate for an event the journal has.
func (p *Pipeline) Receive(ctx context.Context, channel channels.Channel, raws []channels.Raw) ([]channels.IngestResult, error) {
	adapter, err := p.adapter(channel.Type)
	if err != nil {
		return nil, err
	}
	var results []channels.IngestResult
	now := p.now()
	for i, raw := range raws {
		if raw.ReceivedAt.IsZero() {
			// Events of one request keep their order in the journal.
			raw.ReceivedAt = now.Add(time.Duration(i) * time.Microsecond)
		}
		if adapter.Inbound().Mode() == channels.Async {
			id, duplicate, err := p.store.RecordInboundEvent(ctx, channel.WorkspaceID, channel.ID, raw, raw.ReceivedAt)
			if err != nil {
				return results, err
			}
			status := channels.ResultAccepted
			if duplicate {
				status = channels.ResultDuplicate
			}
			results = append(results, channels.IngestResult{Status: status, EventID: id})
			continue
		}
		items, err := adapter.Inbound().Parse(ctx, channel, raw)
		if err != nil {
			return results, err
		}
		for _, item := range items {
			result, err := p.store.IngestInbound(ctx, store.IngestRequest{WorkspaceID: channel.WorkspaceID, ChannelID: channel.ID,
				Raw: raw, Inbound: item, Now: raw.ReceivedAt})
			if err != nil {
				return results, err
			}
			results = append(results, result)
		}
	}
	return results, nil
}

// ProcessDue processes the journal events that are due once: received and failed events, and
// collected groups whose time came. It returns how many sets it processed.
func (p *Pipeline) ProcessDue(ctx context.Context) (int, error) {
	work, err := p.store.DueInbound(ctx, p.now(), batchSize, maxAttempts)
	if err != nil {
		return 0, err
	}
	var errs []error
	for _, item := range work {
		if err := p.process(ctx, item); err != nil {
			errs = append(errs, err)
			if markErr := p.store.MarkInboundFailed(ctx, item.WorkspaceID, item.EventIDs, err.Error()); markErr != nil {
				errs = append(errs, markErr)
			}
		}
	}
	return len(work), errors.Join(errs...)
}

// Run processes the journal every interval until the context ends.
func (p *Pipeline) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := p.ProcessDue(ctx); err != nil && ctx.Err() == nil {
			p.logger.Error("channel journal", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (p *Pipeline) process(ctx context.Context, work store.InboundWork) error {
	channel, err := p.store.GetChannel(ctx, work.WorkspaceID, work.ChannelID)
	if err != nil {
		return err
	}
	events, err := p.store.InboundEvents(ctx, work.WorkspaceID, work.ChannelID, work.EventIDs)
	if err != nil {
		return err
	}
	items, err := p.parseEvents(ctx, channel, events)
	if err != nil {
		return err
	}
	if work.Flush {
		_, err := p.store.IngestInbound(ctx, store.IngestRequest{WorkspaceID: channel.WorkspaceID, ChannelID: channel.ID,
			Inbound: merge(items), EventIDs: work.EventIDs, Now: p.now(), Flushing: true})
		return err
	}
	for i, item := range items {
		if _, err := p.store.IngestInbound(ctx, store.IngestRequest{WorkspaceID: channel.WorkspaceID, ChannelID: channel.ID,
			Raw: channels.Raw{JSON: events[i].Raw}, Inbound: item, EventIDs: []string{events[i].ID}, Now: p.now()}); err != nil {
			return err
		}
	}
	return nil
}

// parseEvents parses journal events, one message each (the journal keeps one event per message).
func (p *Pipeline) parseEvents(ctx context.Context, channel channels.Channel, events []store.InboundEvent) ([]channels.Inbound, error) {
	adapter, err := p.adapter(channel.Type)
	if err != nil {
		return nil, err
	}
	items := make([]channels.Inbound, 0, len(events))
	for _, event := range events {
		raw := channels.Raw{ExternalEventID: event.ExternalEventID, JSON: event.Raw, ReceivedAt: event.ReceivedAt}
		if event.ObjectKey != "" {
			if raw.Body, err = p.readObject(ctx, event.ObjectKey); err != nil {
				return nil, err
			}
		}
		parsed, err := adapter.Inbound().Parse(ctx, channel, raw)
		if err != nil {
			return nil, fmt.Errorf("parse channel event %s: %w", event.ID, err)
		}
		if len(parsed) != 1 {
			return nil, fmt.Errorf("channel event %s parsed into %d messages; a journal event is one message", event.ID, len(parsed))
		}
		items = append(items, parsed[0])
	}
	return items, nil
}

func (p *Pipeline) readObject(ctx context.Context, key string) ([]byte, error) {
	storage := p.store.Objects()
	if storage == nil {
		return nil, errors.New("attachment storage is not configured; a stored channel event cannot be read")
	}
	body, err := storage.Open(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("read channel event: %w", err)
	}
	defer body.Close()
	return io.ReadAll(io.LimitReader(body, maxRawObject))
}

// merge makes one message of collected ones: the first one's sender, thread and id, every text in
// order and every file.
func merge(items []channels.Inbound) channels.Inbound {
	merged := items[0]
	texts := make([]string, 0, len(items))
	merged.Files = nil
	for _, item := range items {
		if strings.TrimSpace(item.Text) != "" {
			texts = append(texts, item.Text)
		}
		merged.Files = append(merged.Files, item.Files...)
		if item.Intent.Kind == channels.IntentCommand {
			merged.Intent = item.Intent
		}
	}
	merged.Text = strings.Join(texts, "\n")
	merged.Aggregate = nil
	return merged
}

// ReplayHeld opens events a channel held (all, or those of ids) as conversations, or adds them to
// conversationID, even while the channel is still off. It returns the results in order.
func (p *Pipeline) ReplayHeld(ctx context.Context, workspaceID, channelID string, ids []string, conversationID string, actor store.ChannelActor) ([]channels.IngestResult, error) {
	channel, err := p.store.GetChannel(ctx, workspaceID, channelID)
	if err != nil {
		return nil, err
	}
	if !channel.Live() {
		return nil, channels.ErrNotFound
	}
	events, err := p.store.HeldEvents(ctx, workspaceID, channelID, ids)
	if err != nil {
		return nil, err
	}
	if len(ids) > 0 && len(events) != len(ids) {
		return nil, channels.ErrNotFound
	}
	items, err := p.parseEvents(ctx, channel, events)
	if err != nil {
		return nil, err
	}
	results := make([]channels.IngestResult, 0, len(items))
	for i, item := range items {
		item.Aggregate = nil
		result, err := p.store.IngestInbound(ctx, store.IngestRequest{WorkspaceID: workspaceID, ChannelID: channelID,
			Raw: channels.Raw{JSON: events[i].Raw}, Inbound: item, EventIDs: []string{events[i].ID}, Now: p.now(), Replay: true,
			IntoConversation: conversationID, DecidedBy: actor.AgentID})
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}
	if len(results) > 0 {
		if err := p.store.RecordHeldReplay(ctx, workspaceID, channelID, len(results), conversationID, actor); err != nil {
			return results, err
		}
	}
	return results, nil
}
