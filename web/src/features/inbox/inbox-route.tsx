import { useEffect, useMemo, useRef, useState } from 'react';
import type { KeyboardEvent } from 'react';
import { keepPreviousData, useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate, useParams, useSearch } from '@tanstack/react-router';
import { api } from '../../api/client';
import type { BulkConversationChanges, UpdateConversationRequest } from '../../api/client';
import type { Conversation } from '../../api/schemas';
import { InboxPage } from './inbox-page';
import { contactPart, ContactsPart } from '../contacts/contact-part';
import { lazyPart, PartUnavailable } from '../../ui/lazy-part';
import { apiQuery, inboxQueryFromSearch, isDefaultQuery } from '../../lib/inbox-query';
import type { InboxQuery, InboxQueryKey } from '../../lib/inbox-query';
import { track } from '../../lib/product-analytics';
import { agentSignatureName } from '../../lib/saved-reply-variables';
import { messages } from '../../messages';
import { outboundNotice } from './outbound-notice';
import { retryRefusalText } from './refusal-text';
import { usePendingRequests } from '../requests/use-pending-requests';
import { useConversationPresence } from './use-conversation-presence';
import { useInboxViewing } from './use-inbox-activity';

// errorMessage reads the message of an API error body, when the answer is one.
function errorMessage(result: unknown): string | undefined {
  if (typeof result !== 'object' || result === null || !('error' in result)) return undefined;
  const { error } = result as { error?: { message?: unknown } };
  return typeof error?.message === 'string' ? error.message : undefined;
}

function bulkActionOf(changes: BulkConversationChanges): 'status' | 'assign' | 'add_tag' | 'remove_tag' | undefined {
  if (changes.status) return 'status';
  if ('assignee_id' in changes) return 'assign';
  if (changes.add_tag_ids) return 'add_tag';
  if (changes.remove_tag_ids) return 'remove_tag';
  return undefined;
}


// StartUnavailable stands in for the window when its chunk cannot load: it takes the focus, and Escape or
// Close give it back to the New conversation button, so a broken network never strands the page.
function StartUnavailable({ onClose }: { onClose: () => void }) {
  const root = useRef<HTMLDivElement>(null);
  useEffect(() => root.current?.focus(), []);
  const close = () => { onClose(); document.querySelector<HTMLElement>('[data-opens="new-conversation"]')?.focus(); };
  const keys = (event: KeyboardEvent) => {
    if (event.key === 'Escape') close();
    if (event.key !== 'Tab') return;
    // Two buttons only, so Tab and Shift+Tab both just swap between them (not the full dialogKeys trap).
    const buttons = root.current?.querySelectorAll('button') ?? [];
    event.preventDefault();
    (buttons[document.activeElement === buttons[0] ? 1 : 0] as HTMLElement | undefined)?.focus();
  };
  // Like the window it replaces, a click on the backdrop does not close it; it does not take the focus
  // either, so Escape still reaches this window.
  return <div className="dialog-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget) event.preventDefault(); }}>
    <div className="dialog" role="alertdialog" aria-modal="true" aria-label={messages.newConversation} aria-describedby="start-unavailable" tabIndex={-1} ref={root}
      onKeyDown={keys}>
      <div id="start-unavailable"><PartUnavailable /></div>
      <div className="button-row"><button type="button" onClick={close}>{messages.reportClose}</button></div>
    </div>
  </div>;
}


// The views menu loads on its own chunk, so the first screen does not wait for it; until then its button
// shows, disabled. It asks for its data only when it opens.
const viewsPart = lazyPart(() => import('./saved-views-menu'));

function SavedViewsPart(props: { query: InboxQuery; timezone: string; onApply: (query: InboxQuery) => void }) {
  const part = viewsPart.use();
  if (part.status === 'ready') return <part.module.SavedViewsMenu {...props} />;
  return part.status === 'error' ? <PartUnavailable /> : <span className="filter-menu views-menu"><button type="button" disabled>{messages.savedViewsTitle}</button></span>;
}

// How long a search term stays unchanged before it counts as a finished search, so typing "refund" is one search.
const searchSettleMs = 800;

// Typing changes these on every key, so they replace the address instead of adding to the history.
const typedKeys: InboxQueryKey[] = ['search', 'organization'];

export function InboxRoute() {
  const client = useQueryClient();
  const navigate = useNavigate();
  const params = useParams({ strict: false }) as { conversationId?: string };
  const search = useSearch({ strict: false });
  const filters = useMemo(() => inboxQueryFromSearch(search), [search]);
  const filterKey = apiQuery(filters, 1);
  // A page belongs to the filters it was turned for; new filters start on page 1.
  const [paging, setPaging] = useState({ key: filterKey, page: 1 });
  const page = paging.key === filterKey ? paging.page : 1;
  const setPage = (next: number) => setPaging({ key: filterKey, page: next });
  const [selectedId, setSelectedId] = useState<string>();
  const query = useMemo(() => apiQuery(filters, page), [filters, page]);
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  // Opening a customer or a new conversation then waits for no download; a failure shows where it is used.
  // Once the inbox is idle, so opening a customer or a new conversation waits for no download.
  useEffect(() => { (window.requestIdleCallback ?? ((load: () => void) => setTimeout(load, 1000)))(contactPart.preload); }, []);
  // Always refetch when the inbox opens: mail may have arrived while the agent was in Settings.
  const conversations = useQuery({ queryKey: ['conversations', query], queryFn: ({ signal }) => api.conversations(query, signal), refetchOnMount: 'always',
    // A new search or filter keeps the old rows on screen until its response arrives.
    placeholderData: keepPreviousData });
  // A search is reported once its term settled and its answer arrived: whether it found anything, never the term.
  const searchTerm = filters.search ?? '';
  const searchSettled = searchTerm !== '' && conversations.isSuccess && !conversations.isPlaceholderData;
  const hasResults = (conversations.data?.items.length ?? 0) > 0;
  const reportedSearch = useRef('');
  useEffect(() => {
    if (!searchSettled || reportedSearch.current === searchTerm) return;
    const timer = setTimeout(() => { reportedSearch.current = searchTerm; track('inbox_searched', { has_results: hasResults }); }, searchSettleMs);
    return () => clearTimeout(timer);
  }, [searchSettled, searchTerm, hasResults]);
  // A conversation reached by its address counts once; one picked from the list was counted when picked.
  const reportedOpen = useRef<string | undefined>(undefined);
  useEffect(() => {
    const id = params.conversationId;
    if (!id || selectedId === id || reportedOpen.current === id) return;
    reportedOpen.current = id;
    track('conversation_opened', { source: 'link' });
  }, [params.conversationId, selectedId]);
  const filtered = !isDefaultQuery(filters);
  // An empty Open view is either a new workspace or one that is caught up; one row of All tells which.
  const checkAny = !filtered && page === 1 && conversations.data?.items.length === 0;
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
  // Who else has which conversation open, from the activity poll the app already runs: no request of its own.
  const viewing = useInboxViewing();
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
  const outbound = outboundNotice(emailChannel.data, me.data?.agent?.role === 'admin');
  // A reply that just failed may mean Gmail stopped sending; the channel is read again so the notice above
  // the composer follows without a reload.
  const failedDeliveries = timeline.data?.items.filter((item) => item.delivery?.status === 'failed').map((item) => item.id).join(',') ?? '';
  useEffect(() => {
    if (failedDeliveries) void client.invalidateQueries({ queryKey: ['email-channel'] });
  }, [client, failedDeliveries]);
  const [starting, setStarting] = useState(false);
  const openContact = (contactId: string) => void navigate({ to: '/contacts/$contactId', params: { contactId } });
  const refresh = () => client.invalidateQueries({ queryKey: ['conversations'] });
  const keepOpened = (changed: Conversation) => { client.setQueryData(['conversations', 'one', changed.id], changed); return refresh(); };
  const agentId = me.data?.agent?.id;
  const update = useMutation({ mutationFn: ({ conversationId, changes }: { conversationId: string; changes: UpdateConversationRequest }) => api.updateConversation(conversationId, changes),
    onSuccess: (changed, { changes }) => {
      if (changes.status) track('conversation_status_changed', { to: changes.status });
      if (changes.assignee_id) track('conversation_assigned', { to_self: changes.assignee_id === agentId });
      return keepOpened(changed);
    } });
  const bulkUpdate = useMutation({ mutationFn: ({ ids, changes }: { ids: string[]; changes: BulkConversationChanges }) => api.bulkUpdateConversations(ids, changes),
    onSuccess: (_result, { ids, changes }) => {
      const action = bulkActionOf(changes);
      if (action) track('bulk_action_applied', { action, count: ids.length });
    },
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
  // The others see that this tab has the conversation open and whether a reply is being written; the first
  // announce waits for the requests above, so it never delays opening the ticket.
  const [writingReply, setWritingReply] = useState(false);
  const presence = useConversationPresence(selected?.id, writingReply);
  // A refusal belongs to the conversation it happened in; opening another one hides it.
  const retryError = retryDelivery.isError && retryDelivery.variables?.conversationId === selected?.id ? retryDelivery.error : undefined;
  const applyQuery = (next: InboxQuery) => {
    // Typing a search widens the status to All by itself; that is not a choice of the person.
    const typing = (next.search ?? '') !== (filters.search ?? '');
    // The menus that change the other filters report them (filter-analytics.ts).
    if (!typing && (next.status ?? '') !== (filters.status ?? '')) track('inbox_filtered', { filter: 'status' });
    const replace = typedKeys.some((key) => (next[key] ?? '') !== (filters[key] ?? ''));
    void navigate({ to: params.conversationId ? '/conversations/$conversationId' : '/', params: params.conversationId ? { conversationId: params.conversationId } : {}, search: next, replace });
  };
  return <><InboxPage hosted={config?.billing.enabled ?? false} conversations={conversations.data?.items} selected={selected} timeline={selected ? timeline.data?.items : undefined} timeEntries={selected ? timeEntries.data?.items : undefined}
    agents={agents.data?.items} tags={tags.data?.items} timezone={me.data?.timezone} onboardingGoal={me.data?.onboarding_goal}
    page={page} hasNextPage={(conversations.data?.items.length ?? 0) === (conversations.data?.page_size ?? 50)} onPage={setPage}
    // Opening a conversation keeps the filters in the address, so Back returns to the same list.
    onSelect={(id) => { reportedOpen.current = id; track('conversation_opened', { source: 'list' }); setSelectedId(id); navigate({ to: '/conversations/$conversationId', params: { conversationId: id }, search: filters }); }}
    query={filters}
    viewsMenu={<SavedViewsPart query={filters} timezone={me.data?.timezone ?? 'UTC'} onApply={applyQuery} />}
    onQuery={applyQuery}
    onUpdate={(conversationId, changes) => update.mutate({ conversationId, changes })}
    onAddTag={(conversationId, tagId) => addTag.mutate({ conversationId, tagId })}
    onRemoveTag={(conversationId, tagId) => removeTag.mutate({ conversationId, tagId })}
    onMessage={(kind, body, files, cc) => message.mutateAsync({ kind, body, files, cc })}
    savedReplies={savedReplies.data?.items} customerName={selected?.contact_name} agentName={agentSignatureName(me.data?.agent)}
    savedRepliesState={savedReplies.isPending ? 'loading' : savedReplies.isError ? 'failed' : 'ready'}
    // A new tab, so the draft and the files in this reply box stay where they are.
    onManageSavedReplies={() => { window.open('/settings', '_blank', 'noopener'); }}
    attachmentLimits={limits ? { bytes: limits.outgoing_attachments_bytes, files: limits.outgoing_attachments_files } : undefined}
    outboundNotice={outbound} deliveryNotice={retryError ? retryRefusalText(retryError) : draftNotice} filtered={filtered} nothingOpen={nothingOpen} loading={conversations.isPending || (checkAny && anyConversation.isPending)} refreshing={conversations.isFetching && !conversations.isPending}
    timelineLoading={Boolean(selected) && timeline.isPending}
    conversationFailed={Boolean(activeID && !selected && opened.isError)} onRetryConversation={() => void opened.refetch()}
    failed={conversations.isError} onRetry={() => void conversations.refetch()}
    currentAgentId={me.data?.agent?.id} onBulkUpdate={(ids, changes) => bulkUpdate.mutateAsync({ ids, changes })}
    canManage={me.data?.agent?.role === 'admin'} onOpenSettings={(to = '/settings') => void navigate({ to })}
    onRetryDelivery={(messageId) => selected && retryDelivery.mutate({ conversationId: selected.id, messageId })}
    onCreateTag={(conversationId, name) => createTag.mutate({ conversationId, name })}
    replyDrafts={selectedDrafts} draftConversationIds={draftConversationIds} viewers={viewing}
    openViewers={presence.viewers} onReplyDraft={setWritingReply} onReplyEdited={presence.markEdited}
    onSendDraft={async (draft, edit) => {
      const outcome = await api.approveAgentRequest(draft.id, edit);
      track('agent_request_decided', { decision: 'approved' });
      await afterDraft(draft.conversation_id);
      // Approved but refused by the reply route (for example the trial's limits): say why, the draft is spent.
      setDraftNotice(outcome.request.status === 'failed' ? messages.draftRefused(errorMessage(outcome.result)) : undefined);
    }}
    onDiscardDraft={async (draft) => { await api.rejectAgentRequest(draft.id); track('agent_request_decided', { decision: 'rejected' }); await afterDraft(draft.conversation_id); }}
    contactMatches={searchTerm.trim() && <ContactsPart render={(chunk) => <chunk.ContactMatches search={searchTerm} onOpenContact={openContact} />} />}
    onOpenContact={openContact}
    onNewConversation={() => setStarting(true)} />
    {starting && <ContactsPart failed={<StartUnavailable onClose={() => setStarting(false)} />}
      render={(chunk) => <chunk.StartConversationWindow onClose={() => setStarting(false)} onStarted={setSelectedId} />} />}
  </>;
}
