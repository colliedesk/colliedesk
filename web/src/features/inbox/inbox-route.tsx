import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate, useParams } from '@tanstack/react-router';
import { api } from '../../api/client';
import type { UpdateConversationRequest } from '../../api/client';
import { InboxPage } from './inbox-page';
import { messages } from '../../messages';

export function InboxRoute() {
  const client = useQueryClient();
  const navigate = useNavigate();
  const params = useParams({ strict: false }) as { conversationId?: string };
  const [filters, setFilters] = useState<Record<string, string>>({});
  const [page, setPage] = useState(1);
  const [selectedId, setSelectedId] = useState<string>();
  const query = useMemo(() => {
    const values = new URLSearchParams(Object.entries(filters).filter(([, value]) => value));
    values.set('page', String(page));
    values.set('page_size', '50');
    return values.toString();
  }, [filters, page]);
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  // Always refetch when the inbox opens: mail may have arrived while the agent was in Settings.
  const conversations = useQuery({ queryKey: ['conversations', query], queryFn: () => api.conversations(query), refetchOnMount: 'always' });
  const agents = useQuery({ queryKey: ['agents'], queryFn: api.agents });
  const tags = useQuery({ queryKey: ['tags'], queryFn: api.tags });
  const emailChannel = useQuery({ queryKey: ['email-channel'], queryFn: api.emailChannel });
  const outbound = emailChannel.data;
  const outboundNotice = outbound?.channel && outbound.sending_from === null
    ? (outbound.smtp.configured ? messages.outboundNeedsVerification : messages.outboundNotConfigured) : undefined;
  const activeID = selectedId ?? params.conversationId;
  const selected = conversations.data?.items.find((item) => item.id === activeID) ?? conversations.data?.items[0];
  const timeline = useQuery({ queryKey: ['messages', selected?.id], queryFn: () => api.messages(selected!.id), enabled: Boolean(selected),
    // Poll while a reply waits in the email outbox so its delivery status updates without a reload.
    refetchInterval: (query) => (query.state.data?.items.some((item) => item.delivery?.status === 'queued') ? 5000 : false) });
  const timeEntries = useQuery({ queryKey: ['time-entries', selected?.id], queryFn: () => api.timeEntries(selected!.id), enabled: Boolean(selected) });
  const refresh = () => client.invalidateQueries({ queryKey: ['conversations'] });
  const update = useMutation({ mutationFn: ({ conversationId, changes }: { conversationId: string; changes: UpdateConversationRequest }) => api.updateConversation(conversationId, changes), onSuccess: refresh });
  const addTag = useMutation({ mutationFn: ({ conversationId, tagId }: { conversationId: string; tagId: string }) => api.addConversationTag(conversationId, tagId), onSuccess: refresh });
  const removeTag = useMutation({ mutationFn: ({ conversationId, tagId }: { conversationId: string; tagId: string }) => api.removeConversationTag(conversationId, tagId), onSuccess: refresh });
  const createTag = useMutation({ mutationFn: ({ name }: { conversationId: string; name: string }) => api.createTag(name), onSuccess: (tag, variables) => {
    void client.invalidateQueries({ queryKey: ['tags'] });
    addTag.mutate({ conversationId: variables.conversationId, tagId: tag.id });
  } });
  const message = useMutation({ mutationFn: ({ kind, body }: { kind: 'outgoing' | 'note'; body: string }) => api.createMessage(selected!.id, kind, body),
    onSuccess: () => client.invalidateQueries({ queryKey: ['messages', selected?.id] }) });
  const retryDelivery = useMutation({ mutationFn: ({ conversationId, messageId }: { conversationId: string; messageId: string }) => api.retryDelivery(conversationId, messageId),
    onSuccess: (retried) => client.invalidateQueries({ queryKey: ['messages', retried.conversation_id] }) });
  return <InboxPage conversations={conversations.data?.items} selected={selected} timeline={timeline.data?.items} timeEntries={timeEntries.data?.items}
    agents={agents.data?.items} tags={tags.data?.items} timezone={me.data?.timezone}
    page={page} hasNextPage={(conversations.data?.items.length ?? 0) === (conversations.data?.page_size ?? 50)} onPage={setPage}
    onSelect={(id) => { setSelectedId(id); navigate({ to: '/conversations/$conversationId', params: { conversationId: id } }); }}
    onFilters={(values) => { setFilters(values); setPage(1); }} onUpdate={(conversationId, changes) => update.mutate({ conversationId, changes })}
    onAddTag={(conversationId, tagId) => addTag.mutate({ conversationId, tagId })}
    onRemoveTag={(conversationId, tagId) => removeTag.mutate({ conversationId, tagId })}
    onMessage={(kind, body) => message.mutate({ kind, body })}
    outboundNotice={outboundNotice} filtered={Object.values(filters).some(Boolean)} loading={conversations.isPending}
    failed={conversations.isError} onRetry={() => void conversations.refetch()}
    canManage={me.data?.agent?.role === 'admin'} onOpenSettings={() => void navigate({ to: '/settings' })}
    onRetryDelivery={(messageId) => selected && retryDelivery.mutate({ conversationId: selected.id, messageId })}
    onCreateTag={(conversationId, name) => createTag.mutate({ conversationId, name })} />;
}
