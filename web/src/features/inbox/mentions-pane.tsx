import { useEffect, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { QueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { api } from '../../api/client';
import type { Mention, Message } from '../../api/schemas';
import { MentionsList } from './mentions-list';
import type { MentionShow } from './mentions-list';

type MentionPage = Awaited<ReturnType<typeof api.mentions>>;

// markedRead writes what the server answered into every Mentions list on screen: the rows it named read (those of a
// conversation, or all), and the unread count it gave. The rows stay where they are until the list is read again.
function markedRead(client: QueryClient, which: (item: Mention) => boolean, unread: number) {
  const now = new Date().toISOString();
  client.setQueriesData<MentionPage>({ queryKey: ['mentions'] }, (page) => page && {
    ...page, unread, items: page.items.map((item) => (!item.read_at && which(item) ? { ...item, read_at: now } : item)),
  });
}

// useMarkMentionsRead marks the mentions of the person on the conversation open read, once its notes are on screen:
// opening a note that asked them is reading it, from the Mentions list or from anywhere else.
export function useMarkMentionsRead(conversationId: string | undefined, timeline: Message[] | undefined, me: string | undefined) {
  const client = useQueryClient();
  const asked = Boolean(conversationId && me && timeline?.some((item) => item.mentions?.some((mention) => mention.agent_id === me && !mention.read_at)));
  const done = useRef<string | undefined>(undefined);
  useEffect(() => {
    if (!asked || !conversationId || done.current === conversationId) return;
    done.current = conversationId;
    api.markMentionsRead({ conversation_id: conversationId }).then((result) => {
      markedRead(client, (item) => item.conversation_id === conversationId, result.unread);
      void client.invalidateQueries({ queryKey: ['messages', conversationId] });
    }, () => { done.current = undefined; });
  }, [asked, conversationId, client]);
}

// MentionsPane is the Mentions list in the inbox's list column: a row opens its conversation beside it, at the note.
export function MentionsPane({ timezone, selectedConversation }: { timezone: string; selectedConversation?: string }) {
  const client = useQueryClient();
  const navigate = useNavigate();
  const [show, setShow] = useState<MentionShow>('unread');
  const list = useQuery({ queryKey: ['mentions', show], queryFn: () => api.mentions(show) });
  const markAll = useMutation({ mutationFn: () => api.markMentionsRead({ all: true }), onSuccess: (result) => markedRead(client, () => true, result.unread) });
  const open = (item: Mention) => void navigate({ to: '/mentions/$conversationId', params: { conversationId: item.conversation_id }, hash: `message-${item.message_id}` });
  return <MentionsList items={list.data?.items ?? []} unread={list.data?.unread ?? 0} show={show} onShow={setShow} onOpen={open}
    onMarkAllRead={() => markAll.mutate()} marking={markAll.isPending} timezone={timezone} selectedConversation={selectedConversation}
    loading={list.isPending} failed={list.isError} onRetry={() => void list.refetch()} />;
}
