import { useEffect, useMemo, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import type { AgentRequest, Conversation, ConversationViewer, Message, SavedReply, TeamMember, Tag, TimeEntry } from '../../api/schemas';
import type { BulkConversationChanges, UpdateConversationRequest } from '../../api/client';
import { BulkActionBar, mayTellCustomers } from './bulk-action-bar';
import { ConversationList } from './conversation-list';
import type { SettingsTarget } from './conversation-list';
import { AITriageSlot } from './ai-triage-slot';
import { DetailsPanel } from './details-panel';
import { InboxFilters } from './inbox-filters';

import type { InboxQuery } from '../../lib/inbox-query';
import type { AttachmentLimits } from './attachment-picker';
import { ReplyComposer } from './reply-composer';
import { ReplyDraftCard } from './reply-draft-card';
import type { DraftEdit } from './reply-draft-card';
import { suggestedCc } from './suggested-cc';
import { TimelinePanel } from './timeline-panel';
import { messageMovesPart } from './message-moves-part';
import { Part } from '../../ui/lazy-part';
import { messages } from '../../messages';

interface InboxPageProps {
  // The hosted cloud, where the server trusts a Cc line only on mail from the platform forwarder.
  hosted?: boolean;
  // The workspace whose unsent replies this browser keeps, one per conversation.
  workspaceId?: string;
  conversations?: Conversation[];
  selected?: Conversation;
  timeline?: Message[];
  timeEntries?: TimeEntry[];
  agents?: TeamMember[];
  tags?: Tag[];
  timezone?: string;
  onSelect?: (id: string) => void;
  // The address names a conversation: on a phone it takes the screen instead of the list, with onBack to return.
  threadOpen?: boolean;
  onBack?: () => void;
  // The filters of the list, as the address carries them, and a change to them.
  query?: InboxQuery;
  onQuery?: (query: InboxQuery) => void;
  // The saved views menu, which sits in the filter bar (the route loads it lazily with its data).
  viewsMenu?: ReactNode;
  // seen is the newest reply number on screen when the reply was started.
  // key is the send's Idempotency-Key when the reply box keeps a draft (it is stored with the draft).
  onMessage?: (kind: 'outgoing' | 'note', body: string, files?: File[], cc?: string[], seen?: number, aiDraft?: string, key?: string) => void | Promise<unknown>;
  // onFindSent says whether the send under key stored its message in the conversation.
  onFindSent?: (conversationId: string, key: string) => Promise<boolean>;
  // Reply drafts are on in the workspace; undefined until that is known (nothing is shown then).
  aiDrafts?: boolean;
  // Triage suggestions are on in the workspace.
  aiTriage?: boolean;
  // The server runs Autofix (/api/v1/config).
  autofix?: boolean;
  onTriageApplied?: () => void;
  attachmentLimits?: AttachmentLimits;
  savedReplies?: SavedReply[];
  // The names the composer fills into a saved reply: the selected customer and the signed-in agent.
  customerName?: string;
  agentName?: string;
  onManageSavedReplies?: () => void;
  savedRepliesState?: 'loading' | 'failed' | 'ready';
  onUpdate?: (conversationId: string, value: UpdateConversationRequest) => void;
  // Changes one conversation's status and settles when the server answered: the thread header and Send and resolve.
  onSetStatus?: (conversationId: string, status: Conversation['status']) => Promise<unknown>;
  onAddTag?: (conversationId: string, tagId: string) => void;
  onRemoveTag?: (conversationId: string, tagId: string) => void;
  onCreateTag?: (conversationId: string, name: string) => void;
  page?: number;
  hasNextPage?: boolean;
  // pageSize is the page length the list asked for, to number the conversations of a later page.
  pageSize?: number;
  onPage?: (page: number) => void;
  onRetryDelivery?: (messageId: string) => void;
  outboundNotice?: string;
  // sendsFrom names the channel a reply leaves from, when the workspace has more than one it could be.
  sendsFrom?: string;
  // channelLabel names the channel of a conversation where the workspace has two of its type.
  channelLabel?: (conversation: Conversation) => string | undefined;
  // outboundLink is where Open Settings in that notice leads: the channel that cannot send.
  outboundLink?: SettingsTarget;
  // noReplies: the conversation's channel was removed, so the composer sends notes only.
  noReplies?: boolean;
  // deliveryNotice explains a refused retry of a failed delivery.
  deliveryNotice?: string;
  filtered?: boolean;
  nothingOpen?: boolean;
  loading?: boolean;
  refreshing?: boolean;
  timelineLoading?: boolean;
  conversationFailed?: boolean;
  onRetryConversation?: () => void;
  // results_refreshed: the list of a results number whose snapshot is no longer kept (410).
  failed?: boolean | 'results_refreshed';
  onRetry?: () => void;
  canManage?: boolean;
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
  onNewConversation?: () => void;
  // The setup guide while setup is unfinished; it floats in a corner, outside the conversation list.
  guide?: ReactNode;
  onTestMessage?: () => Promise<unknown>;
}

// ShownCount is how many conversations the list shows. One page shows its count; with more pages a bare
// "50" reads as the total, so it shows the range of this page instead ("1–50", "51–75").
function ShownCount({ count, page, pageSize, more }: { count: number; page: number; pageSize: number; more: boolean }) {
  if (page === 1 && !more) return <span>{count}</span>;
  const first = count === 0 ? 0 : (page - 1) * pageSize + 1;
  const last = (page - 1) * pageSize + count;
  return <span><span aria-hidden="true">{`${first}–${last}`}</span><span className="sr-only">{messages.inboxShown(first, last)}</span></span>;
}

// What a bulk change says once done: a status that may have told customers in a chat says so, with the
// server's count when it gives one.
function bulkSaid(count: number, changes: BulkConversationChanges, rows: Conversation[], result: unknown) {
  if (!changes.status || changes.notify === false || !mayTellCustomers(rows)) return messages.bulkUpdated(count);
  const told = result && typeof result === 'object' && 'told' in result && typeof result.told === 'number' ? result.told : undefined;
  return told === undefined ? `${messages.bulkUpdated(count)}.${messages.bulkMayHaveTold}` : messages.customersTold(count, changes.status, told);
}

export function InboxPage({ hosted = false, workspaceId, conversations = [], selected, threadOpen = false, onBack, timeline = [], timeEntries = [], agents = [], tags = [], timezone = 'UTC', attachmentLimits, savedReplies, customerName, agentName, onManageSavedReplies, savedRepliesState, onSelect, query = {}, onQuery, viewsMenu, onMessage, onUpdate, onSetStatus, onAddTag, onRemoveTag, onCreateTag, page = 1, hasNextPage = false, pageSize = 50, onPage, onRetryDelivery, outboundNotice, sendsFrom, channelLabel, outboundLink = '/settings/channels/type/email?tab=replies', noReplies, deliveryNotice, filtered, nothingOpen, loading, refreshing, timelineLoading, conversationFailed, onRetryConversation, failed, onRetry, canManage, currentAgentId, onBulkUpdate, replyDrafts = [], draftConversationIds, viewers, openViewers, onReplyDraft, onReplyEdited, onSendDraft, onDiscardDraft, contactMatches, onNewConversation, guide, onTestMessage, aiDrafts, aiTriage = false, onTriageApplied, onFindSent, autofix = false }: InboxPageProps) {
  // Below the three-pane width the details panel opens as a drawer instead of a column.
  // Where this browser keeps the unsent text: known once the workspace and agent load, which may be after
  // the conversation on a reload. The composer is remounted when it arrives, so it reads the kept draft.
  const draftKey = selected && workspaceId && currentAgentId ? `${workspaceId}:${currentAgentId}:${selected.id}` : undefined;
  const latestCustomerMessage = useMemo(() => [...timeline].reverse().find((item) => item.kind === 'incoming')?.id, [timeline]);
  const [detailsOpen, setDetailsOpen] = useState(false);
  // The details' AI triage card says when its suggestion has a part nobody accepted yet.
  const [triageWaiting, setTriageWaiting] = useState(false);
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
      const result = await onBulkUpdate(ids, changes);
      setChecked(new Set());
      setBulkNotice(bulkSaid(ids.length, changes, conversations.filter((item) => checked.has(item.id)), result));
      setRefocus((count) => count + 1);
    } catch {
      setBulkFailed(true);
    } finally {
      setBulkBusy(false);
    }
  };
  const copied = useMemo(() => suggestedCc(timeline, { hosted }), [timeline, hosted]);
  // A refusal under a long timeline (a phone) is brought on screen with the reply drafts below it, whose
  // buttons are the next step, not left below the screen.
  const deliveryRef = useRef<HTMLParagraphElement>(null);
  useEffect(() => {
    const notice = deliveryRef.current;
    if (!deliveryNotice || !notice) return;
    const drafts = notice.parentElement?.querySelectorAll('.reply-draft');
    // The code comes with the presence chunk, which an open conversation has loaded already.
    let stop = () => {};
    let gone = false;
    void import('./presence-view').then(({ keepInView }) => { if (!gone) stop = keepInView(notice, drafts?.[drafts.length - 1] ?? notice); });
    return () => { gone = true; stop(); };
  }, [deliveryNotice]);
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
    <main className="inbox-shell" data-view={threadOpen ? 'thread' : 'list'} data-details-open={detailsOpen}>
      <section className="list-pane panel" ref={listPane} tabIndex={-1} aria-label={messages.conversationList}>
        <header>
          <div className="list-title"><h1>{messages.inbox}</h1><ShownCount count={conversations.length} page={page} pageSize={pageSize} more={hasNextPage} />
            {onNewConversation && <button type="button" className="new-conversation-button" data-opens="new-conversation" onClick={onNewConversation}>{messages.newConversation}</button>}</div>
          <div className="list-head">
            <InboxFilters query={query} onChange={onQuery} tags={tags} agents={agents} currentAgentId={currentAgentId} timezone={timezone} conversations={conversations} focusSignal={focusSignal}
              views={viewsMenu} />
          </div>
          {bulkFailed && <p role="alert" className="error-copy bulk-message">{messages.bulkUpdateFailed}</p>}
          {/* A live region without role="status": it is mounted before the first result so screen readers
              announce it, while an idle inbox still exposes no status. */}
          <p aria-live="polite" className="bulk-message">{bulkNotice}</p>
          {contactMatches}
        </header>
        <ConversationList items={conversations} channelLabel={channelLabel} selectedId={selected?.id} timezone={timezone} onSelect={onSelect}
          page={page} hasNextPage={hasNextPage} onPage={onPage} filtered={filtered} nothingOpen={nothingOpen} loading={loading} refreshing={refreshing}
          failed={failed} onRetry={onRetry} onClearFilters={clearFilters}
          checkedIds={checked} onToggle={toggle} onToggleAll={toggleAll} selectAllRef={selectAll}
          toolbar={checkedIds.length > 0 && <BulkActionBar selected={conversations.filter((item) => checked.has(item.id))} agents={agents} tags={tags} currentAgentId={currentAgentId}
            busy={bulkBusy} onApply={(changes) => void applyBulk(changes)} onClear={() => { changeSelection(new Set()); setRefocus((count) => count + 1); }} />} draftIds={draftConversationIds} viewers={viewers}
          grouped={(query.sort ?? 'updated_desc') === 'updated_desc'} onTestMessage={onTestMessage} />
      </section>
      <div className="conversation-center panel">
        <TimelinePanel conversation={selected} channelLabel={selected && channelLabel?.(selected)} items={timeline} onBack={threadOpen ? onBack : undefined}
          afterFirstCustomerMessage={aiTriage && selected ? <AITriageSlot placement="timeline" conversationId={selected.id} tags={tags} agents={agents} onApplied={() => onTriageApplied?.()}
            currentAgentId={currentAgentId} timezone={timezone} canManage={canManage} /> : undefined} timeEntries={timeEntries} loading={timelineLoading} loadFailed={conversationFailed} onRetryLoad={onRetryConversation} timezone={timezone} onRetryDelivery={onRetryDelivery} agentNames={agentNames}
          before={selected && <Part part={messageMovesPart} render={(part) => <part.MessageMovesLog key={selected.id} conversation={selected} timeline={timeline}
            timezone={timezone} currentAgentId={currentAgentId} agentNames={agentNames} />} />}
          messageLabel={selected && ((item) => item.kind === 'incoming' && <Part part={messageMovesPart} render={(part) => <part.MessageIntakeLabel conversation={selected} message={item} />} />)}
          messageMenu={selected && ((item) => item.kind === 'incoming' && <Part part={messageMovesPart} render={(part) => <part.MessageMoveMenu conversation={selected} message={item} timeline={timeline} timezone={timezone} />} />)}
          viewers={openViewers} onSetStatus={selected && onSetStatus ? (status) => onSetStatus(selected.id, status) : undefined} detailsOpen={detailsOpen} suggestionWaiting={aiTriage && triageWaiting} detailsToggleRef={detailsToggle} onToggleDetails={() => (detailsOpen ? closeDetails() : setDetailsOpen(true))} autofix={autofix} />
        {outboundNotice && <p role="status" className="composer-notice">{outboundNotice}
          {canManage && <> <a className="link-action" href={outboundLink}>{messages.openSettings}</a></>}</p>}
        {sendsFrom && <p className="composer-from">{sendsFrom}</p>}
        {deliveryNotice && <p role="alert" className="composer-notice" ref={deliveryRef}>{deliveryNotice}</p>}
        {replyDrafts.map((draft) => <ReplyDraftCard key={draft.id} draft={draft} customer={customerName}
          onSend={(edit) => onSendDraft?.(draft, edit) ?? Promise.resolve()} onDiscard={() => onDiscardDraft?.(draft) ?? Promise.resolve()} />)}
        {/* One composer per conversation: its draft and refusal belong to the customer it was written for. */}
        <ReplyComposer key={draftKey ?? selected?.id} disabled={!selected} noReplies={noReplies} draftKey={draftKey} onSubmit={onMessage} attachmentLimits={attachmentLimits} suggestedCc={copied}
          latestReplyNumber={selected && !timelineLoading ? latestReply : undefined} timezone={timezone}
          latestCustomerMessage={latestCustomerMessage}
          savedReplies={savedReplies} customerName={customerName} agentName={agentName} onManageSavedReplies={onManageSavedReplies} savedRepliesState={savedRepliesState}
          canManage={canManage} onReplyDraft={onReplyDraft} onReplyEdited={onReplyEdited}
          // Send and resolve is offered while there is something to resolve; a resolved or closed one is reopened from the header.
          onResolve={selected && onSetStatus && (selected.status === 'open' || selected.status === 'pending') ? () => onSetStatus(selected.id, 'resolved') : undefined}
          aiDraftsFor={aiDrafts && selected ? selected.id : undefined} aiDraftsOff={aiDrafts === false} agentId={currentAgentId}
          findSent={selected && onFindSent ? (key) => onFindSent(selected.id, key) : undefined} />
      </div>
      <DetailsPanel autofix={autofix} conversation={selected} timezone={timezone} agents={agents} tags={tags} onUpdate={onUpdate} aiTriage={aiTriage} onTriageApplied={onTriageApplied} currentAgentId={currentAgentId} canManage={canManage} onTriageWaiting={setTriageWaiting}
        onAddTag={onAddTag} onRemoveTag={onRemoveTag} onCreateTag={onCreateTag} onClose={detailsOpen ? closeDetails : undefined}
        onShowContactConversations={onQuery ? (contact) => onQuery({ contact, status: 'all' }) : undefined} />
      {guide}
    </main>
  );
}
