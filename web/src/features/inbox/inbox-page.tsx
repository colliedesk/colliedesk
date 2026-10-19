import { useEffect, useMemo, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import type { AgentRequest, Conversation, Message, SavedReply, TeamMember, Tag, TimeEntry } from '../../api/schemas';
import type { BulkConversationChanges, UpdateConversationRequest } from '../../api/client';
import { BulkActionBar } from './bulk-action-bar';
import { ConversationList } from './conversation-list';
import { DetailsPanel } from './details-panel';
import { InboxFilters } from './inbox-filters';

import type { InboxQuery } from '../../lib/inbox-query';
import type { AttachmentLimits } from './attachment-picker';
import { ReplyComposer } from './reply-composer';
import { ReplyDraftCard } from './reply-draft-card';
import type { DraftEdit } from './reply-draft-card';
import { suggestedCc } from './suggested-cc';
import { TimelinePanel } from './timeline-panel';
import { messages } from '../../messages';

interface InboxPageProps {
  // The hosted cloud, where the server trusts a Cc line only on mail from the platform forwarder.
  hosted?: boolean;
  conversations?: Conversation[];
  selected?: Conversation;
  timeline?: Message[];
  timeEntries?: TimeEntry[];
  agents?: TeamMember[];
  tags?: Tag[];
  timezone?: string;
  onSelect?: (id: string) => void;
  // The filters of the list, as the address carries them, and a change to them.
  query?: InboxQuery;
  onQuery?: (query: InboxQuery) => void;
  // The saved views menu, which sits in the filter bar (the route loads it lazily with its data).
  viewsMenu?: ReactNode;
  onMessage?: (kind: 'outgoing' | 'note', body: string, files?: File[], cc?: string[]) => void | Promise<unknown>;
  attachmentLimits?: AttachmentLimits;
  savedReplies?: SavedReply[];
  // The names the composer fills into a saved reply: the selected customer and the signed-in agent.
  customerName?: string;
  agentName?: string;
  onManageSavedReplies?: () => void;
  savedRepliesState?: 'loading' | 'failed' | 'ready';
  onUpdate?: (conversationId: string, value: UpdateConversationRequest) => void;
  onAddTag?: (conversationId: string, tagId: string) => void;
  onRemoveTag?: (conversationId: string, tagId: string) => void;
  onCreateTag?: (conversationId: string, name: string) => void;
  page?: number;
  hasNextPage?: boolean;
  onPage?: (page: number) => void;
  onRetryDelivery?: (messageId: string) => void;
  outboundNotice?: string;
  // deliveryNotice explains a refused retry of a failed delivery.
  deliveryNotice?: string;
  filtered?: boolean;
  nothingOpen?: boolean;
  loading?: boolean;
  refreshing?: boolean;
  timelineLoading?: boolean;
  conversationFailed?: boolean;
  onRetryConversation?: () => void;
  failed?: boolean;
  onRetry?: () => void;
  canManage?: boolean;
  onOpenSettings?: () => void;
  currentAgentId?: string;
  // onBulkUpdate resolves when every listed conversation changed, and rejects when none did.
  onBulkUpdate?: (ids: string[], changes: BulkConversationChanges) => Promise<unknown>;
  // Reply drafts AI agents left on the selected conversation, and the conversations that have one.
  replyDrafts?: AgentRequest[];
  draftConversationIds?: ReadonlySet<string>;
  onSendDraft?: (draft: AgentRequest, edit?: DraftEdit) => Promise<unknown>;
  onDiscardDraft?: (draft: AgentRequest) => Promise<unknown>;
}

export function InboxPage({ hosted = false, conversations = [], selected, timeline = [], timeEntries = [], agents = [], tags = [], timezone = 'UTC', attachmentLimits, savedReplies, customerName, agentName, onManageSavedReplies, savedRepliesState, onSelect, query = {}, onQuery, viewsMenu, onMessage, onUpdate, onAddTag, onRemoveTag, onCreateTag, page = 1, hasNextPage = false, onPage, onRetryDelivery, outboundNotice, deliveryNotice, filtered, nothingOpen, loading, refreshing, timelineLoading, conversationFailed, onRetryConversation, failed, onRetry, canManage, onOpenSettings, currentAgentId, onBulkUpdate, replyDrafts = [], draftConversationIds, onSendDraft, onDiscardDraft }: InboxPageProps) {
  // Below the three-pane width the details panel opens as a drawer instead of a column.
  const [detailsOpen, setDetailsOpen] = useState(false);
  // Clearing from the empty list returns to the Open view and puts focus back on the search box.
  const [focusSignal, setFocusSignal] = useState(0);
  const clearFilters = () => { setFocusSignal((current) => current + 1); onQuery?.({}); };
  const detailsToggle = useRef<HTMLButtonElement>(null);
  const listPane = useRef<HTMLElement>(null);
  const [checked, setChecked] = useState<ReadonlySet<string>>(new Set());
  const [bulkBusy, setBulkBusy] = useState(false);
  const [bulkFailed, setBulkFailed] = useState(false);
  const [bulkNotice, setBulkNotice] = useState('');
  const selectAll = useRef<HTMLInputElement | null>(null);
  // The bar unmounts when the selection clears, so focus returns to the list once it has rendered.
  const [refocus, setRefocus] = useState(0);
  useEffect(() => {
    if (refocus > 0) (selectAll.current ?? listPane.current)?.focus();
  }, [refocus]);
  // Only rows still on the page count: a filter, a page change or a refetch can remove ticked rows.
  const checkedIds = conversations.filter((item) => checked.has(item.id)).map((item) => item.id);
  const changeSelection = (next: Set<string>) => { setChecked(next); setBulkFailed(false); setBulkNotice(''); };
  const toggle = (id: string) => {
    const next = new Set(checkedIds);
    if (!next.delete(id)) next.add(id);
    changeSelection(next);
  };
  const toggleAll = (on: boolean) => changeSelection(new Set(on ? conversations.map((item) => item.id) : []));
  const applyBulk = async (changes: BulkConversationChanges) => {
    if (!onBulkUpdate || bulkBusy) return;
    const ids = checkedIds;
    setBulkBusy(true);
    setBulkFailed(false);
    setBulkNotice('');
    try {
      await onBulkUpdate(ids, changes);
      setChecked(new Set());
      setBulkNotice(messages.bulkUpdated(ids.length));
      setRefocus((count) => count + 1);
    } catch {
      setBulkFailed(true);
    } finally {
      setBulkBusy(false);
    }
  };
  const copied = useMemo(() => suggestedCc(timeline, { hosted }), [timeline, hosted]);
  const agentNames = useMemo(() => new Map(agents.map((agent) => [agent.id, agent.display_name || agent.email])), [agents]);
  const closeDetails = () => { setDetailsOpen(false); (detailsToggle.current ?? listPane.current)?.focus(); };
  // The drawer belongs to one conversation: a new selection or none closes it. Without a toggle left
  // to return to, focus goes to the list pane instead of falling back to the page.
  useEffect(() => {
    if (detailsOpen && !selected) listPane.current?.focus();
    setDetailsOpen(false);
    // Only a change of selection closes it, so the effect lists the id and not the open state.
  }, [selected?.id]);
  // Past the three-pane width the drawer button is hidden, so an open drawer would strand focus.
  useEffect(() => {
    const wide = window.matchMedia?.('(min-width: 75rem)');
    if (!wide) return;
    const onChange = (event: { matches: boolean }) => { if (event.matches) setDetailsOpen(false); };
    wide.addEventListener('change', onChange);
    return () => wide.removeEventListener('change', onChange);
  }, []);
  return (
    <main className="inbox-shell" data-details-open={detailsOpen}>
      <section className="list-pane panel" ref={listPane} tabIndex={-1} aria-label={messages.conversationList}>
        <header>
          {/* The filters stay mounted while the bar replaces them, so their state survives. */}
          <div className="list-title" data-collapsed={checkedIds.length > 0 || undefined}><h1>{messages.inbox}</h1><span>{conversations.length}</span></div>
          <div className="list-head" hidden={checkedIds.length > 0}>
            <InboxFilters query={query} onChange={onQuery} tags={tags} agents={agents} timezone={timezone} conversations={conversations} focusSignal={focusSignal}
              views={viewsMenu} />
          </div>
          {checkedIds.length > 0 && <BulkActionBar count={checkedIds.length} agents={agents} tags={tags} currentAgentId={currentAgentId}
            busy={bulkBusy} onApply={(changes) => void applyBulk(changes)} onClear={() => { changeSelection(new Set()); setRefocus((count) => count + 1); }} />}
          {bulkFailed && <p role="alert" className="error-copy bulk-message">{messages.bulkUpdateFailed}</p>}
          {/* A live region without role="status": it is mounted before the first result so screen readers
              announce it, while an idle inbox still exposes no status. */}
          <p aria-live="polite" className="bulk-message">{bulkNotice}</p>
        </header>
        <ConversationList items={conversations} selectedId={selected?.id} timezone={timezone} onSelect={onSelect}
          page={page} hasNextPage={hasNextPage} onPage={onPage} filtered={filtered} nothingOpen={nothingOpen} loading={loading} refreshing={refreshing}
          failed={failed} onRetry={onRetry} onClearFilters={clearFilters} canManage={canManage} onOpenSettings={onOpenSettings}
          checkedIds={checked} onToggle={toggle} onToggleAll={toggleAll} selectAllRef={selectAll} draftIds={draftConversationIds}
          grouped={(query.sort ?? 'updated_desc') === 'updated_desc'} />
      </section>
      <div className="conversation-center panel">
        <TimelinePanel conversation={selected} items={timeline} timeEntries={timeEntries} loading={timelineLoading} loadFailed={conversationFailed} onRetryLoad={onRetryConversation} timezone={timezone} onRetryDelivery={onRetryDelivery} agentNames={agentNames}
          detailsOpen={detailsOpen} detailsToggleRef={detailsToggle} onToggleDetails={() => (detailsOpen ? closeDetails() : setDetailsOpen(true))} />
        {outboundNotice && <p role="status" className="composer-notice">{outboundNotice}</p>}
        {deliveryNotice && <p role="alert" className="composer-notice">{deliveryNotice}</p>}
        {replyDrafts.map((draft) => <ReplyDraftCard key={draft.id} draft={draft} customer={customerName}
          onSend={(edit) => onSendDraft?.(draft, edit) ?? Promise.resolve()} onDiscard={() => onDiscardDraft?.(draft) ?? Promise.resolve()} />)}
        {/* One composer per conversation: its draft and refusal belong to the customer it was written for. */}
        <ReplyComposer key={selected?.id} disabled={!selected} onSubmit={onMessage} attachmentLimits={attachmentLimits} suggestedCc={copied}
          savedReplies={savedReplies} customerName={customerName} agentName={agentName} onManageSavedReplies={onManageSavedReplies} savedRepliesState={savedRepliesState} />
      </div>
      <DetailsPanel conversation={selected} timezone={timezone} agents={agents} tags={tags} onUpdate={onUpdate}
        onAddTag={onAddTag} onRemoveTag={onRemoveTag} onCreateTag={onCreateTag} onClose={detailsOpen ? closeDetails : undefined}
        onShowContactConversations={onQuery ? (contact) => onQuery({ contact, status: 'all' }) : undefined} />
    </main>
  );
}
