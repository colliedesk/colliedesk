import { Fragment, useState } from 'react';
import type { ReactNode, RefObject } from 'react';
import type { Conversation, ConversationViewer, Message, TimeEntry } from '../../api/schemas';
import { messages } from '../../messages';
import { CheckCircleIcon, ChevronLeftIcon, CircleDotIcon, ClockIcon, CopyIcon, RotateCcwIcon } from '../../ui/icons';
import { formatDate, stampTitle, zoneSuffix } from '../../lib/format-date';
import { CopyButton } from '../settings/copy-button';
import { Part } from '../../ui/lazy-part';
import { ActionMenu } from './action-menu';
import { MessageItem } from './message-item';
import { presenceViewPart } from './presence-part';
import { PriorityLabel, StatusLabel } from './conversation-labels';
import { ownRef } from './conversation-ref';
import { slaMark, slaOf } from '../../lib/sla';
import { TimeEntriesSection } from './time-entries-section';
import { AutofixStrip } from '../autofix/autofix-slot';

interface TimelinePanelProps {
  conversation?: Conversation;
  // The conversation's channel, named where the workspace has two of its type.
  channelLabel?: string;
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
  // The others who have this conversation open now, and whether one is writing a reply.
  viewers?: ConversationViewer[];
  // Changes the status from the header: Resolve while it is open or pending, Reopen once resolved or closed.
  // notify false sets it without telling the customer (their chat channel's status notice).
  onSetStatus?: (status: Conversation['status'], options?: { notify?: boolean }) => Promise<unknown>;
  // An admin may open the channel's settings from the timeline's line for a channel closing it by itself.
  canManageChannels?: boolean;
  // Returns to the list; the button shows only where the thread replaces the list (a phone).
  onBack?: () => void;
  // The server runs Autofix: narrow screens get the fix's one line under the header.
  autofix?: boolean;
  // Above the messages: what moved out of or into the conversation. Per message: where it came from, and its menu.
  before?: ReactNode;
  messageLabel?: (item: Message) => ReactNode;
  messageMenu?: (item: Message) => ReactNode;
}

// One press resolves the conversation without the details pane (a drawer on narrower screens); a resolved or
// closed one offers the way back. A failure says so beside the button.
type SetStatus = (status: Conversation['status'], options?: { notify?: boolean }) => Promise<unknown>;

function StatusButton({ conversation, onSetStatus }: { conversation: Conversation; onSetStatus: SetStatus }) {
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const done = conversation.status === 'resolved' || conversation.status === 'closed';
  const target = done ? 'open' : 'resolved';
  // The server says which statuses would tell the customer now; only then is there a quiet way to offer.
  const channel = conversation.channel_ref?.name;
  const tells = Boolean(channel) && (conversation.status_notices ?? []).includes(target);
  const change = (quiet: boolean) => {
    if (busy) return;
    setBusy(true);
    setFailed(false);
    (quiet ? onSetStatus(target, { notify: false }) : onSetStatus(target)).catch(() => setFailed(true)).finally(() => setBusy(false));
  };
  // aria-disabled, not disabled: a disabled button drops the keyboard focus, and Reopen takes its place.
  const button = <button type="button" className="status-button" aria-disabled={busy || undefined} onClick={() => change(false)}
    aria-label={tells && channel ? (done ? messages.reopenAndTellName(channel) : messages.resolveAndTellName(channel)) : undefined}>
    {done ? <RotateCcwIcon /> : <CheckCircleIcon />}{tells ? (done ? messages.reopenAndTell : messages.resolveAndTell) : done ? messages.reopen : messages.resolve}</button>;
  return <>
    {tells ? <span className="status-split">{button}
      <ActionMenu label={done ? messages.moreWaysToReopen : messages.moreWaysToResolve} icon={null} disabled={busy} onApply={() => change(true)}>
        {(apply) => <button type="button" onClick={() => apply(undefined)}>{done ? <>{messages.reopenQuietly}{' '}<small>{messages.reopenQuietlyUse}</small></> : messages.resolveQuietly}</button>}
      </ActionMenu></span> : button}
    {failed && <p role="alert" className="error-copy status-button-error">{messages.statusChangeFailed}</p>}
  </>;
}

export function TimelinePanel({ conversation, channelLabel, items, afterFirstCustomerMessage, timeEntries = [], loading = false, loadFailed = false, onRetryLoad, timezone, onRetryDelivery, detailsOpen = false, suggestionWaiting = false, onToggleDetails, detailsToggleRef, agentNames, viewers = [], onSetStatus, canManageChannels = false, onBack, autofix = false, before, messageLabel, messageMenu }: TimelinePanelProps) {
  const firstCustomer = items.findIndex((item) => item.kind === 'incoming');
  const event = conversation ? <StatusEvent conversation={conversation} timezone={timezone} canManageChannels={canManageChannels} /> : null;
  // The line stands at its time, before a message of the same moment (the notice it caused).
  const at = conversation?.last_status_change?.at;
  const next = at ? items.findIndex((item) => Date.parse(item.created_at) >= Date.parse(at)) : -1;
  const eventAt = !at ? -1 : next === -1 ? items.length : next;
  return (
    <section className="timeline-panel" aria-label={messages.timeline}>
      <header className="timeline-header">
        {onBack && <button type="button" className="thread-back" onClick={onBack}><ChevronLeftIcon />{messages.backToInbox}</button>}
        <div>
          <h2>{conversation?.subject ?? messages.selectConversation}</h2>
          {conversation && <div className="timeline-meta">{ownRef(conversation) && <span className="ref-copy"><span className="conv-ref">{ownRef(conversation)}</span>
            <CopyButton value={ownRef(conversation) ?? ''} className="icon-button" label={messages.copyReference(ownRef(conversation) ?? '')}><CopyIcon /></CopyButton></span>}
            <StatusLabel status={conversation.status} />
            <PriorityLabel priority={conversation.priority} /><HeaderMark conversation={conversation} timezone={timezone} />
            {channelLabel && <span className="channel-pill">{channelLabel}</span>}
            {/* Anyone can type any address into the help chat: say so, and link the chat it came from. */}
            {conversation.channel === 'help_chat' && <Part part={presenceViewPart} render={(view) => <view.HelpChatLine conversationId={conversation.id} />} />}
            <a className="timeline-contact" href={`/contacts/${conversation.contact_id}`} aria-label={messages.openContact(conversation.contact_name)}>
              {conversation.contact_name}</a></div>}
          {conversation && viewers.length > 0 && <Part part={presenceViewPart} render={(view) => <view.ConversationViewers viewers={viewers} timezone={timezone} />} />}
        </div>
        {conversation && (onSetStatus || onToggleDetails) && <div className="timeline-actions">
          {onSetStatus && <StatusButton key={conversation.id} conversation={conversation} onSetStatus={onSetStatus} />}
          {onToggleDetails && <button type="button" className="details-toggle" ref={detailsToggleRef}
            aria-expanded={detailsOpen} aria-controls="conversation-details" onClick={onToggleDetails}
            aria-label={suggestionWaiting ? `${messages.showDetails}, ${messages.aiSuggestionWaiting}` : undefined}>
            {messages.showDetails}{suggestionWaiting && <span className="details-badge" aria-hidden="true" />}</button>}
        </div>}
      </header>
      {autofix && conversation && onToggleDetails && <AutofixStrip conversationId={conversation.id} customerName={conversation.contact_name} timezone={timezone} onOpenDetails={onToggleDetails} />}
      <div className="timeline-scroll">
        <div className="timeline-stack">
          {!conversation && loadFailed && <div role="alert" className="empty-copy"><p>{messages.conversationNotLoaded}</p>
            <button type="button" onClick={onRetryLoad}>{messages.retryLoad}</button></div>}
          {loading && items.length === 0 && <p role="status" className="empty-copy">{messages.loading}</p>}
          {before}
          {items.map((item, index) => <Fragment key={item.id}>{index === eventAt && event}<MessageItem item={item} timezone={timezone} onRetryDelivery={onRetryDelivery}
            approvedBy={item.author_agent_id ? agentNames?.get(item.author_agent_id) : undefined} label={messageLabel?.(item)} menu={messageMenu?.(item)} />
            {index === firstCustomer && afterFirstCustomerMessage}</Fragment>)}
          {eventAt === items.length && event}
          {timeEntries.length > 0 && <TimeEntriesSection items={timeEntries} timezone={timezone} />}
        </div>
      </div>
    </section>
  );
}

// The last status change when something other than a person on the team made it: an AI agent, the customer,
// their Reopen command, the channel closing it by itself, an import. Only the last one is known.
function StatusEvent({ conversation, timezone, canManageChannels }: { conversation: Conversation; timezone: string; canManageChannels: boolean }) {
  const change = conversation.last_status_change;
  if (!change || change.by === 'agent') return null;
  const channel = conversation.channel_ref;
  const words = change.by === 'token' ? messages.statusEvent.token(change.name || messages.statusEvent.aiAgent, change.to)
    : change.by === 'customer' ? messages.statusEvent.customer
    : change.by === 'command' ? messages.statusEvent.command
    : change.by === 'import' ? messages.statusEvent.imported(messages.statuses[change.to])
    : <>{messages.statusEvent.autoClose}{channel && <> ({canManageChannels ? <a href={`/settings/channels/${channel.id}?tab=messages`}>{channel.name}</a> : channel.name} {messages.statusEvent.setting})</>}</>;
  const icon = change.by === 'auto_close' ? <ClockIcon /> : change.to === 'open' ? <RotateCcwIcon /> : change.to === 'resolved' ? <CheckCircleIcon /> : <CircleDotIcon />;
  return <p className="status-event">{icon}<span>{words}</span>
    <time dateTime={change.at} title={stampTitle(change.at, timezone)}>{formatDate(change.at, timezone)}{zoneSuffix(change.at, timezone)}</time></p>;
}

// The header says which clock and when in full words, since the details (with Targets) hide below 75 rem.
function HeaderMark({ conversation, timezone }: { conversation: Conversation; timezone: string }) {
  const mark = slaMark(slaOf(conversation), timezone);
  return mark ? <span className={`sla-mark sla-${mark.tone}`} aria-label={mark.full} data-tip={mark.full}><ClockIcon />{mark.header}</span> : null;
}
