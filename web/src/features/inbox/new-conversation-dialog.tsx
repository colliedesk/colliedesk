import { useId, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { api, ConflictError, InvalidRequestError } from '../../api/client';
import { channelsApi } from '../../api/channels';
import { contactApi, existingContact } from '../contacts/contact-api';
import type { ConversationContact, FirstReply } from '../contacts/contact-api';
import { OutboundRefusedError } from '../../api/outbound-error';
import type { Contact } from '../../api/schemas';
import { messages } from '../../messages';
import { contactAddress, contactMessages } from '../contacts/contact-messages';
import { dialogKeys, keepDialogFocus, useReturnFocus } from '../../ui/dialog-keys';
import { RouterLink } from '../../ui/router-link';
import { ContactPicker } from '../contacts/contact-picker';
import { AttachmentPicker, attachmentProblem } from './attachment-picker';
import type { AttachmentLimits } from './attachment-picker';
import { CcField } from './cc-field';
import { replyRefusalText } from './refusal-text';

// Why a new conversation cannot be sent at all, known before the person writes anything.
export type NewConversationBlock = 'no_channel' | 'trial' | undefined;

// StartConversationWindow is what the inbox opens: the window, with what it needs read from the queries
// the inbox already holds (plan, email channel, file limits), the call that starts the conversation, and
// the way to it once it exists. It lives in this chunk so the inbox bundle carries none of it.
export function StartConversationWindow({ onClose, onStarted }: { onClose: () => void; onStarted: (conversationId: string) => void }) {
  const client = useQueryClient();
  const navigate = useNavigate();
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  // The email channels a conversation can start from; the type's default is chosen until the person picks another.
  const list = useQuery({ queryKey: ['channels'], queryFn: channelsApi.list });
  const senders: SendingChannel[] = (list.data?.items ?? []).filter((item) => item.type === 'email' && !item.deleted_at)
    .map((item) => ({ id: item.id, name: item.name, off: !item.enabled }));
  const [chosen, setChosen] = useState<string>();
  const channelID = chosen ?? list.data?.types.find((info) => info.type === 'email')?.default_channel_id ?? undefined;
  const channel = useQuery({ queryKey: ['email-channel', channelID], queryFn: () => channelsApi.email(channelID ?? ''), enabled: Boolean(channelID) });
  const limits = useQuery({ queryKey: ['public-config'], queryFn: api.config }).data?.limits;
  // On the trial a new conversation reaches a customer who never wrote, which only a verified sending
  // domain or a plan allows; the server stays the judge, this only says so before anything is typed.
  const blocked: NewConversationBlock = list.data && !channelID ? 'no_channel'
    : me.data?.plan?.plan === 'trial' && channel.data && !channel.data.channel?.domain_verified_at ? 'trial' : undefined;
  const start = async (contact: ConversationContact, subject: string, reply: FirstReply) => {
    // A channel removed or turned off while the window was open is shown as it is now.
    const created = await contactApi.startConversation(contact, subject, { ...reply, channelID }).catch((error: unknown) => {
      if (channelRefusal(error)) void client.invalidateQueries({ queryKey: ['channels'] });
      throw error;
    });
    client.setQueryData(['conversations', 'one', created.id], created);
    void client.invalidateQueries({ queryKey: ['conversations'] });
    void client.invalidateQueries({ queryKey: ['contact', created.contact_id] });
    // A customer added with the conversation is new to every search.
    if (typeof contact !== 'string') void client.invalidateQueries({ queryKey: ['contacts'] });
    onStarted(created.id);
    void navigate({ to: '/conversations/$conversationId', params: { conversationId: created.id } });
  };
  const settingsHref = channelID ? `/settings/channels/${encodeURIComponent(channelID)}?tab=replies` : '/settings/channels/type/email?tab=replies';
  return <NewConversationDialog blocked={blocked} checking={me.isPending || list.isPending || (Boolean(channelID) && channel.isPending)} onClose={onClose} onStart={start}
    senders={senders} channelID={channelID} onChooseChannel={setChosen} settingsHref={settingsHref}
    attachmentLimits={limits ? { bytes: limits.outgoing_attachments_bytes, files: limits.outgoing_attachments_files } : undefined}
    onOpenSettings={() => void navigate({ href: settingsHref })} onChoosePlan={() => void navigate({ to: '/settings/billing', search: { checkout: '' } })} />;
}

// An email channel a conversation can start from; one that is off is listed but cannot be chosen.
export interface SendingChannel { id: string; name: string; off: boolean }

// channelRefusal is the server refusing the chosen channel: removed (404) or turned off (409 channel_off).
function channelRefusal(error: unknown): boolean {
  return (error instanceof ConflictError && error.detail?.code === 'channel_off') || (error instanceof Error && error.message === 'API 404');
}

export interface NewConversationDialogProps {
  blocked?: NewConversationBlock;
  // The email channels of the workspace and the one sending; the choice is offered when there are two or more.
  senders?: SendingChannel[];
  channelID?: string;
  onChooseChannel?: (id: string) => void;
  // Where the blocked notice leads: the sending channel's settings.
  settingsHref?: string;
  // The plan or the email channel is still loading, so whether the email may be sent is not known yet.
  checking?: boolean;
  attachmentLimits?: AttachmentLimits;
  // Resolves once the conversation exists and its email is queued; rejects when nothing was stored. A
  // customer added here is written with the conversation, so a refusal leaves no contact behind either.
  onStart: (contact: ConversationContact, subject: string, reply: FirstReply) => Promise<unknown>;
  // Where a blocked window sends the person: Settings (email channel, sending domain) and the plans.
  onOpenSettings?: () => void;
  onChoosePlan?: () => void;
  onClose: () => void;
}

const plainAddress = /^[^\s@<>,;"()[\]\\]+@[^\s@<>,;"()[\]\\]+\.[^\s@<>,;"()[\]\\]+$/;

function startRefusalText(error: unknown): string {
  if (error instanceof OutboundRefusedError && error.code === 'trial_outbound_restricted' && error.refusedCc.length === 0) return contactMessages.newConversationRefused;
  if (error instanceof OutboundRefusedError) return replyRefusalText(error);
  // The server's words for a refusal it explains: no email channel, a merged contact, a bad Cc.
  if (error instanceof InvalidRequestError) return error.message;
  const text = replyRefusalText(error);
  return text === messages.replyNotSent ? contactMessages.newConversationNotSent : text;
}

// NewConversationDialog starts a conversation by email: the customer (found or added), a subject, the
// message, Cc and files. Nothing typed is lost when the server refuses.
export function NewConversationDialog({ blocked, checking = false, senders = [], channelID, onChooseChannel, settingsHref = '/settings/channels/type/email?tab=replies',
  attachmentLimits, onStart, onOpenSettings, onChoosePlan, onClose }: NewConversationDialogProps) {
  useReturnFocus('[data-opens="new-conversation"]');
  const [contact, setContact] = useState<Contact>();
  const [adding, setAdding] = useState(false);
  const [name, setName] = useState('');
  const [email, setEmail] = useState('');
  const [subject, setSubject] = useState('');
  const [body, setBody] = useState('');
  const [cc, setCc] = useState<string[]>([]);
  const [ccBlocking, setCcBlocking] = useState(false);
  const [files, setFiles] = useState<File[]>([]);
  const [sending, setSending] = useState(false);
  const [problem, setProblem] = useState<string>();
  // The server refused the chosen channel; said at the choice, with the text kept.
  const [channelProblem, setChannelProblem] = useState<string>();
  // An address that is already a customer: the contact to use instead.
  const [existing, setExisting] = useState<Contact>();
  const root = useRef<HTMLDivElement>(null);
  const subjectField = useRef<HTMLInputElement>(null);
  const titleId = useId();
  const nameId = useId();
  const emailId = useId();
  const emailHintId = useId();
  const subjectId = useId();
  const bodyId = useId();
  const missingId = useId();
  const fromId = useId();
  const fromProblemId = useId();
  const fileProblem = attachmentProblem(files, attachmentLimits);
  const emailInvalid = email.trim() !== '' && !plainAddress.test(email.trim());
  const newContactValid = name.trim() !== '' && plainAddress.test(email.trim());
  // What still stops the send, said next to the button so a disabled Send is never silent.
  const missing = [
    ...(contact || (adding && newContactValid) ? [] : [contactMessages.missingCustomer]),
    ...(subject.trim() ? [] : [contactMessages.missingSubject]),
    ...(body.trim() ? [] : [contactMessages.missingMessage]),
  ];
  // Nothing is written until the window knows the email may be sent, so no text is typed for nothing.
  const closed = Boolean(blocked) || checking;
  const ready = !closed && missing.length === 0 && !fileProblem && !ccBlocking;
  // Escape closes only an empty window; once something is written, only Cancel throws it away.
  const written = [subject, body, name, email].some((text) => text.trim() !== '') || cc.length > 0 || files.length > 0;
  const closeUnlessWritten = () => { if (!written) onClose(); };
  const pick = (picked: Contact) => {
    setContact(picked);
    setAdding(false);
    setExisting(undefined);
    setProblem(undefined);
    // The next thing to write; the button that was clicked is gone.
    requestAnimationFrame(() => subjectField.current?.focus());
  };
  const send = async () => {
    if (!ready || sending) return;
    setSending(true);
    setProblem(undefined);
    setChannelProblem(undefined);
    setExisting(undefined);
    try {
      const to: ConversationContact = contact ? contact.id : { name: name.trim(), email: email.trim() };
      await onStart(to, subject.trim(), { body: body.trim(), files, cc });
      onClose();
    } catch (error) {
      const known = existingContact(error);
      if (known) setExisting(known);
      else if (channelRefusal(error) && senders.length > 1) setChannelProblem(error instanceof ConflictError ? error.message : contactMessages.newConversationChannelGone);
      else setProblem(startRefusalText(error));
    } finally {
      setSending(false);
    }
  };
  return (
    <div className="dialog-backdrop" onMouseDown={keepDialogFocus}>
      <div className="dialog new-conversation" role="dialog" aria-modal="true" aria-labelledby={titleId} ref={root} onKeyDown={dialogKeys(root, closeUnlessWritten)}>
        <h2 id={titleId}>{contactMessages.newConversationTitle}</h2>
        {blocked && <p role="status" className="composer-notice">
          {blocked === 'trial' ? contactMessages.newConversationTrial : contactMessages.newConversationNeedsChannel}{' '}
          <RouterLink href={settingsHref} onFollow={onOpenSettings && (() => { onClose(); onOpenSettings(); })}>
            {blocked === 'trial' ? contactMessages.verifySendingDomain : contactMessages.connectEmailChannel}</RouterLink>
          {blocked === 'trial' && <>{' · '}<RouterLink href="/settings/billing" onFollow={onChoosePlan && (() => { onClose(); onChoosePlan(); })}>{messages.choosePlan}</RouterLink></>}
        </p>}
        <form className="new-conversation-form" onSubmit={(event) => { event.preventDefault(); void send(); }}>
          {/* Nothing can be sent while the workspace is blocked, so nothing can be written either. */}
          {senders.length > 1 && <div className="new-conversation-from">
            <label htmlFor={fromId}>{contactMessages.newConversationFrom}</label>
            <select id={fromId} value={channelID ?? ''} disabled={sending} aria-invalid={channelProblem ? true : undefined} aria-describedby={channelProblem ? fromProblemId : undefined}
              onChange={(event) => { setChannelProblem(undefined); onChooseChannel?.(event.target.value); }}>
              {senders.map((sender) => <option key={sender.id} value={sender.id} disabled={sender.off}>{sender.off ? contactMessages.newConversationFromOff(sender.name) : sender.name}</option>)}
            </select>
            {channelProblem && <p id={fromProblemId} role="alert" className="error-copy">{channelProblem}</p>}
          </div>}
          <fieldset className="new-conversation-fields" disabled={closed}>
            {contact && <div className="new-conversation-to">
              <span className="field-label">{contactMessages.newConversationTo}</span>
              <span>{contactMessages.newConversationPick(contact.name, contactAddress(contact))}</span>
              {!contact.email && <span className="muted-copy">{contactMessages.noEmailReplies}</span>}
              <button type="button" disabled={sending} onClick={() => setContact(undefined)}>{contactMessages.newConversationChange}</button>
            </div>}
            {!contact && !adding && <>
              <ContactPicker label={contactMessages.newConversationFind} autoFocus={!closed} disabled={sending || closed} onPick={pick} emptyText={contactMessages.newConversationNoMatch} />
              <div className="button-row"><button type="button" disabled={sending} onClick={() => setAdding(true)}>{contactMessages.newConversationAddContact}</button></div>
            </>}
            {!contact && adding && <fieldset className="new-conversation-contact" disabled={sending}>
              <legend>{contactMessages.newConversationAddContact}</legend>
              <label htmlFor={nameId}>{contactMessages.newConversationContactName}</label>
              <input id={nameId} value={name} autoFocus maxLength={200} autoComplete="off" onChange={(event) => setName(event.target.value)} />
              <label htmlFor={emailId}>{contactMessages.newConversationContactEmail}</label>
              <input id={emailId} type="email" value={email} maxLength={254} autoComplete="off" aria-invalid={emailInvalid || existing ? true : undefined}
                aria-describedby={emailInvalid || existing ? emailHintId : undefined} onChange={(event) => { setEmail(event.target.value); setExisting(undefined); }} />
              {emailInvalid && <p id={emailHintId} className="error-copy">{contactMessages.newConversationContactInvalid}</p>}
              {existing && <div id={emailHintId} className="new-conversation-existing" role="alert">
                <p className="error-copy">{contactMessages.newConversationExists(existing.name, contactAddress(existing))}</p>
                <button type="button" onClick={() => pick(existing)}>{contactMessages.newConversationUse(existing.name)}</button>
              </div>}
              <div className="button-row"><button type="button" onClick={() => { setAdding(false); setExisting(undefined); }}>{contactMessages.newConversationBackToSearch}</button></div>
            </fieldset>}
            <label htmlFor={subjectId}>{contactMessages.newConversationSubject}</label>
            <input id={subjectId} ref={subjectField} value={subject} maxLength={500} disabled={sending} onChange={(event) => setSubject(event.target.value)} />
            <label htmlFor={bodyId}>{contactMessages.newConversationBody}</label>
            <textarea id={bodyId} rows={8} value={body} disabled={sending} onChange={(event) => setBody(event.target.value)}
              onKeyDown={(event) => { if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) { event.preventDefault(); void send(); } }} />
            <CcField value={cc} onChange={setCc} disabled={sending} onSend={() => void send()} onBlockingChange={setCcBlocking} />
            <AttachmentPicker files={files} onChange={setFiles} limits={attachmentLimits} disabled={sending} labelled />
          </fieldset>
          {problem && <p role="alert" className="error-copy">{problem}</p>}
          {!closed && missing.length > 0 && <p id={missingId} className="muted-copy">{contactMessages.newConversationMissing(missing)}</p>}
          <div className="button-row">
            <button type="submit" className="primary-action" disabled={!ready || sending}
              aria-describedby={!closed && missing.length > 0 ? missingId : undefined}>{sending ? contactMessages.newConversationSending : contactMessages.newConversationSend}</button>
            <button type="button" disabled={sending} onClick={onClose}>{messages.cancel}</button>
          </div>
        </form>
      </div>
    </div>
  );
}
