import { useEffect, useRef, useState } from 'react';
import type { Conversation, Message, TeamMember, Tag, TimeEntry } from '../../api/schemas';
import type { UpdateConversationRequest } from '../../api/client';
import { ConversationList } from './conversation-list';
import { DetailsPanel } from './details-panel';
import { InboxFilters } from './inbox-filters';
import type { AttachmentLimits } from './attachment-picker';
import { ReplyComposer } from './reply-composer';
import { TimelinePanel } from './timeline-panel';
import { messages } from '../../messages';

interface InboxPageProps {
  conversations?: Conversation[];
  selected?: Conversation;
  timeline?: Message[];
  timeEntries?: TimeEntry[];
  agents?: TeamMember[];
  tags?: Tag[];
  timezone?: string;
  onSelect?: (id: string) => void;
  onFilters?: (filters: Record<string, string>) => void;
  onMessage?: (kind: 'outgoing' | 'note', body: string, files?: File[]) => void | Promise<unknown>;
  attachmentLimits?: AttachmentLimits;
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
  refreshing?: boolean;
  timelineLoading?: boolean;
  failed?: boolean;
  onRetry?: () => void;
  canManage?: boolean;
  onOpenSettings?: () => void;
}

export function InboxPage({ conversations = [], selected, timeline = [], timeEntries = [], agents = [], tags = [], timezone = 'UTC', attachmentLimits, onSelect, onFilters, onMessage, onUpdate, onAddTag, onRemoveTag, onCreateTag, page = 1, hasNextPage = false, onPage, onRetryDelivery, outboundNotice, deliveryNotice, filtered, loading, refreshing, timelineLoading, failed, onRetry, canManage, onOpenSettings }: InboxPageProps) {
  // Below the three-pane width the details panel opens as a drawer instead of a column.
  const [detailsOpen, setDetailsOpen] = useState(false);
  // Filters are an uncontrolled form; clearing remounts it so every field returns to its default.
  const [filterGeneration, setFilterGeneration] = useState(0);
  const clearFilters = () => { setFilterGeneration((current) => current + 1); onFilters?.({}); };
  const detailsToggle = useRef<HTMLButtonElement>(null);
  const listPane = useRef<HTMLElement>(null);
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
          <div className="list-title"><h1>{messages.inbox}</h1><span>{conversations.length}</span></div>
          <InboxFilters key={filterGeneration} tags={tags} onChange={onFilters} focusSearch={filterGeneration > 0} />
        </header>
        <ConversationList items={conversations} selectedId={selected?.id} timezone={timezone} onSelect={onSelect}
          page={page} hasNextPage={hasNextPage} onPage={onPage} filtered={filtered} loading={loading} refreshing={refreshing}
          failed={failed} onRetry={onRetry} onClearFilters={clearFilters} canManage={canManage} onOpenSettings={onOpenSettings} />
      </section>
      <div className="conversation-center panel">
        <TimelinePanel conversation={selected} items={timeline} timeEntries={timeEntries} loading={timelineLoading} timezone={timezone} onRetryDelivery={onRetryDelivery}
          detailsOpen={detailsOpen} detailsToggleRef={detailsToggle} onToggleDetails={() => (detailsOpen ? closeDetails() : setDetailsOpen(true))} />
        {outboundNotice && <p role="status" className="composer-notice">{outboundNotice}</p>}
        {deliveryNotice && <p role="alert" className="composer-notice">{deliveryNotice}</p>}
        {/* One composer per conversation: its draft and refusal belong to the customer it was written for. */}
        <ReplyComposer key={selected?.id} disabled={!selected} onSubmit={onMessage} attachmentLimits={attachmentLimits} />
      </div>
      <DetailsPanel conversation={selected} timezone={timezone} agents={agents} tags={tags} onUpdate={onUpdate}
        onAddTag={onAddTag} onRemoveTag={onRemoveTag} onCreateTag={onCreateTag} onClose={detailsOpen ? closeDetails : undefined} />
    </main>
  );
}
