import { Fragment, useCallback, useMemo, useState } from 'react';
import type { ReactNode, RefObject } from 'react';
import type { Conversation, ConversationViewer, Message, SettableStatus, TimeEntry } from '../../api/schemas';
import { messages } from '../../messages';
import { BanIcon, CheckCircleIcon, ChevronLeftIcon, CircleDotIcon, ClockIcon, CopyIcon, PanelRightIcon, RotateCcwIcon } from '../../ui/icons';
import { formatDate, stampTitle, zoneSuffix } from '../../lib/format-date';
import { CopyButton } from '../settings/copy-button';
import { Part } from '../../ui/lazy-part';
import { ActionMenu } from './action-menu';
import { MessageItem } from './message-item';
import { mediaKind } from './media-kind';
import type { GalleryItem } from './media-kind';
import { mediaViewerPart } from './media-part';
import { mayTell, noticesOf } from './status-tell';
import { presenceViewPart } from './presence-part';
import { PriorityLabel, StatusLabel } from './conversation-labels';
import { ownRef } from './conversation-ref';
import { slaMark, slaOf } from '../../lib/sla';
import { TimeEntriesSection } from './time-entries-section';
import { AutofixStrip } from '../autofix/autofix-slot';
import { useHolds } from '../../lib/permissions-context';

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
  onSetStatus?: (status: SettableStatus, options?: { notify?: boolean }) => Promise<unknown>;
  // Spam beside Resolve; on a conversation in Spam, Not spam in its place.
  onSpam?: (spam: boolean) => Promise<unknown>;
  // A chat conversation's statuses that would tell could not be read: its quiet way is offered anyway.
  noticesFailed?: boolean;
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
  messageNote?: (item: Message) => ReactNode;
}

// One press resolves the conversation without the details pane (a drawer on narrower screens); a resolved or
// closed one offers the way back. A failure says so beside the button.
type SetStatus = (status: SettableStatus, options?: { notify?: boolean }) => Promise<unknown>;

function StatusButton({ conversation, onSetStatus, noticesFailed }: { conversation: Conversation; onSetStatus: SetStatus; noticesFailed: boolean }) {
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const done = conversation.status === 'resolved' || conversation.status === 'closed';
  const target = done ? 'open' : 'resolved';
  // The server says which statuses would tell the customer now; only then is there a quiet way to offer.
  // A chat conversation whose answer is still on its way cannot be pressed: the press might tell. If it never
  // comes, the quiet way is offered beside the plain words.
  const channel = conversation.channel_ref?.name ?? messages.theChannel;
  const notices = noticesOf(conversation);
  const known = notices !== undefined;
  const tells = known && notices.includes(target);
  const waiting = !known && mayTell(conversation) && !noticesFailed;
  const quietOffered = tells || (!known && mayTell(conversation) && noticesFailed);
  const change = (quiet: boolean) => {
    if (busy || waiting) return;
    setBusy(true);
    setFailed(false);
    (quiet ? onSetStatus(target, { notify: false }) : onSetStatus(target)).catch(() => setFailed(true)).finally(() => setBusy(false));
  };
  // aria-disabled, not disabled: a disabled button drops the keyboard focus, and Reopen takes its place.
  const button = <button type="button" className="status-button" aria-disabled={busy || waiting || undefined} aria-busy={waiting || undefined} onClick={() => change(false)}
    aria-label={tells ? (done ? messages.reopenAndTellName(channel) : messages.resolveAndTellName(channel)) : undefined}>
    {done ? <RotateCcwIcon /> : <CheckCircleIcon />}{tells ? (done ? messages.reopenAndTell : messages.resolveAndTell) : done ? messages.reopen : messages.resolve}</button>;
  return <>
    {quietOffered ? <span className="status-split">{button}
      <ActionMenu label={done ? messages.moreWaysToReopen : messages.moreWaysToResolve} icon={null} disabled={busy} onApply={() => change(true)}>
        {(apply) => <button type="button" onClick={() => apply(undefined)}>{done ? <>{messages.reopenQuietly}{' '}<small>{messages.reopenQuietlyUse}</small></> : messages.resolveQuietly}</button>}
      </ActionMenu></span> : button}
    {failed && <p role="alert" className="error-copy status-button-error">{messages.statusChangeFailed}</p>}
  </>;
}

// Spam files the conversation and its sender's later mail in Spam; Not spam puts it back where it was.
function SpamButton({ spam, onSpam }: { spam: boolean; onSpam: (spam: boolean) => Promise<unknown> }) {
  const [busy, setBusy] = useState(false);
  const press = () => {
    if (busy) return;
    setBusy(true);
    onSpam(spam).finally(() => setBusy(false));
  };
  return <button type="button" className="status-button" aria-disabled={busy || undefined} data-tip={spam ? messages.spamTip : messages.notSpamTip} onClick={press}>
    {spam ? <BanIcon /> : <RotateCcwIcon />}{spam ? messages.spam : messages.notSpam}</button>;
}

export function TimelinePanel({ conversation, channelLabel, items, afterFirstCustomerMessage, timeEntries = [], loading = false, loadFailed = false, onRetryLoad, timezone, onRetryDelivery, detailsOpen = false, suggestionWaiting = false, onToggleDetails, detailsToggleRef, agentNames, viewers = [], onSetStatus, onSpam, noticesFailed = false, canManageChannels = false, onBack, autofix = false, before, messageLabel, messageMenu, messageNote }: TimelinePanelProps) {
  // A role that reads conversations but not their messages is told so instead of waiting for them.
  const readsMessages = useHolds()('messages:read');
  const firstCustomer = items.findIndex((item) => item.kind === 'incoming');
  const event = conversation ? <StatusEvent conversation={conversation} timezone={timezone} canManageChannels={canManageChannels} /> : null;
  // The line stands at its time, before a message of the same moment (the notice it caused).
  const at = conversation?.last_status_change?.at;
  const next = at ? items.findIndex((item) => Date.parse(item.created_at) >= Date.parse(at)) : -1;
  const eventAt = !at ? -1 : next === -1 ? items.length : next;
  const media = useMemo(() => galleryOf(items), [items]);
  const [opened, setOpened] = useState<string | null>(null);
  const openMedia = useCallback((attachmentId: string) => { mediaViewerPart.preload(); setOpened(attachmentId); }, []);
  const closeMedia = useCallback(() => setOpened(null), []);
  return (
    <section className="timeline-panel" aria-label={messages.timeline}>
      <header className="timeline-header">
        {onBack && <button type="button" className="thread-back" onClick={onBack}><ChevronLeftIcon /><span className="thread-back-label">{messages.backToInbox}</span></button>}
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
        {conversation && (onSetStatus || onSpam || onToggleDetails) && <div className="timeline-actions">
          {onSpam && conversation.status === 'spam' ? <SpamButton spam={false} onSpam={onSpam} />
            : <>
              {onSetStatus && <StatusButton key={conversation.id} conversation={conversation} onSetStatus={onSetStatus} noticesFailed={noticesFailed} />}
              {onSpam && <SpamButton spam onSpam={onSpam} />}
            </>}
          {onToggleDetails && <button type="button" className="details-toggle" ref={detailsToggleRef}
            aria-expanded={detailsOpen} aria-controls="conversation-details" onClick={onToggleDetails}
            aria-label={suggestionWaiting ? `${messages.showDetails}, ${messages.aiSuggestionWaiting}` : undefined}>
            <PanelRightIcon /><span className="details-toggle-label">{messages.showDetails}</span>{suggestionWaiting && <span className="details-badge" aria-hidden="true" />}</button>}
        </div>}
      </header>
      {autofix && conversation && onToggleDetails && <AutofixStrip conversationId={conversation.id} customerName={conversation.contact_name} timezone={timezone} onOpenDetails={onToggleDetails} />}
      <div className="timeline-scroll">
        <div className="timeline-stack">
          {!conversation && loadFailed && <div role="alert" className="empty-copy"><p>{messages.conversationNotLoaded}</p>
            <button type="button" onClick={onRetryLoad}>{messages.retryLoad}</button></div>}
          {conversation && !readsMessages && <p className="empty-copy">{messages.messagesClosed}</p>}
          {loading && items.length === 0 && <p role="status" className="empty-copy">{messages.loading}</p>}
          {before}
          {items.map((item, index) => <Fragment key={item.id}>{index === eventAt && event}<MessageItem item={item} timezone={timezone} onRetryDelivery={onRetryDelivery} channelName={conversation?.channel_ref?.name}
            approvedBy={item.author_agent_id ? agentNames?.get(item.author_agent_id) : undefined} label={messageLabel?.(item)} menu={messageMenu?.(item)} note={messageNote?.(item)} onOpenMedia={openMedia} />
            {index === firstCustomer && afterFirstCustomerMessage}</Fragment>)}
          {eventAt === items.length && event}
          {timeEntries.length > 0 && <TimeEntriesSection items={timeEntries} timezone={timezone} />}
        </div>
      </div>
      {opened && <MediaGalleryAt items={items} media={media} attachmentId={opened} timezone={timezone} onClose={closeMedia} />}
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

// The pictures, videos and voice clips of a conversation in timeline order, as the timeline shows them: not the
// stored raw email, and not an image embedded in a formatted email (it is part of that email, often a logo).
function galleryOf(items: Message[]): GalleryItem[] {
  return items.flatMap((item) => item.attachments
    .filter((file) => file.id !== item.email?.original_attachment_id && !file.inline)
    .flatMap((file) => {
      const kind = mediaKind(file.content_type);
      return kind ? [{ file, kind, sender: senderOf(item), at: item.created_at }] : [];
    }));
}

function senderOf(item: Message) {
  return item.author_name ?? item.email?.from ?? item.origin?.from ?? messages.someone;
}

// The gallery opens at the file pressed. An embedded image shown in the plain-text view is in no list: it opens alone.
function MediaGalleryAt({ items, media, attachmentId, timezone, onClose }: { items: Message[]; media: GalleryItem[]; attachmentId: string; timezone: string; onClose: () => void }) {
  let list = media;
  let start = media.findIndex((entry) => entry.file.id === attachmentId);
  if (start === -1) {
    const owner = items.find((item) => item.attachments.some((file) => file.id === attachmentId));
    const file = owner?.attachments.find((entry) => entry.id === attachmentId);
    const kind = file && mediaKind(file.content_type);
    if (!owner || !file || !kind) return null;
    list = [{ file, kind, sender: senderOf(owner), at: owner.created_at }];
    start = 0;
  }
  // Safari does not focus a button on a press: the gallery gives the focus back to the one that opened it by name.
  const opener = `[data-media-open="${attachmentId}"]`;
  return <Part part={mediaViewerPart} render={(viewer) => <viewer.MediaGallery items={list} start={start} timezone={timezone} onClose={onClose} opener={opener} />} />;
}
