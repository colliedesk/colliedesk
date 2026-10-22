import { useEffect, useMemo, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import type { AgentRequest, Conversation, ConversationViewer, Message, OnboardingGoal, SavedReply, TeamMember, Tag, TimeEntry } from '../../api/schemas';
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
  // seen is the newest reply number on screen when the reply was started.
  onMessage?: (kind: 'outgoing' | 'note', body: string, files?: File[], cc?: string[], seen?: number) => void | Promise<unknown>;
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
  // viewers are the others who have a conversation open now, by conversation id.
  viewers?: ReadonlyMap<string, ConversationViewer[]>;
  // The others who have the open conversation in front of them now.
  openViewers?: ConversationViewer[];
  // The composer reports a reply being written (never its text), for the others to see.
  onReplyDraft?: (writing: boolean) => void;
  onReplyEdited?: () => void;
  onSendDraft?: (draft: AgentRequest, edit?: DraftEdit) => Promise<unknown>;
  onDiscardDraft?: (draft: AgentRequest) => Promise<unknown>;
  // Customers whose name or email matches the search, shown under the filters (contact-matches.tsx).
  contactMatches?: ReactNode;
  onOpenContact?: (contactId: string) => void;
  onNewConversation?: () => void;
  // The goal given at cloud sign-up; it orders the get-started steps of an empty inbox.
  onboardingGoal?: OnboardingGoal | null;
}

export function InboxPage({ hosted = false, conversations = [], selected, timeline = [], timeEntries = [], agents = [], tags = [], timezone = 'UTC', attachmentLimits, savedReplies, customerName, agentName, onManageSavedReplies, savedRepliesState, onSelect, query = {}, onQuery, viewsMenu, onMessage, onUpdate, onAddTag, onRemoveTag, onCreateTag, page = 1, hasNextPage = false, onPage, onRetryDelivery, outboundNotice, deliveryNotice, filtered, nothingOpen, loading, refreshing, timelineLoading, conversationFailed, onRetryConversation, failed, onRetry, canManage, onOpenSettings, currentAgentId, onBulkUpdate, replyDrafts = [], draftConversationIds, viewers, openViewers, onReplyDraft, onReplyEdited, onSendDraft, onDiscardDraft, contactMatches, onOpenContact, onNewConversation, onboardingGoal }: InboxPageProps) {
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
  // The newest reply on screen; unknown while the timeline loads, so nothing is checked against a guess.
  const latestReply = useMemo(() => timeline.reduce((latest, item) => Math.max(latest, item.reply_number ?? 0), 0), [timeline]);
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
          <div className="list-title"><h1>{messages.inbox}</h1><span>{conversations.length}</span>
            {onNewConversation && <button type="button" className="new-conversation-button" data-opens="new-conversation" onClick={onNewConversation}>{messages.newConversation}</button>}</div>
          <div className="list-head">
            <InboxFilters query={query} onChange={onQuery} tags={tags} agents={agents} timezone={timezone} conversations={conversations} focusSignal={focusSignal}
              views={viewsMenu} />
          </div>
          {bulkFailed && <p role="alert" className="error-copy bulk-message">{messages.bulkUpdateFailed}</p>}
          {/* A live region without role="status": it is mounted before the first result so screen readers
              announce it, while an idle inbox still exposes no status. */}
          <p aria-live="polite" className="bulk-message">{bulkNotice}</p>
          {contactMatches}
        </header>
        <ConversationList items={conversations} selectedId={selected?.id} timezone={timezone} onSelect={onSelect}
          page={page} hasNextPage={hasNextPage} onPage={onPage} filtered={filtered} nothingOpen={nothingOpen} loading={loading} refreshing={refreshing}
          failed={failed} onRetry={onRetry} onClearFilters={clearFilters} canManage={canManage} onOpenSettings={onOpenSettings}
          checkedIds={checked} onToggle={toggle} onToggleAll={toggleAll} selectAllRef={selectAll}
          toolbar={checkedIds.length > 0 && <BulkActionBar selected={conversations.filter((item) => checked.has(item.id))} agents={agents} tags={tags} currentAgentId={currentAgentId}
            busy={bulkBusy} onApply={(changes) => void applyBulk(changes)} onClear={() => { changeSelection(new Set()); setRefocus((count) => count + 1); }} />} draftIds={draftConversationIds} viewers={viewers}
          grouped={(query.sort ?? 'updated_desc') === 'updated_desc'} onboardingGoal={onboardingGoal} />
      </section>
      <div className="conversation-center panel">
        <TimelinePanel conversation={selected} items={timeline} onOpenContact={onOpenContact} timeEntries={timeEntries} loading={timelineLoading} loadFailed={conversationFailed} onRetryLoad={onRetryConversation} timezone={timezone} onRetryDelivery={onRetryDelivery} agentNames={agentNames}
          viewers={openViewers} detailsOpen={detailsOpen} detailsToggleRef={detailsToggle} onToggleDetails={() => (detailsOpen ? closeDetails() : setDetailsOpen(true))} />
        {outboundNotice && <p role="status" className="composer-notice">{outboundNotice}
          {canManage && onOpenSettings && <> <button type="button" className="link-action" onClick={onOpenSettings}>{messages.openSettings}</button></>}</p>}
        {deliveryNotice && <p role="alert" className="composer-notice">{deliveryNotice}</p>}
        {replyDrafts.map((draft) => <ReplyDraftCard key={draft.id} draft={draft} customer={customerName}
          onSend={(edit) => onSendDraft?.(draft, edit) ?? Promise.resolve()} onDiscard={() => onDiscardDraft?.(draft) ?? Promise.resolve()} />)}
        {/* One composer per conversation: its draft and refusal belong to the customer it was written for. */}
        <ReplyComposer key={selected?.id} disabled={!selected} onSubmit={onMessage} attachmentLimits={attachmentLimits} suggestedCc={copied}
          latestReplyNumber={selected && !timelineLoading ? latestReply : undefined} timezone={timezone}
          savedReplies={savedReplies} customerName={customerName} agentName={agentName} onManageSavedReplies={onManageSavedReplies} savedRepliesState={savedRepliesState}
          canManage={canManage} onReplyDraft={onReplyDraft} onReplyEdited={onReplyEdited} />
      </div>
      <DetailsPanel conversation={selected} timezone={timezone} agents={agents} tags={tags} onUpdate={onUpdate}
        onAddTag={onAddTag} onRemoveTag={onRemoveTag} onCreateTag={onCreateTag} onClose={detailsOpen ? closeDetails : undefined}
        onShowContactConversations={onQuery ? (contact) => onQuery({ contact, status: 'all' }) : undefined} />
    </main>
  );
}
