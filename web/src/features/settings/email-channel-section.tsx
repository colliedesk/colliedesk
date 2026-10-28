import { useEffect, useId, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import type { EmailChannelSettings, EmailDnsRecord } from '../../api/schemas';
import { formatDate } from '../../lib/format-date';
import { messages } from '../../messages';
import { channelSetupMessages } from './channel-setup-messages';
import { PartUnavailable } from '../../ui/lazy-part';
import type { MailboxProvider, MailboxReturn } from './gmail-return';
import { CopyButton } from './copy-button';
import { InboundURLGuide } from './inbound-url-guide';
import { ServerDetails } from './server-details';
import { NameForm } from './name-form';
import { useSaveFeedback } from './save-feedback';
import type { SaveState } from './save-feedback';
import { gmailPart } from './gmail-part';

// The block's chunk lives in gmail-part.ts; this file keeps the name its callers and tests import.
export { gmailPart };

interface EmailChannelSectionProps {
  settings?: EmailChannelSettings;
  timezone: string;
  canManage: boolean;
  workspaceSlug?: string;
  createdSecret?: string;
  addressSave?: SaveState;
  senderNameSave?: SaveState;
  slugSave?: SaveState;
  onSave?: (address: string) => void;
  // addressConflictLink, for an address another channel has (addressSave.conflict), is a link to it.
  addressConflictLink?: (conflict: NonNullable<SaveState['conflict']>) => ReactNode;
  onSaveSenderName?: (name: string) => void;
  onSaveSlug?: (slug: string) => void;
  onRotate?: () => void;
  onDisconnect?: () => void;
  onVerifyDomain?: () => void;
  // Why the last domain check did not verify; the server's own wording.
  verifyError?: string;
  verifyPending?: boolean;
  // The channel's mailbox (Gmail or Microsoft 365): connect, disconnect, the provider whose sign-in is
  // opening, why the last action failed and the outcome a provider sent back.
  onConnectMailbox?: (provider: MailboxProvider) => void;
  onDisconnectMailbox?: (provider: MailboxProvider) => void;
  mailboxPending?: MailboxProvider;
  mailboxError?: string;
  mailboxReturned?: MailboxReturn;
  // part shows one half of the channel as a channel tab does, without the section frame: how mail arrives
  // (connection) or how replies leave (replies). All, the default, is the whole section.
  part?: 'all' | 'connection' | 'replies';
}

export function EmailChannelSection({ settings, timezone, canManage, workspaceSlug, createdSecret, addressSave, senderNameSave, slugSave, onSave, onSaveSenderName, onSaveSlug, onRotate, onDisconnect, onVerifyDomain, verifyError, verifyPending,
  onConnectMailbox, onDisconnectMailbox, mailboxPending, mailboxError, mailboxReturned, part = 'all', addressConflictLink }: EmailChannelSectionProps) {
  const channel = settings?.channel ?? null;
  // A connected mailbox sends by itself; a broken one says so in its own block.
  const mailboxConnection = channel ? settings?.gmail.connection ?? settings?.microsoft.connection ?? null : null;
  const gmail = gmailPart.use();
  const [address, setAddress] = useState(channel?.support_address ?? '');
  useEffect(() => setAddress(channel?.support_address ?? ''), [channel?.support_address]);
  const [slug, setSlug] = useState(workspaceSlug ?? '');
  useEffect(() => setSlug(workspaceSlug ?? ''), [workspaceSlug]);
  const addressError = useId();
  const addressField = useRef<HTMLInputElement>(null);
  const addressFeedback = useSaveFeedback(addressSave, addressError, addressField, address);
  const slugFeedback = useSaveFeedback(slugSave);
  // Disconnecting stops inbound mail for everyone, so it asks first; Cancel is the focused default.
  const [confirmingDisconnect, setConfirmingDisconnect] = useState(false);
  const disconnectButton = useRef<HTMLButtonElement>(null);
  const returnFocus = useRef(false);
  const cancelDisconnect = () => { returnFocus.current = true; setConfirmingDisconnect(false); };
  useEffect(() => {
    if (confirmingDisconnect || !returnFocus.current) return;
    returnFocus.current = false;
    disconnectButton.current?.focus();
  }, [confirmingDisconnect]);
  const smtp = settings?.smtp;
  const hostedAddress = settings?.hosted_address ?? null;
  const mailboxSending = mailboxConnection?.status === 'connected' &&
    (mailboxConnection.provider === 'microsoft' ? settings?.microsoft.available : settings?.gmail.available) === true;
  const dnsVerification = channel && <div className="email-channel-dns">
    <p className="muted-copy">{settings?.domain_verification === 'provider' ? messages.providerProofHelp : messages.domainVerificationHelp}</p>
    <label className="invite-link"><span className="field-label">{messages.dnsRecordName}</span><input readOnly value={channel.dns_record_name} /></label>
    <label className="invite-link"><span className="field-label">{messages.dnsRecordValue}</span><input readOnly value={channel.dns_record_value} /></label>
    {settings?.domain_verification === 'provider' && <DnsRecordsTable records={channel.dns_records} expiredAt={channel.dns_records_expired_at} timezone={timezone} />}
    {verifyError && <p role="alert" className="error-copy">{verifyError}</p>}
    {canManage && <span className="button-row"><button type="button" disabled={verifyPending} onClick={() => onVerifyDomain?.()}>{verifyPending ? messages.verifyingDomain : messages.verifyDomain}</button></span>}
  </div>;
  const addressPart = <>
        {canManage
          ? <form className="form-row" onSubmit={(event) => { event.preventDefault(); if (!addressFeedback.pending) onSave?.(address.trim()); }}>
              <label className="field"><span>{messages.supportAddress}</span><input ref={addressField} type="email" value={address} aria-invalid={addressFeedback.invalid || undefined}
                aria-describedby={addressFeedback.invalid ? addressError : undefined} onChange={(event) => { addressFeedback.touch(); setAddress(event.target.value); }} required /></label>
              <button className="primary-action" type="submit" disabled={addressFeedback.pending}>{addressFeedback.saveLabel(messages.saveSupportAddress)}</button>
              {addressFeedback.status}
              {addressFeedback.invalid && addressSave?.conflict && addressConflictLink?.(addressSave.conflict)}
            </form>
          : <p><strong>{messages.supportAddress}: </strong><span>{channel ? channel.support_address : messages.emailNotConnected}</span></p>}
  </>;
  const senderPart = <>
        {channel && <NameForm label={messages.senderName} saveLabel={messages.saveSenderName} hint={messages.senderNameHelp}
          value={channel.sender_name} canManage={canManage} save={senderNameSave} onSave={onSaveSenderName} />}
  </>;
  const arrivalPart = <>
        {hostedAddress && <div className="email-channel-forwarding">
          <div className="copy-field">
            <label className="invite-link"><span className="field-label">{messages.forwardingAddress}</span><input readOnly value={hostedAddress} /></label>
            <span className="button-row"><CopyButton value={hostedAddress} copiedText={messages.addressCopied}>{messages.copyForwardingAddress}</CopyButton></span>
          </div>
          <p className="muted-copy">{messages.forwardingHelp}</p>
          <ul className="muted-copy"><li>{messages.forwardingGmail}</li><li>{messages.forwardingOutlook}</li></ul>
          {canManage && onSaveSlug && <form className="form-row" onSubmit={(event) => { event.preventDefault(); if (!slugFeedback.pending) onSaveSlug(slug.trim()); }}>
            <label className="field"><span>{messages.workspaceAddressName}</span><input value={slug} onChange={(event) => { slugFeedback.touch(); setSlug(event.target.value); }} required /></label>
            <button type="submit" disabled={slugFeedback.pending}>{slugFeedback.saveLabel(messages.saveAddressName)}</button>
            {slugFeedback.status}
            <small>{messages.addressNameWarning}</small>
          </form>}
        </div>}
        {settings && !hostedAddress && <div className="copy-field">
          <label className="invite-link"><span className="field-label">{messages.inboundURL}</span><input readOnly value={settings.inbound_url} /></label>
          <span className="button-row"><CopyButton value={settings.inbound_url} copiedText={messages.urlCopied}>{messages.copyInboundURL}</CopyButton></span>
        </div>}
        {channel && !hostedAddress && <p className="muted-copy">{channel.secret_set && channel.secret_rotated_at
          ? messages.inboundSecretSet(formatDate(channel.secret_rotated_at, timezone)) : channelSetupMessages.inboundSecretMissing}</p>}
        {canManage && !hostedAddress && createdSecret && <label className="invite-link"><span className="field-label">{messages.inboundSecret}</span>
          <input readOnly value={createdSecret} /><small>{messages.inboundSecretNotice}</small></label>}
        {canManage && channel && settings && !hostedAddress && <InboundURLGuide inboundURL={settings.inbound_url} supportAddress={channel.support_address} secret={createdSecret} />}
        {canManage && channel && <span className="button-row">
          {!hostedAddress && <button type="button" onClick={() => onRotate?.()}>{channel.secret_set ? messages.rotateInboundSecret : messages.createInboundSecret}</button>}
          {!confirmingDisconnect && onDisconnect && <button type="button" className="danger-action" ref={disconnectButton} onClick={() => setConfirmingDisconnect(true)}>{messages.disconnectEmail}</button>}
        </span>}
        {canManage && channel && confirmingDisconnect && <div className="confirm-panel" role="group" aria-labelledby="disconnect-email-question"
          onKeyDown={(event) => { if (event.key === 'Escape') cancelDisconnect(); }}>
          <p><strong id="disconnect-email-question">{messages.disconnectEmailQuestion}</strong> {messages.disconnectEmailConsequence(channel.support_address)}</p>
          <span className="button-row">
            <button type="button" className="danger-action" onClick={() => { setConfirmingDisconnect(false); onDisconnect?.(); }}>{messages.confirmDisconnectEmail}</button>
            <button type="button" autoFocus onClick={cancelDisconnect}>{messages.cancel}</button>
          </span>
        </div>}
  </>;
  const domainPart = <>
        {channel && (channel.domain_verified_at
          ? <p className="muted-copy">{messages.domainVerified(channel.domain, formatDate(channel.domain_verified_at, timezone))} {sharedDomain(channel.domain)}</p>
          : mailboxSending
            // Replies through a mailbox need no verified domain; the records stay one click away for SMTP.
            ? <details className="email-channel-dns-folded" open={Boolean(verifyError) || undefined}>
                <summary>{messages.domainVerificationNotNeeded}</summary>
                {dnsVerification}
              </details>
            : dnsVerification)}
  </>;
  const pausePart = <>
        {settings?.sending_pause && <div role="alert" aria-labelledby="email-paused-heading" className="warning-note">
          <strong id="email-paused-heading">{messages.emailSendingPausedTitle}</strong>
          <p>{messages.emailSendingPausedBody(settings.sending_pause.reason, formatDate(settings.sending_pause.paused_at, timezone))}</p>
        </div>}
  </>;
  const sendingPart = <>
        {channel && !mailboxConnection && (settings?.sending_from
          ? <p className="muted-copy">{messages.sendingFrom(settings.sending_from)}</p>
          : <p role="status" className="error-copy">{messages.cannotSend}</p>)}
  </>;
  const gmailBlock = <>
        {channel && settings && (gmail.status === 'ready'
          ? <gmail.module.MailboxConnection gmail={settings.gmail} microsoft={settings.microsoft} supportAddress={channel.support_address} timezone={timezone}
              canManage={canManage} onConnect={onConnectMailbox} onDisconnect={onDisconnectMailbox} connectPending={mailboxPending} error={mailboxError}
              returned={mailboxReturned} />
          : gmail.status === 'error'
            ? <PartUnavailable />
            // Holds the block's place while its chunk arrives, so what follows does not jump.
            : <div className="gmail-connection gmail-connection-placeholder" aria-hidden="true" />)}
  </>;
  const smtpPart = <>
        {smtp && (smtp.configured
          ? <><p className="muted-copy">{messages.smtpPlain}</p>
            {canManage && <ServerDetails><p className="muted-copy"><strong>{messages.outgoingEmail}: </strong>{messages.smtpConfigured(smtp.host, smtp.port, smtp.starttls, smtp.authenticated)}</p></ServerDetails>}</>
          : !mailboxConnection && <p role="note" className="warning-note"><strong>{messages.outgoingEmail}: </strong>{channelSetupMessages.smtpNotConfigured}</p>)}
  </>;
  const repliesPart = <>{domainPart}{pausePart}{sendingPart}{gmailBlock}{smtpPart}</>;
  if (part === 'connection') return <div className="settings-body">{addressPart}{arrivalPart}</div>;
  // The Replies tab groups the same blocks under headings: how replies leave now first, then the name,
  // the domain and the mailbox.
  if (part === 'replies') return <div className="settings-body email-replies">
    <section aria-labelledby="replies-leave-heading"><h3 id="replies-leave-heading">{repliesHeadings.leave}</h3>{pausePart}{sendingPart}{smtpPart}</section>
    {channel && <section aria-labelledby="replies-name-heading"><h3 id="replies-name-heading">{repliesHeadings.name}</h3>{senderPart}</section>}
    {channel && <section aria-labelledby="replies-domain-heading"><h3 id="replies-domain-heading">{repliesHeadings.domain}</h3>{domainPart}</section>}
    {gmailBlock}
  </div>;
  return (
    <section className="settings-card" aria-labelledby="email-channel-heading">
      <div className="section-heading"><h2 id="email-channel-heading">{messages.emailChannel}</h2><p>{channelSetupMessages.emailChannelDescription}</p></div>
      <div className="settings-body">
        {addressPart}{senderPart}{arrivalPart}{repliesPart}
      </div>
    </section>
  );
}

// sharedDomain says a verification is the domain's: every mailbox on it shares it.
const sharedDomain = (domain: string) => `The verification is for ${domain}: every mailbox on it shares it.`;

// Headings of the Replies tab, which the Settings page did not have.
const repliesHeadings = { leave: 'How replies leave', name: 'The name customers see', domain: 'Your domain' };

function DnsRecordsTable({ records, expiredAt, timezone }: { records: EmailDnsRecord[]; expiredAt: string | null; timezone: string }) {
  if (records.length === 0) return <p className="muted-copy">{expiredAt ? messages.dnsRecordsExpired(formatDate(expiredAt, timezone)) : messages.dnsRecordsNotReady}</p>;
  return <>
    <p className="muted-copy">{messages.dnsRecordsHelp}</p>
    {/* A narrow screen scrolls the table sideways inside its own frame, faded at the edges. */}
    <div className="scroll-x-fade"><table aria-label={messages.dnsRecordsHeading}>
      <thead><tr><th>{messages.dnsColumnPurpose}</th><th>{messages.dnsColumnType}</th><th>{messages.dnsColumnName}</th><th>{messages.dnsColumnValue}</th><th>{messages.dnsColumnPriority}</th><th>{messages.dnsColumnStatus}</th></tr></thead>
      <tbody>{records.map((record) => <tr key={`${record.type}-${record.name}-${record.value}`}>
        <td>{record.record}</td><td>{record.type}</td><td>{record.name}</td><td><code>{record.value}</code></td>
        <td>{record.priority ?? ''}</td><td>{messages.dnsRecordStatus(record.status)}</td>
      </tr>)}</tbody>
    </table></div>
  </>;
}
