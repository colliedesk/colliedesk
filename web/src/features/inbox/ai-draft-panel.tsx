import { useEffect } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { aiDraftApi } from './ai-draft-api';
import { AIDraftCard } from './ai-draft-card';
import { aiMessages } from './ai-draft-messages';

interface AIDraftPanelProps {
  conversationId: string;
  // The newest message on screen: a new one (the customer wrote again, a reply went out) reads the draft again.
  latestMessageId?: string;
  timezone: string;
}

// Statuses whose draft has nothing left for a person to do here.
const done = new Set(['used', 'superseded', 'auto_sent', 'cancelled', 'discarded']);

// AIDraftPanel reads the conversation's newest AI draft on its own: the conversation is on screen
// before it, and a draft still being written is read again every two seconds until it is there.
export function AIDraftPanel({ conversationId, latestMessageId, timezone }: AIDraftPanelProps) {
  const client = useQueryClient();
  const queryKey = ['ai-draft', conversationId];
  const state = useQuery({
    queryKey, queryFn: ({ signal }) => aiDraftApi.get(conversationId, signal),
    refetchInterval: (query) => {
      const status = query.state.data?.draft?.status;
      return status === 'queued' ? 2000 : status === 'scheduled' ? 15000 : false;
    },
  });
  useEffect(() => { void client.invalidateQueries({ queryKey }); }, [latestMessageId]);
  const refresh = async (conversationChanged = false) => {
    await client.invalidateQueries({ queryKey });
    if (conversationChanged) {
      await client.invalidateQueries({ queryKey: ['messages', conversationId] });
      await client.invalidateQueries({ queryKey: ['conversations'] });
    }
  };
  const data = state.data;
  if (!data?.available || !data.drafts_enabled) return null;
  const draft = data.draft;
  if (!draft || done.has(draft.status)) {
    return <p className="ai-draft-ask"><button type="button" className="link-action"
      onClick={() => void aiDraftApi.requestNew(conversationId).then(() => refresh())}>{aiMessages.ask}</button></p>;
  }
  return <AIDraftCard draft={draft} timezone={timezone}
    onSend={(body) => aiDraftApi.send(conversationId, draft.id, body).then(() => refresh(true))}
    onDiscard={() => aiDraftApi.discard(conversationId, draft.id).then(() => refresh())}
    onApplyTriage={() => aiDraftApi.applyTriage(conversationId, draft.id).then(() => refresh(true))}
    onCancelAutoAnswer={() => aiDraftApi.cancelAutoAnswer(conversationId, draft.id).then(() => refresh())}
    onRegenerate={() => aiDraftApi.requestNew(conversationId).then(() => refresh())} />;
}
