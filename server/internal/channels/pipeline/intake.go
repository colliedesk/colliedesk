package pipeline

import (
	"context"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// ApplyGathering applies a decision of a gathering of intake that opens a message: its held events,
// parsed again, are replayed as the one message the batch is, into a new conversation (apply.ForceNew)
// or conversationID. It fails with store.ErrGatheringDecided when the gathering was decided meanwhile
// or another worker holds its events.
func (p *Pipeline) ApplyGathering(ctx context.Context, workspaceID, gatheringID, conversationID string, apply store.IntakeApply) (channels.IngestResult, error) {
	channelID, events, err := p.store.GatheringEvents(ctx, workspaceID, gatheringID)
	if err != nil {
		return channels.IngestResult{}, err
	}
	if len(events) == 0 {
		return channels.IngestResult{}, store.ErrGatheringDecided
	}
	channel, err := p.store.GetChannel(ctx, workspaceID, channelID)
	if err != nil {
		return channels.IngestResult{}, err
	}
	if !channel.Live() {
		return channels.IngestResult{}, channels.ErrNotFound
	}
	items, err := p.parseEvents(ctx, channel, events)
	if err != nil {
		return channels.IngestResult{}, err
	}
	ids := make([]string, len(events))
	for i, event := range events {
		ids[i] = event.ID
	}
	apply.GatheringID = gatheringID
	result, err := p.store.IngestInbound(ctx, store.IngestRequest{WorkspaceID: workspaceID, ChannelID: channelID, Inbound: merge(items), EventIDs: ids,
		Now: p.now(), Replay: true, IntoConversation: conversationID, DecidedBy: apply.Actor.AgentID, Intake: &apply})
	if err == nil && result.Status == store.ResultSkipped {
		return result, store.ErrGatheringDecided
	}
	return result, err
}
