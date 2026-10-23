import { Fragment, useState } from 'react';
import type { ReactNode, RefObject } from 'react';
import type { Conversation, ConversationViewer, Message, TimeEntry } from '../../api/schemas';
import { messages } from '../../messages';
import { CheckCircleIcon, RotateCcwIcon } from '../../ui/icons';
import { Part } from '../../ui/lazy-part';
import { MessageItem } from './message-item';
import { presenceViewPart } from './presence-part';
import { PriorityLabel, StatusLabel } from './conversation-labels';
import { TimeEntriesSection } from './time-entries-section';

interface TimelinePanelProps {
  conversation?: Conversation;
  items: Message[];
  // Shown after the first customer message: what AI suggested or applied for it.
  afterFirstCustomerMessage?: ReactNode;
  timeEntries?: TimeEntry[];
  loading?: boolean;
  // The ticket an address names could not be loaded: say so, and offer to try again.
  loadFailed?: boolean;
  onRetryLoad?: () => void;
  timezone: string;
  onRetryDelivery?: (messageId: string) => void;
  detailsOpen?: boolean;
  // An AI triage suggestion waits in the details, which a narrow screen hides behind the toggle.
  suggestionWaiting?: boolean;
  onToggleDetails?: () => void;
  detailsToggleRef?: RefObject<HTMLButtonElement | null>;
  // Team member names by id, to name the person who approved a reply an AI agent drafted.
  agentNames?: ReadonlyMap<string, string>;
  // Opens the customer's page: every conversation they had, newest first.
  onOpenContact?: (contactId: string) => void;
  // The others who have this conversation open now, and whether one is writing a reply.
  viewers?: ConversationViewer[];
  // Changes the status from the header: Resolve while it is open or pending, Reopen once resolved or closed.
  onSetStatus?: (status: Conversation['status']) => Promise<unknown>;
}

// One press resolves the conversation without the details pane (a drawer on narrower screens); a resolved or
// closed one offers the way back. A failure says so beside the button.
function StatusButton({ status, onSetStatus }: { status: Conversation['status']; onSetStatus: (status: Conversation['status']) => Promise<unknown> }) {
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const done = status === 'resolved' || status === 'closed';
  const change = () => {
    if (busy) return;
    setBusy(true);
    setFailed(false);
    onSetStatus(done ? 'open' : 'resolved').catch(() => setFailed(true)).finally(() => setBusy(false));
  };
  return <>
    {/* aria-disabled, not disabled: a disabled button drops the keyboard focus, and Reopen takes its place. */}
    <button type="button" className="status-button" aria-disabled={busy || undefined} onClick={change}>
      {done ? <RotateCcwIcon /> : <CheckCircleIcon />}{done ? messages.reopen : messages.resolve}</button>
    {failed && <p role="alert" className="error-copy status-button-error">{messages.statusChangeFailed}</p>}
  </>;
}

export function TimelinePanel({ conversation, items, afterFirstCustomerMessage, timeEntries = [], loading = false, loadFailed = false, onRetryLoad, timezone, onRetryDelivery, detailsOpen = false, suggestionWaiting = false, onToggleDetails, detailsToggleRef, agentNames, onOpenContact, viewers = [], onSetStatus }: TimelinePanelProps) {
  const firstCustomer = items.findIndex((item) => item.kind === 'incoming');
  return (
    <section className="timeline-panel" aria-label={messages.timeline}>
      <header className="timeline-header">
        <div>
          <h2>{conversation?.subject ?? messages.selectConversation}</h2>
          {conversation && <div className="timeline-meta"><StatusLabel status={conversation.status} /><PriorityLabel priority={conversation.priority} />
            {/* A plain click opens the page in the app; a new tab or a copied address still works. */}
            <a className="timeline-contact" href={`/contacts/${conversation.contact_id}`} aria-label={messages.openContact(conversation.contact_name)}
              onClick={(event) => { if (onOpenContact && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey && event.button === 0) { event.preventDefault(); onOpenContact(conversation.contact_id); } }}>
              {conversation.contact_name}</a></div>}
          {conversation && viewers.length > 0 && <Part part={presenceViewPart} render={(view) => <view.ConversationViewers viewers={viewers} timezone={timezone} />} />}
        </div>
        {conversation && (onSetStatus || onToggleDetails) && <div className="timeline-actions">
          {onSetStatus && <StatusButton key={conversation.id} status={conversation.status} onSetStatus={onSetStatus} />}
          {onToggleDetails && <button type="button" className="details-toggle" ref={detailsToggleRef}
            aria-expanded={detailsOpen} aria-controls="conversation-details" onClick={onToggleDetails}
            aria-label={suggestionWaiting ? `${messages.showDetails}, ${messages.aiSuggestionWaiting}` : undefined}>
            {messages.showDetails}{suggestionWaiting && <span className="details-badge" aria-hidden="true" />}</button>}
        </div>}
      </header>
      <div className="timeline-scroll">
        <div className="timeline-stack">
          {!conversation && loadFailed && <div role="alert" className="empty-copy"><p>{messages.conversationNotLoaded}</p>
            <button type="button" onClick={onRetryLoad}>{messages.retryLoad}</button></div>}
          {loading && items.length === 0 && <p role="status" className="empty-copy">{messages.loading}</p>}
          {items.map((item, index) => <Fragment key={item.id}><MessageItem item={item} timezone={timezone} onRetryDelivery={onRetryDelivery}
            approvedBy={item.author_agent_id ? agentNames?.get(item.author_agent_id) : undefined} />
            {index === firstCustomer && afterFirstCustomerMessage}</Fragment>)}
          {timeEntries.length > 0 && <TimeEntriesSection items={timeEntries} timezone={timezone} />}
        </div>
      </div>
    </section>
  );
}
