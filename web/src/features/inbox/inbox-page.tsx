import type { Agent, Conversation, Message, Tag } from '../../api/schemas';
import type { UpdateConversationRequest } from '../../api/client';
import { ConversationList } from './conversation-list';
import { DetailsPanel } from './details-panel';
import { InboxFilters } from './inbox-filters';
import { ReplyComposer } from './reply-composer';
import { TimelinePanel } from './timeline-panel';

interface InboxPageProps {
  conversations?: Conversation[];
  selected?: Conversation;
  timeline?: Message[];
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
}

export function InboxPage({ conversations = [], selected, timeline = [], agents = [], tags = [], timezone = 'UTC', onSelect, onFilters, onMessage, onUpdate, onAddTag, onRemoveTag, onCreateTag, page = 1, hasNextPage = false, onPage, onRetryDelivery }: InboxPageProps) {
  return (
    <main className="inbox-shell">
      <InboxFilters tags={tags} onChange={onFilters} />
      <div className="inbox-grid">
        <ConversationList items={conversations} selectedId={selected?.id} timezone={timezone} onSelect={onSelect}
          page={page} hasNextPage={hasNextPage} onPage={onPage} />
        <div className="conversation-center"><TimelinePanel conversation={selected} items={timeline} timezone={timezone} onRetryDelivery={onRetryDelivery} />
          <ReplyComposer disabled={!selected} onSubmit={onMessage} /></div>
        <DetailsPanel conversation={selected} agents={agents} tags={tags} onUpdate={onUpdate}
          onAddTag={onAddTag} onRemoveTag={onRemoveTag} onCreateTag={onCreateTag} />
      </div>
    </main>
  );
}
