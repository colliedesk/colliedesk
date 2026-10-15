import { useRef, useState } from 'react';
import type { Agent, Conversation, Message, Tag, TimeEntry } from '../../api/schemas';
import type { UpdateConversationRequest } from '../../api/client';
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
  onMessage?: (kind: 'outgoing' | 'note', body: string) => void;
  onUpdate?: (conversationId: string, value: UpdateConversationRequest) => void;
  onAddTag?: (conversationId: string, tagId: string) => void;
  onRemoveTag?: (conversationId: string, tagId: string) => void;
  onCreateTag?: (conversationId: string, name: string) => void;
  page?: number;
  hasNextPage?: boolean;
  onPage?: (page: number) => void;
  onRetryDelivery?: (messageId: string) => void;
  outboundNotice?: string;
  filtered?: boolean;
  loading?: boolean;
  failed?: boolean;
  onRetry?: () => void;
  canManage?: boolean;
  onOpenSettings?: () => void;
}

export function InboxPage({ conversations = [], selected, timeline = [], timeEntries = [], agents = [], tags = [], timezone = 'UTC', onSelect, onFilters, onMessage, onUpdate, onAddTag, onRemoveTag, onCreateTag, page = 1, hasNextPage = false, onPage, onRetryDelivery, outboundNotice, filtered, loading, failed, onRetry, canManage, onOpenSettings }: InboxPageProps) {
  // Below the three-pane width the details panel opens as a drawer instead of a column.
  const [detailsOpen, setDetailsOpen] = useState(false);
  const detailsToggle = useRef<HTMLButtonElement>(null);
  const closeDetails = () => { setDetailsOpen(false); detailsToggle.current?.focus(); };
  return (
    <main className="inbox-shell" data-details-open={detailsOpen}>
      <section className="list-pane" aria-label={messages.conversationList}>
        <header>
          <div className="list-title"><h1>{messages.inbox}</h1><span>{conversations.length}</span></div>
          <InboxFilters tags={tags} onChange={onFilters} />
        </header>
        <ConversationList items={conversations} selectedId={selected?.id} timezone={timezone} onSelect={onSelect}
          page={page} hasNextPage={hasNextPage} onPage={onPage} filtered={filtered} loading={loading}
          failed={failed} onRetry={onRetry} canManage={canManage} onOpenSettings={onOpenSettings} />
      </section>
      <div className="conversation-center">
        <TimelinePanel conversation={selected} items={timeline} timeEntries={timeEntries} timezone={timezone} onRetryDelivery={onRetryDelivery}
          detailsOpen={detailsOpen} detailsToggleRef={detailsToggle} onToggleDetails={() => (detailsOpen ? closeDetails() : setDetailsOpen(true))} />
        {outboundNotice && <p role="status" className="composer-notice">{outboundNotice}</p>}
        <ReplyComposer disabled={!selected} onSubmit={onMessage} />
      </div>
      <DetailsPanel conversation={selected} timezone={timezone} agents={agents} tags={tags} onUpdate={onUpdate}
        onAddTag={onAddTag} onRemoveTag={onRemoveTag} onCreateTag={onCreateTag} onClose={detailsOpen ? closeDetails : undefined} />
    </main>
  );
}
