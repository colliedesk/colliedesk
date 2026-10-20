import { useQuery } from '@tanstack/react-query';
import { useEffect, useRef, useState } from 'react';
import { api } from '../../api/client';
import type { GmailSettings } from '../../api/schemas';
import { formatDate } from '../../lib/format-date';
import { messages as shared } from '../../messages';
import { gmailMessages } from './gmail-messages';
import type { GmailReturn } from './gmail-return';

const messages = { ...shared, ...gmailMessages };

// returnText explains the outcome Google sent back. Unknown reasons get the generic text: the address is never
// shown as written.
function returnText(returned: GmailReturn): string {
  if (returned.kind === 'connected') return messages.gmailJustConnected;
  const reason = returned.reason ?? '';
  return Object.hasOwn(messages.gmailReturnErrors, reason) ? messages.gmailReturnErrors[reason] : messages.gmailConnectFailed;
}

interface GmailConnectionProps {
  gmail: GmailSettings;
  supportAddress: string;
  timezone: string;
  canManage: boolean;
  onConnect?: () => void;
  onDisconnect?: () => void;
  connectPending?: boolean;
  // Why the last connect or disconnect failed, in the server's words.
  error?: string;
  returned?: GmailReturn;
}

// GmailConnection is the "Send from Gmail" part of the email channel: connect, see which account sends and
// whether it still works, reconnect, disconnect. A failed connection is an alert: replies stop until an
// admin reconnects, and they never leave another way.
export function GmailConnection({ gmail, supportAddress, timezone, canManage, onConnect, onDisconnect, connectPending, error, returned }: GmailConnectionProps) {
  const connection = gmail.connection;
  // Cloud the way the server tells it (realm in problem-report-text.go): the realm analytics name, else cloud
  // sign-in with billing. Read here, in this block's chunk, so the main bundle does not grow.
  const config = useQuery({ queryKey: ['public-config'], queryFn: api.config }).data;
  const cloud = (config?.product_analytics.realm ?? (config?.cloud_sign_in.enabled && config.billing.enabled ? 'cloud' : null)) === 'cloud';
  const [confirming, setConfirming] = useState(false);
  const disconnectButton = useRef<HTMLButtonElement>(null);
  const returnFocus = useRef(false);
  useEffect(() => {
    if (confirming || !returnFocus.current) return;
    returnFocus.current = false;
    disconnectButton.current?.focus();
  }, [confirming]);
  // Back from Google, the page opens at the top; the outcome is down here, so it is brought into view.
  const block = useRef<HTMLElement>(null);
  useEffect(() => {
    if (returned) block.current?.scrollIntoView?.({ block: 'center' });
  }, [returned]);
  const cancelDisconnect = () => { returnFocus.current = true; setConfirming(false); };
  // The server's reason ends with "Reconnect Gmail in Settings", meant for the timeline; here it is said by
  // the code instead, and the button is right under it.
  const failureReason = connection?.error_code ? messages.gmailFailureReasons[connection.error_code] ?? messages.gmailFailureUnknown : messages.gmailFailureUnknown;
  const connectButton = (label: string, primary = false) => (
    <button type="button" className={primary ? 'primary-action' : undefined} disabled={connectPending} onClick={() => onConnect?.()}>
      {connectPending ? messages.connectingGmail : label}
    </button>
  );
  return (
    <section ref={block} className="gmail-connection" aria-labelledby="gmail-connection-heading">
      <h3 id="gmail-connection-heading">{messages.gmailHeading}</h3>
      <p className="muted-copy">{connection ? messages.gmailConnectedDescription : messages.gmailDescription}</p>
      {returned && <p role={returned.kind === 'error' ? 'alert' : 'status'} className={returned.kind === 'error' ? 'error-copy' : 'muted-copy'}>{returnText(returned)}</p>}
      {error && <p role="alert" className="error-copy">{error}</p>}
      {!gmail.available && <>
        <p role="note" className="warning-note">{messages.gmailNotAvailable(gmail.missing_settings)}</p>
        {canManage && !connection && <label className="invite-link"><span className="field-label">{messages.gmailRedirectUri}</span><input readOnly value={gmail.redirect_uri} /></label>}
      </>}
      {cloud && gmail.available && canManage && <p className="muted-copy">
        {messages.gmailGoogleData} <a href={messages.gmailPrivacyURL} target="_blank" rel="noopener noreferrer">{messages.gmailPrivacyPolicy}</a>
      </p>}
      {gmail.available && !connection && (canManage
        ? <span className="button-row">{connectButton(messages.connectGmail, true)}</span>
        : <p className="muted-copy">{messages.gmailAdminOnly}</p>)}
      {connection?.status === 'failed' && <div role="alert" className="warning-note" aria-labelledby="gmail-failed-heading">
        <strong id="gmail-failed-heading">{messages.gmailFailedTitle}</strong>
        <p>{messages.gmailFailedBody(failureReason, formatDate(connection.failed_at ?? connection.connected_at, timezone))}</p>
        {canManage && gmail.available && <span className="button-row">{connectButton(messages.reconnectGmail, true)}</span>}
      </div>}
      {connection?.status === 'connected' && gmail.available && <p className="muted-copy">{messages.gmailConnected(connection.address, formatDate(connection.connected_at, timezone))}</p>}
      {connection && connection.address !== supportAddress.toLowerCase() && <p className="muted-copy">{messages.gmailOtherAddress(connection.address, supportAddress)}</p>}
      {connection && canManage && !confirming && <span className="button-row">
        {connection.status === 'connected' && gmail.available && connectButton(messages.useAnotherGmail)}
        <button type="button" className="danger-action" ref={disconnectButton} onClick={() => setConfirming(true)}>{messages.disconnectGmail}</button>
      </span>}
      {connection && canManage && confirming && <div className="confirm-panel" role="group" aria-labelledby="disconnect-gmail-question"
        onKeyDown={(event) => { if (event.key === 'Escape') cancelDisconnect(); }}>
        <p><strong id="disconnect-gmail-question">{messages.disconnectGmailQuestion}</strong> {messages.disconnectGmailConsequence(connection.address)}</p>
        <span className="button-row">
          <button type="button" className="danger-action" onClick={() => { setConfirming(false); onDisconnect?.(); }}>{messages.confirmDisconnectGmail}</button>
          <button type="button" autoFocus onClick={cancelDisconnect}>{messages.cancel}</button>
        </span>
      </div>}
    </section>
  );
}
