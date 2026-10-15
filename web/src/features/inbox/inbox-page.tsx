import { useEffect, useRef, useState } from 'react';
import type { Agent, Conversation, Message, Tag, TimeEntry } from '../../api/schemas';
import type { BulkConversationChanges, UpdateConversationRequest } from '../../api/client';
import { BulkActionBar } from './bulk-action-bar';
import { ConversationList } from './conversation-list';
import { DetailsPanel } from './details-panel';
import { InboxFilters } from './inbox-filters';
import { ReplyComposer } from './reply-composer';
import { TimelinePanel } from './timeline-panel';
import { messages } from '../../messages';

interface InboxPageProps {
  conversations?: Conversation[];
  selected?: Conversation;
  timeline?: Message[];
  timeEntries?: TimeEntry[];
  agents?: Agent[];
  tags?: Tag[];
  timezone?: string;
  onSelect?: (id: string) => void;
  onFilters?: (filters: Record<string, string>) => void;
  onMessage?: (kind: 'outgoing' | 'note', body: string) => void | Promise<unknown>;
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
  loading?: boolean;
  failed?: boolean;
  onRetry?: () => void;
  canManage?: boolean;
  onOpenSettings?: () => void;
  currentAgentId?: string;
  // onBulkUpdate resolves when every listed conversation changed, and rejects when none did.
  onBulkUpdate?: (ids: string[], changes: BulkConversationChanges) => Promise<unknown>;
}

export function InboxPage({ conversations = [], selected, timeline = [], timeEntries = [], agents = [], tags = [], timezone = 'UTC', onSelect, onFilters, onMessage, onUpdate, onAddTag, onRemoveTag, onCreateTag, page = 1, hasNextPage = false, onPage, onRetryDelivery, outboundNotice, deliveryNotice, filtered, loading, failed, onRetry, canManage, onOpenSettings, currentAgentId, onBulkUpdate }: InboxPageProps) {
  // Below the three-pane width the details panel opens as a drawer instead of a column.
  const [detailsOpen, setDetailsOpen] = useState(false);
  const detailsToggle = useRef<HTMLButtonElement>(null);
  const listPane = useRef<HTMLElement>(null);
  const [checked, setChecked] = useState<ReadonlySet<string>>(new Set());
  const [bulkBusy, setBulkBusy] = useState(false);
  const [bulkFailed, setBulkFailed] = useState(false);
  const [bulkNotice, setBulkNotice] = useState('');
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
    } catch {
      setBulkFailed(true);
    } finally {
      setBulkBusy(false);
    }
  };
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
          <div className="list-head" hidden={checkedIds.length > 0}>
            <div className="list-title"><h1>{messages.inbox}</h1><span>{conversations.length}</span></div>
            <InboxFilters tags={tags} onChange={onFilters} />
          </div>
          {checkedIds.length > 0 && <BulkActionBar count={checkedIds.length} agents={agents} tags={tags} currentAgentId={currentAgentId}
            busy={bulkBusy} onApply={(changes) => void applyBulk(changes)} onClear={() => changeSelection(new Set())} />}
          {bulkFailed && <p role="alert" className="error-copy bulk-message">{messages.bulkUpdateFailed}</p>}
          {bulkNotice && <p role="status" className="bulk-message">{bulkNotice}</p>}
        </header>
        <ConversationList items={conversations} selectedId={selected?.id} timezone={timezone} onSelect={onSelect}
          page={page} hasNextPage={hasNextPage} onPage={onPage} filtered={filtered} loading={loading}
          failed={failed} onRetry={onRetry} canManage={canManage} onOpenSettings={onOpenSettings}
          checkedIds={checked} onToggle={toggle} onToggleAll={toggleAll} />
      </section>
      <div className="conversation-center panel">
        <TimelinePanel conversation={selected} items={timeline} timeEntries={timeEntries} timezone={timezone} onRetryDelivery={onRetryDelivery}
          detailsOpen={detailsOpen} detailsToggleRef={detailsToggle} onToggleDetails={() => (detailsOpen ? closeDetails() : setDetailsOpen(true))} />
        {outboundNotice && <p role="status" className="composer-notice">{outboundNotice}</p>}
        {deliveryNotice && <p role="alert" className="composer-notice">{deliveryNotice}</p>}
        {/* One composer per conversation: its draft and refusal belong to the customer it was written for. */}
        <ReplyComposer key={selected?.id} disabled={!selected} onSubmit={onMessage} />
      </div>
      <DetailsPanel conversation={selected} timezone={timezone} agents={agents} tags={tags} onUpdate={onUpdate}
        onAddTag={onAddTag} onRemoveTag={onRemoveTag} onCreateTag={onCreateTag} onClose={detailsOpen ? closeDetails : undefined} />
    </main>
  );
}
