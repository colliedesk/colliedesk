import { useMemo, useState } from 'react';
import { keepPreviousData, useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate, useParams } from '@tanstack/react-router';
import { api } from '../../api/client';
import type { BulkConversationChanges, UpdateConversationRequest } from '../../api/client';
import type { Conversation } from '../../api/schemas';
import { InboxPage } from './inbox-page';
import { defaultInboxFilters } from './inbox-filters';
import { agentSignatureName } from '../../lib/saved-reply-variables';
import { messages } from '../../messages';
import { retryRefusalText } from './refusal-text';
import { usePendingRequests } from '../requests/use-pending-requests';

// errorMessage reads the message of an API error body, when the answer is one.
function errorMessage(result: unknown): string | undefined {
  if (typeof result !== 'object' || result === null || !('error' in result)) return undefined;
  const { error } = result as { error?: { message?: unknown } };
  return typeof error?.message === 'string' ? error.message : undefined;
}

export function InboxRoute() {
  const client = useQueryClient();
  const navigate = useNavigate();
  const params = useParams({ strict: false }) as { conversationId?: string };
  const [filters, setFilters] = useState<Record<string, string>>({ ...defaultInboxFilters });
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
  const conversations = useQuery({ queryKey: ['conversations', query], queryFn: () => api.conversations(query), refetchOnMount: 'always',
    // A new search or filter keeps the old rows on screen until its response arrives.
    placeholderData: keepPreviousData });
  const narrowed = Boolean(filters.search || filters.assignee || filters.tag);
  const filtered = narrowed || !['', defaultInboxFilters.status].includes(filters.status ?? '');
  // An empty Open view is either a new workspace or one that is caught up; one row of All tells which.
  const checkAny = !narrowed && filters.status === defaultInboxFilters.status && page === 1 && conversations.data?.items.length === 0;
  const anyConversation = useQuery({ queryKey: ['conversations', 'any'], queryFn: () => api.conversations('page=1&page_size=1'), enabled: checkAny });
  // A failed check is not proof of an empty workspace, so it never shows the new-workspace steps.
  const nothingOpen = checkAny && (anyConversation.isError || (anyConversation.data?.items.length ?? 0) > 0);
  const activeID = selectedId ?? params.conversationId;
  const listed = conversations.data?.items.find((item) => item.id === activeID);
  // A conversation the agent opened stays on screen after it leaves the list, e.g. once resolved. It is
  // fetched alongside the list for a deep link, and seeded from every change so the panes never swap.
  const opened = useQuery({ queryKey: ['conversations', 'one', activeID], queryFn: () => api.conversation(activeID!),
    enabled: Boolean(activeID && !listed) });
  // Header, composer and timeline all belong to one ticket. With an address in the URL that is the ticket the
  // address names, once its row or its detail has loaded; until then nothing is shown for it, never the first
  // row of the list, whose composer would send a reply to the wrong customer.
  const selected = listed ?? (opened.data?.id === activeID ? opened.data : undefined) ?? (activeID ? undefined : conversations.data?.items[0]);
  // The messages and time entries need only the id. A ticket opened by its address knows it from the URL, so
  // they go out together with the list and the detail instead of after one of them. Without an address the id
  // is the first row of the list, which is the one thing that has to be waited for.
  const openID = activeID ?? selected?.id;
  const timeline = useQuery({ queryKey: ['messages', openID], queryFn: () => api.messages(openID!), enabled: Boolean(openID),
    // Poll while a reply waits in the email outbox so its delivery status updates without a reload.
    refetchInterval: (query) => (query.state.data?.items.some((item) => item.delivery?.status === 'queued') ? 5000 : false) });
  // Reply drafts AI agents left: the list marks their conversations, the open one shows them above the composer.
  const pending = usePendingRequests().data?.items;
  const drafts = useMemo(() => (pending ?? []).filter((item) => item.kind === 'reply_draft' && item.conversation_id), [pending]);
  const draftConversationIds = useMemo(() => new Set(drafts.map((item) => item.conversation_id!)), [drafts]);
  // The poll carries no body, so the open conversation reads its drafts in full.
  const draftDetails = useQueries({ queries: drafts.filter((item) => item.conversation_id === selected?.id)
    .map((item) => ({ queryKey: ['agent-requests', 'one', item.id], queryFn: () => api.agentRequest(item.id) })) });
  const selectedDrafts = draftDetails.flatMap((query) => (query.data?.status === 'pending' ? [query.data] : []));
  const [draftNotice, setDraftNotice] = useState<string>();
  const afterDraft = (conversationId: string | null) => {
    void client.invalidateQueries({ queryKey: ['agent-requests'] });
    if (conversationId) void client.invalidateQueries({ queryKey: ['messages', conversationId] });
    return refresh();
  };
  const timeEntries = useQuery({ queryKey: ['time-entries', openID], queryFn: () => api.timeEntries(openID!), enabled: Boolean(openID) });
  // The side lists come after everything the open ticket needs: a browser allows six HTTP/1.1 connections per
  // host, and a request that waits for one is a request the person waits for.
  const agents = useQuery({ queryKey: ['agents'], queryFn: api.agents });
  const tags = useQuery({ queryKey: ['tags'], queryFn: api.tags });
  // Fetched once and shared with Settings, so the picker opens from memory.
  const savedReplies = useQuery({ queryKey: ['saved-replies'], queryFn: api.savedReplies });
  const emailChannel = useQuery({ queryKey: ['email-channel'], queryFn: api.emailChannel });
  // Shared with the app shell, so the limits cost no extra request.
  const config = useQuery({ queryKey: ['public-config'], queryFn: api.config }).data;
  const limits = config?.limits;
  const outbound = emailChannel.data;
  const outboundNotice = outbound?.channel && outbound.sending_from === null
    ? (outbound.smtp.configured ? messages.outboundNeedsVerification : messages.outboundNotConfigured) : undefined;
  const refresh = () => client.invalidateQueries({ queryKey: ['conversations'] });
  const keepOpened = (changed: Conversation) => { client.setQueryData(['conversations', 'one', changed.id], changed); return refresh(); };
  const update = useMutation({ mutationFn: ({ conversationId, changes }: { conversationId: string; changes: UpdateConversationRequest }) => api.updateConversation(conversationId, changes), onSuccess: keepOpened });
  const bulkUpdate = useMutation({ mutationFn: ({ ids, changes }: { ids: string[]; changes: BulkConversationChanges }) => api.bulkUpdateConversations(ids, changes),
    // A refusal usually means a row is gone or changed under the agent; reload so the list shows it.
    onSettled: refresh });
  const addTag = useMutation({ mutationFn: ({ conversationId, tagId }: { conversationId: string; tagId: string }) => api.addConversationTag(conversationId, tagId), onSuccess: keepOpened });
  const removeTag = useMutation({ mutationFn: ({ conversationId, tagId }: { conversationId: string; tagId: string }) => api.removeConversationTag(conversationId, tagId), onSuccess: keepOpened });
  const createTag = useMutation({ mutationFn: ({ name }: { conversationId: string; name: string }) => api.createTag(name), onSuccess: (tag, variables) => {
    void client.invalidateQueries({ queryKey: ['tags'] });
    addTag.mutate({ conversationId: variables.conversationId, tagId: tag.id });
  } });
  const message = useMutation({ mutationFn: ({ kind, body, files, cc }: { kind: 'outgoing' | 'note'; body: string; files?: File[]; cc?: string[] }) => api.createMessage(selected!.id, kind, body, files, cc),
    onSuccess: () => client.invalidateQueries({ queryKey: ['messages', selected?.id] }) });
  const retryDelivery = useMutation({ mutationFn: ({ conversationId, messageId }: { conversationId: string; messageId: string }) => api.retryDelivery(conversationId, messageId),
    onSuccess: (retried) => client.invalidateQueries({ queryKey: ['messages', retried.conversation_id] }) });
  // A refusal belongs to the conversation it happened in; opening another one hides it.
  const retryError = retryDelivery.isError && retryDelivery.variables?.conversationId === selected?.id ? retryDelivery.error : undefined;
  return <InboxPage hosted={config?.billing.enabled ?? false} conversations={conversations.data?.items} selected={selected} timeline={selected ? timeline.data?.items : undefined} timeEntries={selected ? timeEntries.data?.items : undefined}
    agents={agents.data?.items} tags={tags.data?.items} timezone={me.data?.timezone}
    page={page} hasNextPage={(conversations.data?.items.length ?? 0) === (conversations.data?.page_size ?? 50)} onPage={setPage}
    onSelect={(id) => { setSelectedId(id); navigate({ to: '/conversations/$conversationId', params: { conversationId: id } }); }}
    onFilters={(values) => { setFilters(values); setPage(1); }} onUpdate={(conversationId, changes) => update.mutate({ conversationId, changes })}
    onAddTag={(conversationId, tagId) => addTag.mutate({ conversationId, tagId })}
    onRemoveTag={(conversationId, tagId) => removeTag.mutate({ conversationId, tagId })}
    onMessage={(kind, body, files, cc) => message.mutateAsync({ kind, body, files, cc })}
    savedReplies={savedReplies.data?.items} customerName={selected?.contact_name} agentName={agentSignatureName(me.data?.agent)}
    savedRepliesState={savedReplies.isPending ? 'loading' : savedReplies.isError ? 'failed' : 'ready'}
    // A new tab, so the draft and the files in this reply box stay where they are.
    onManageSavedReplies={() => { window.open('/settings', '_blank', 'noopener'); }}
    attachmentLimits={limits ? { bytes: limits.outgoing_attachments_bytes, files: limits.outgoing_attachments_files } : undefined}
    outboundNotice={outboundNotice} deliveryNotice={retryError ? retryRefusalText(retryError) : draftNotice} filtered={filtered} nothingOpen={nothingOpen} loading={conversations.isPending || (checkAny && anyConversation.isPending)} refreshing={conversations.isFetching && !conversations.isPending}
    timelineLoading={Boolean(selected) && timeline.isPending}
    conversationFailed={Boolean(activeID && !selected && opened.isError)} onRetryConversation={() => void opened.refetch()}
    failed={conversations.isError} onRetry={() => void conversations.refetch()}
    currentAgentId={me.data?.agent?.id} onBulkUpdate={(ids, changes) => bulkUpdate.mutateAsync({ ids, changes })}
    canManage={me.data?.agent?.role === 'admin'} onOpenSettings={() => void navigate({ to: '/settings' })}
    onRetryDelivery={(messageId) => selected && retryDelivery.mutate({ conversationId: selected.id, messageId })}
    onCreateTag={(conversationId, name) => createTag.mutate({ conversationId, name })}
    replyDrafts={selectedDrafts} draftConversationIds={draftConversationIds}
    onSendDraft={async (draft, edit) => {
      const outcome = await api.approveAgentRequest(draft.id, edit);
      await afterDraft(draft.conversation_id);
      // Approved but refused by the reply route (for example the trial's limits): say why, the draft is spent.
      setDraftNotice(outcome.request.status === 'failed' ? messages.draftRefused(errorMessage(outcome.result)) : undefined);
    }}
    onDiscardDraft={async (draft) => { await api.rejectAgentRequest(draft.id); await afterDraft(draft.conversation_id); }} />;
}
