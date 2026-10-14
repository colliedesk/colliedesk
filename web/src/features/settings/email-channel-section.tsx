import { useEffect, useState } from 'react';
import type { EmailChannelSettings } from '../../api/schemas';
import { formatDate } from '../../lib/format-date';
import { messages } from '../../messages';

interface EmailChannelSectionProps {
  settings?: EmailChannelSettings;
  timezone: string;
  canManage: boolean;
  createdSecret?: string;
  onSave?: (address: string) => void;
  onRotate?: () => void;
  onDisconnect?: () => void;
  onVerifyDomain?: () => void;
}

export function EmailChannelSection({ settings, timezone, canManage, createdSecret, onSave, onRotate, onDisconnect, onVerifyDomain }: EmailChannelSectionProps) {
  const channel = settings?.channel ?? null;
  const [address, setAddress] = useState(channel?.support_address ?? '');
  useEffect(() => setAddress(channel?.support_address ?? ''), [channel?.support_address]);
  const smtp = settings?.smtp;
  return (
    <section aria-labelledby="email-channel-heading">
      <div className="section-heading"><div><h2 id="email-channel-heading">{messages.emailChannel}</h2><p>{messages.emailChannelDescription}</p></div></div>
      <div className="email-channel">
        {canManage
          ? <form className="invite-form" onSubmit={(event) => { event.preventDefault(); onSave?.(address.trim()); }}>
              <label><span>{messages.supportAddress}</span><input type="email" value={address} onChange={(event) => setAddress(event.target.value)} required /></label>
              <span />
              <button className="primary-action" type="submit">{messages.saveSupportAddress}</button>
            </form>
          : <p><strong>{messages.supportAddress}: </strong><span>{channel ? channel.support_address : messages.emailNotConnected}</span></p>}
        {settings && <label className="invite-link"><span>{messages.inboundURL}</span><input readOnly value={settings.inbound_url} /></label>}
        {channel && <p className="muted-copy">{channel.secret_set && channel.secret_rotated_at
          ? messages.inboundSecretSet(formatDate(channel.secret_rotated_at, timezone)) : messages.inboundSecretMissing}</p>}
        {canManage && createdSecret && <label className="invite-link"><span>{messages.inboundSecret}</span>
          <input readOnly value={createdSecret} /><small>{messages.inboundSecretNotice}</small></label>}
        {canManage && channel && <span className="button-row">
          <button type="button" onClick={() => onRotate?.()}>{channel.secret_set ? messages.rotateInboundSecret : messages.createInboundSecret}</button>
          <button type="button" className="danger-action" onClick={() => onDisconnect?.()}>{messages.disconnectEmail}</button>
        </span>}
        {channel && (channel.domain_verified_at
          ? <p className="muted-copy">{messages.domainVerified(channel.domain, formatDate(channel.domain_verified_at, timezone))}</p>
          : <div className="email-channel-dns"><p className="muted-copy">{messages.domainVerificationHelp}</p>
              <label className="invite-link"><span>{messages.dnsRecordName}</span><input readOnly value={channel.dns_record_name} /></label>
              <label className="invite-link"><span>{messages.dnsRecordValue}</span><input readOnly value={channel.dns_record_value} /></label>
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
