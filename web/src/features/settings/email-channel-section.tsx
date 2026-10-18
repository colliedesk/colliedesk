import { useEffect, useRef, useState } from 'react';
import type { EmailChannelSettings, EmailDnsRecord } from '../../api/schemas';
import { formatDate } from '../../lib/format-date';
import { messages } from '../../messages';
import { useSaveFeedback } from './save-feedback';
import type { SaveState } from './save-feedback';

interface EmailChannelSectionProps {
  settings?: EmailChannelSettings;
  timezone: string;
  canManage: boolean;
  workspaceSlug?: string;
  createdSecret?: string;
  addressSave?: SaveState;
  slugSave?: SaveState;
  onSave?: (address: string) => void;
  onSaveSlug?: (slug: string) => void;
  onRotate?: () => void;
  onDisconnect?: () => void;
  onVerifyDomain?: () => void;
  // Why the last domain check did not verify; the server's own wording.
  verifyError?: string;
  verifyPending?: boolean;
}

export function EmailChannelSection({ settings, timezone, canManage, workspaceSlug, createdSecret, addressSave, slugSave, onSave, onSaveSlug, onRotate, onDisconnect, onVerifyDomain, verifyError, verifyPending }: EmailChannelSectionProps) {
  const channel = settings?.channel ?? null;
  const [address, setAddress] = useState(channel?.support_address ?? '');
  useEffect(() => setAddress(channel?.support_address ?? ''), [channel?.support_address]);
  const [slug, setSlug] = useState(workspaceSlug ?? '');
  useEffect(() => setSlug(workspaceSlug ?? ''), [workspaceSlug]);
  const addressFeedback = useSaveFeedback(addressSave);
  const slugFeedback = useSaveFeedback(slugSave);
  const [copied, setCopied] = useState(false);
  const [urlCopied, setUrlCopied] = useState(false);
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
  const copyAddress = () => {
    if (hostedAddress) void navigator.clipboard.writeText(hostedAddress).then(() => setCopied(true));
  };
  return (
    <section className="settings-card" aria-labelledby="email-channel-heading">
      <div className="section-heading"><h2 id="email-channel-heading">{messages.emailChannel}</h2><p>{messages.emailChannelDescription}</p></div>
      <div className="settings-body">
        {canManage
          ? <form className="form-row" onSubmit={(event) => { event.preventDefault(); if (!addressFeedback.pending) onSave?.(address.trim()); }}>
              <label className="field"><span>{messages.supportAddress}</span><input type="email" value={address} onChange={(event) => { addressFeedback.touch(); setAddress(event.target.value); }} required /></label>
              <button className="primary-action" type="submit" disabled={addressFeedback.pending}>{addressFeedback.saveLabel(messages.saveSupportAddress)}</button>
              {addressFeedback.status}
            </form>
          : <p><strong>{messages.supportAddress}: </strong><span>{channel ? channel.support_address : messages.emailNotConnected}</span></p>}
        {hostedAddress && <div className="email-channel-forwarding">
          <label className="invite-link"><span className="field-label">{messages.forwardingAddress}</span><input readOnly value={hostedAddress} /></label>
          <span className="button-row"><button type="button" onClick={copyAddress}>{messages.copyForwardingAddress}</button>{copied && <span role="status">{messages.addressCopied}</span>}</span>
          <p className="muted-copy">{messages.forwardingHelp}</p>
          <ul className="muted-copy"><li>{messages.forwardingGmail}</li><li>{messages.forwardingOutlook}</li></ul>
          {canManage && <form className="form-row" onSubmit={(event) => { event.preventDefault(); if (!slugFeedback.pending) onSaveSlug?.(slug.trim()); }}>
            <label className="field"><span>{messages.workspaceAddressName}</span><input value={slug} onChange={(event) => { slugFeedback.touch(); setSlug(event.target.value); }} required /></label>
            <button type="submit" disabled={slugFeedback.pending}>{slugFeedback.saveLabel(messages.saveAddressName)}</button>
            {slugFeedback.status}
            <small>{messages.addressNameWarning}</small>
          </form>}
        </div>}
        {settings && !hostedAddress && <div className="copy-field">
          <label className="invite-link"><span className="field-label">{messages.inboundURL}</span><input readOnly value={settings.inbound_url} /></label>
          <span className="button-row"><button type="button" onClick={() => void navigator.clipboard.writeText(settings.inbound_url).then(() => setUrlCopied(true))}>{messages.copyInboundURL}</button>
            {urlCopied && <span role="status">{messages.urlCopied}</span>}</span>
        </div>}
        {channel && !hostedAddress && <p className="muted-copy">{channel.secret_set && channel.secret_rotated_at
          ? messages.inboundSecretSet(formatDate(channel.secret_rotated_at, timezone)) : messages.inboundSecretMissing}</p>}
        {canManage && !hostedAddress && createdSecret && <label className="invite-link"><span className="field-label">{messages.inboundSecret}</span>
          <input readOnly value={createdSecret} /><small>{messages.inboundSecretNotice}</small></label>}
        {canManage && channel && <span className="button-row">
          {!hostedAddress && <button type="button" onClick={() => onRotate?.()}>{channel.secret_set ? messages.rotateInboundSecret : messages.createInboundSecret}</button>}
          {!confirmingDisconnect && <button type="button" className="danger-action" ref={disconnectButton} onClick={() => setConfirmingDisconnect(true)}>{messages.disconnectEmail}</button>}
        </span>}
        {canManage && channel && confirmingDisconnect && <div className="confirm-panel" role="group" aria-labelledby="disconnect-email-question">
          <p><strong id="disconnect-email-question">{messages.disconnectEmailQuestion}</strong> {messages.disconnectEmailConsequence(channel.support_address)}</p>
          <span className="button-row">
            <button type="button" className="danger-action" onClick={() => { setConfirmingDisconnect(false); onDisconnect?.(); }}>{messages.confirmDisconnectEmail}</button>
            <button type="button" autoFocus onClick={cancelDisconnect}>{messages.cancel}</button>
          </span>
        </div>}
        {channel && (channel.domain_verified_at
          ? <p className="muted-copy">{messages.domainVerified(channel.domain, formatDate(channel.domain_verified_at, timezone))}</p>
          : <div className="email-channel-dns">
              <p className="muted-copy">{settings?.domain_verification === 'provider' ? messages.providerProofHelp : messages.domainVerificationHelp}</p>
              <label className="invite-link"><span className="field-label">{messages.dnsRecordName}</span><input readOnly value={channel.dns_record_name} /></label>
              <label className="invite-link"><span className="field-label">{messages.dnsRecordValue}</span><input readOnly value={channel.dns_record_value} /></label>
              {settings?.domain_verification === 'provider' && <DnsRecordsTable records={channel.dns_records} expiredAt={channel.dns_records_expired_at} timezone={timezone} />}
              {verifyError && <p role="alert" className="error-copy">{verifyError}</p>}
              {canManage && <span className="button-row"><button type="button" disabled={verifyPending} onClick={() => onVerifyDomain?.()}>{verifyPending ? messages.verifyingDomain : messages.verifyDomain}</button></span>}
            </div>)}
        {settings?.sending_pause && <div role="alert" aria-labelledby="email-paused-heading" className="warning-note">
          <strong id="email-paused-heading">{messages.emailSendingPausedTitle}</strong>
          <p>{messages.emailSendingPausedBody(settings.sending_pause.reason, formatDate(settings.sending_pause.paused_at, timezone))}</p>
        </div>}
        {channel && (settings?.sending_from
          ? <p className="muted-copy">{messages.sendingFrom(settings.sending_from)}</p>
          : <p role="status" className="error-copy">{messages.cannotSend}</p>)}
        {smtp && (smtp.configured
          ? <p className="muted-copy"><strong>{messages.outgoingEmail}: </strong>{messages.smtpConfigured(smtp.host, smtp.port, smtp.starttls, smtp.authenticated)}</p>
          : <p role="note" className="warning-note"><strong>{messages.outgoingEmail}: </strong>{messages.smtpNotConfigured}</p>)}
      </div>
    </section>
  );
}

function DnsRecordsTable({ records, expiredAt, timezone }: { records: EmailDnsRecord[]; expiredAt: string | null; timezone: string }) {
  if (records.length === 0) return <p className="muted-copy">{expiredAt ? messages.dnsRecordsExpired(formatDate(expiredAt, timezone)) : messages.dnsRecordsNotReady}</p>;
  return <>
    <p className="muted-copy">{messages.dnsRecordsHelp}</p>
    <table aria-label={messages.dnsRecordsHeading}>
      <thead><tr><th>{messages.dnsColumnPurpose}</th><th>{messages.dnsColumnType}</th><th>{messages.dnsColumnName}</th><th>{messages.dnsColumnValue}</th><th>{messages.dnsColumnPriority}</th><th>{messages.dnsColumnStatus}</th></tr></thead>
      <tbody>{records.map((record) => <tr key={`${record.type}-${record.name}-${record.value}`}>
        <td>{record.record}</td><td>{record.type}</td><td>{record.name}</td><td><code>{record.value}</code></td>
        <td>{record.priority ?? ''}</td><td>{messages.dnsRecordStatus(record.status)}</td>
      </tr>)}</tbody>
    </table>
  </>;
}
