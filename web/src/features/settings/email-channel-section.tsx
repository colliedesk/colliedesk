import { useEffect, useRef, useState } from 'react';
import type { EmailChannelSettings } from '../../api/schemas';
import { formatDate } from '../../lib/format-date';
import { messages } from '../../messages';

interface EmailChannelSectionProps {
  settings?: EmailChannelSettings;
  timezone: string;
  canManage: boolean;
  workspaceSlug?: string;
  createdSecret?: string;
  onSave?: (address: string) => void;
  onSaveSlug?: (slug: string) => void;
  onRotate?: () => void;
  onDisconnect?: () => void;
  onVerifyDomain?: () => void;
}

export function EmailChannelSection({ settings, timezone, canManage, workspaceSlug, createdSecret, onSave, onSaveSlug, onRotate, onDisconnect, onVerifyDomain }: EmailChannelSectionProps) {
  const channel = settings?.channel ?? null;
  const [address, setAddress] = useState(channel?.support_address ?? '');
  useEffect(() => setAddress(channel?.support_address ?? ''), [channel?.support_address]);
  const [slug, setSlug] = useState(workspaceSlug ?? '');
  useEffect(() => setSlug(workspaceSlug ?? ''), [workspaceSlug]);
  const [copied, setCopied] = useState(false);
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
          ? <form className="form-row" onSubmit={(event) => { event.preventDefault(); onSave?.(address.trim()); }}>
              <label className="field"><span>{messages.supportAddress}</span><input type="email" value={address} onChange={(event) => setAddress(event.target.value)} required /></label>
              <button className="primary-action" type="submit">{messages.saveSupportAddress}</button>
            </form>
          : <p><strong>{messages.supportAddress}: </strong><span>{channel ? channel.support_address : messages.emailNotConnected}</span></p>}
        {hostedAddress && <div className="email-channel-forwarding">
          <label className="invite-link"><span className="field-label">{messages.forwardingAddress}</span><input readOnly value={hostedAddress} /></label>
          <span className="button-row"><button type="button" onClick={copyAddress}>{messages.copyForwardingAddress}</button>{copied && <span role="status">{messages.addressCopied}</span>}</span>
          <p className="muted-copy">{messages.forwardingHelp}</p>
          <ul className="muted-copy"><li>{messages.forwardingGmail}</li><li>{messages.forwardingOutlook}</li></ul>
          {canManage && <form className="form-row" onSubmit={(event) => { event.preventDefault(); onSaveSlug?.(slug.trim()); }}>
            <label className="field"><span>{messages.workspaceAddressName}</span><input value={slug} onChange={(event) => setSlug(event.target.value)} required /></label>
            <button type="submit">{messages.saveAddressName}</button>
            <small>{messages.addressNameWarning}</small>
          </form>}
        </div>}
        {settings && !hostedAddress && <label className="invite-link"><span className="field-label">{messages.inboundURL}</span><input readOnly value={settings.inbound_url} /></label>}
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
          : <div className="email-channel-dns"><p className="muted-copy">{messages.domainVerificationHelp}</p>
              <label className="invite-link"><span className="field-label">{messages.dnsRecordName}</span><input readOnly value={channel.dns_record_name} /></label>
              <label className="invite-link"><span className="field-label">{messages.dnsRecordValue}</span><input readOnly value={channel.dns_record_value} /></label>
              {canManage && <span className="button-row"><button type="button" onClick={() => onVerifyDomain?.()}>{messages.verifyDomain}</button></span>}
            </div>)}
        {channel && (settings?.sending_from
          ? <p className="muted-copy">{messages.sendingFrom(settings.sending_from)}</p>
          : <p role="status" className="error-copy">{messages.cannotSend}</p>)}
        {smtp && <p className="muted-copy"><strong>{messages.outgoingEmail}: </strong>
          {smtp.configured ? messages.smtpConfigured(smtp.host, smtp.port, smtp.starttls, smtp.authenticated) : messages.smtpNotConfigured}</p>}
      </div>
    </section>
  );
}
