import { useQuery } from '@tanstack/react-query';
import { useEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { api } from '../../api/client';
import type { GmailSettings, MailboxConnection as Connection, MicrosoftSettings } from '../../api/schemas';
import { formatDate } from '../../lib/format-date';
import { messages as shared } from '../../messages';
import { CopyButton } from './copy-button';
import { gmailMessages } from './gmail-messages';
import type { MailboxProvider, MailboxReturn } from './gmail-return';
import { microsoftMessages } from './microsoft-messages';

const messages = { ...shared, ...gmailMessages, ...microsoftMessages };

// What the block says and does differently for each provider; everything else is one flow.
interface ProviderText {
  label: string;
  site: string;
  connect: string;
  connecting: string;
  reconnect: string;
  useAnother: string;
  connectedDescription: string;
  connected: (address: string, when: string) => string;
  failedTitle: string;
  failedBody: (reason: string, when: string) => string;
  failureReasons: Record<string, string>;
  failureUnknown: string;
  addressMismatch: (address: string, support: string) => string;
  notAvailable: (settings: string[]) => string;
  notSetUpSummary: string;
  redirectUri: string;
  disconnect: string;
  disconnectQuestion: string;
  confirmDisconnect: string;
  signInAs: (address: string, button: string) => string;
  justConnected: string;
  connectFailed: string;
  returnErrors: Record<string, string>;
}

const providers: Record<MailboxProvider, ProviderText> = {
  google: {
    label: messages.gmailLabel, site: messages.gmailSite, connect: messages.connectGmail, connecting: messages.connectingGmail, reconnect: messages.reconnectGmail,
    useAnother: messages.useAnotherGmail, connectedDescription: messages.gmailConnectedDescription, connected: messages.gmailConnected,
    failedTitle: messages.gmailFailedTitle, failedBody: messages.gmailFailedBody, failureReasons: messages.gmailFailureReasons, failureUnknown: messages.gmailFailureUnknown,
    addressMismatch: messages.gmailAddressMismatch, notAvailable: messages.gmailNotAvailable, notSetUpSummary: messages.gmailNotSetUpSummary,
    redirectUri: messages.gmailRedirectUri, disconnect: messages.disconnectGmail, disconnectQuestion: messages.disconnectGmailQuestion,
    confirmDisconnect: messages.confirmDisconnectGmail, signInAs: messages.gmailSignInAs, justConnected: messages.gmailJustConnected,
    connectFailed: messages.gmailConnectFailed, returnErrors: messages.gmailReturnErrors,
  },
  microsoft: {
    label: messages.microsoftLabel, site: messages.microsoftSite, connect: messages.connectMicrosoft, connecting: messages.connectingMicrosoft,
    reconnect: messages.reconnectMicrosoft, useAnother: messages.useAnotherMicrosoft, connectedDescription: messages.microsoftConnectedDescription,
    connected: messages.microsoftConnected, failedTitle: messages.microsoftFailedTitle, failedBody: messages.microsoftFailedBody,
    failureReasons: messages.microsoftFailureReasons, failureUnknown: messages.microsoftFailureUnknown, addressMismatch: messages.microsoftAddressMismatch,
    notAvailable: messages.microsoftNotAvailable, notSetUpSummary: messages.microsoftNotSetUpSummary, redirectUri: messages.microsoftRedirectUri,
    disconnect: messages.disconnectMicrosoft, disconnectQuestion: messages.disconnectMicrosoftQuestion, confirmDisconnect: messages.confirmDisconnectMicrosoft,
    signInAs: messages.microsoftSignInAs, justConnected: messages.microsoftJustConnected, connectFailed: messages.microsoftConnectFailed,
    returnErrors: messages.microsoftReturnErrors,
  },
};

// returnText explains the outcome the provider sent back; button is what starts that provider's sign-in on
// screen now. Unknown reasons get the generic text: the address is never shown as written.
function returnText(returned: MailboxReturn, button: string): string {
  const text = providers[returned.provider ?? 'google'];
  if (returned.kind === 'connected') return text.justConnected;
  if (returned.kind === 'admin_consent_done') return messages.microsoftAdminConsentDone(button);
  const reason = returned.reason ?? '';
  return Object.hasOwn(text.returnErrors, reason) ? text.returnErrors[reason] : text.connectFailed;
}

// useCloud tells Colliedesk Cloud the way the server tells it (realm in problem-report-text.go): the realm
// analytics name, else cloud sign-in with billing. Read here, in this chunk, so the main bundle does not grow.
function useCloud() {
  return useRealm() === 'cloud';
}

// useRealm is 'cloud', 'self-hosted', or undefined while the public config is on its way.
function useRealm(): 'cloud' | 'self-hosted' | undefined {
  const config = useQuery({ queryKey: ['public-config'], queryFn: api.config }).data;
  if (!config) return undefined;
  return (config.product_analytics.realm ?? (config.cloud_sign_in.enabled && config.billing.enabled ? 'cloud' : null)) === 'cloud' ? 'cloud' : 'self-hosted';
}

// GmailDataDisclosure says, before an admin goes to Google's consent page, how Colliedesk Cloud handles Google
// data. Only on Cloud: the statement binds HC Labs' Google client, not a self-hosted server's.
export function GmailDataDisclosure() {
  const cloud = useCloud();
  return cloud && <p className="muted-copy">
    {messages.gmailGoogleData} <a href={messages.gmailPrivacyURL} target="_blank" rel="noopener noreferrer">{messages.gmailPrivacyPolicy}</a>
  </p>;
}

// MicrosoftDataDisclosure points to how Colliedesk Cloud handles Microsoft data. Microsoft has no statement
// like Google's Limited Use, so it is a link only, and only on Cloud.
export function MicrosoftDataDisclosure() {
  const cloud = useCloud();
  return cloud && <p className="muted-copy">
    {messages.microsoftData} <a href={messages.microsoftPrivacyURL} target="_blank" rel="noopener noreferrer">{messages.gmailPrivacyPolicy}</a>
  </p>;
}

// MicrosoftRequestNote is what the approver of an agent's request to connect Microsoft 365 reads first: the
// approval opens Microsoft's sign-in, which a work account may only pass with its admin's approval.
export function MicrosoftRequestNote() {
  return <>
    <p className="muted-copy">{messages.microsoftRequestNote}</p>
    <MicrosoftDataDisclosure />
  </>;
}

interface MailboxConnectionProps {
  gmail: GmailSettings;
  // Microsoft 365 on this server; absent, the block offers Gmail only.
  microsoft?: MicrosoftSettings;
  supportAddress: string;
  timezone: string;
  canManage: boolean;
  onConnect?: (provider: MailboxProvider) => void;
  onDisconnect?: (provider: MailboxProvider) => void;
  // The provider whose sign-in is opening (true: Gmail).
  connectPending?: boolean | MailboxProvider;
  // Why the last connect or disconnect failed, in the server's words.
  error?: string;
  returned?: MailboxReturn;
}

// MailboxConnection is the "Send from your mailbox" part of the email channel: connect Gmail or Microsoft 365,
// see which account sends and whether it still works, reconnect, switch, disconnect. A channel has one
// connection. A failed connection is an alert: replies stop until an admin reconnects, and they never leave
// another way.
export function MailboxConnection({ gmail, microsoft: offeredMicrosoft, supportAddress, timezone, canManage, onConnect, onDisconnect, connectPending, error, returned }: MailboxConnectionProps) {
  // Microsoft 365 the server has not set up is only the operator's business: on Colliedesk Cloud (and until
  // the page knows where it runs) it is left out, with nothing to name; a self-hosted admin sees what to set.
  const realm = useRealm();
  const microsoft = offeredMicrosoft && (offeredMicrosoft.available || offeredMicrosoft.connection || realm === 'self-hosted') ? offeredMicrosoft : undefined;
  const connection: Connection | null = gmail.connection ?? microsoft?.connection ?? null;
  const active: MailboxProvider | undefined = connection ? (connection.provider === 'microsoft' ? 'microsoft' : 'google') : undefined;
  const settings = active === 'microsoft' ? microsoft! : gmail;
  const text = providers[active ?? 'google'];
  const available = (provider: MailboxProvider) => (provider === 'google' ? gmail.available : microsoft?.available === true);
  const other: MailboxProvider | undefined = active && available(active === 'google' ? 'microsoft' : 'google') ? (active === 'google' ? 'microsoft' : 'google') : undefined;
  const pending: MailboxProvider | undefined = connectPending === true ? 'google' : connectPending || undefined;
  const [confirming, setConfirming] = useState<'disconnect' | 'switch' | null>(null);
  const disconnectButton = useRef<HTMLButtonElement>(null);
  const switchButton = useRef<HTMLButtonElement>(null);
  const returnFocus = useRef<'disconnect' | 'switch' | null>(null);
  useEffect(() => {
    if (confirming || !returnFocus.current) return;
    (returnFocus.current === 'switch' ? switchButton : disconnectButton).current?.focus();
    returnFocus.current = null;
  }, [confirming]);
  // Back from the provider, the page opens at the top; the outcome is down here, so it is brought into view.
  const block = useRef<HTMLElement>(null);
  useEffect(() => {
    if (returned) block.current?.scrollIntoView?.({ block: 'center' });
  }, [returned]);
  const cancel = () => { returnFocus.current = confirming; setConfirming(null); };
  // The server's reason ends with "Reconnect … in Settings", meant for the timeline; here it is said by the
  // code instead, and the button is right under it.
  const failureReason = connection?.error_code ? text.failureReasons[connection.error_code] ?? text.failureUnknown : text.failureUnknown;
  const connectButton = (provider: MailboxProvider, label: string, primary = false) => (
    <button type="button" className={primary ? 'primary-action' : undefined} disabled={Boolean(pending)} onClick={() => onConnect?.(provider)}>
      {pending === provider ? providers[provider].connecting : label}
    </button>
  );
  const unconnected = (['google', 'microsoft'] as const).filter((provider) => provider === 'google' || microsoft);
  const offered = unconnected.filter(available);
  // Microsoft said the person needs an admin's approval, or the approval failed: the link opens at once.
  const approvalOpen = returned?.provider === 'microsoft' && returned.kind === 'error' && (returned.reason === 'denied' || returned.reason === 'admin_consent_failed');
  const approval = microsoft?.admin_consent_url && <details className="email-channel-dns-folded mailbox-approval" open={approvalOpen || undefined}>
    <summary>{messages.microsoftAdminApprovalSummary}</summary>
    <div className="mailbox-approval-body">
      <p className="muted-copy">{messages.microsoftAdminApprovalBody}</p>
      <div className="copy-field">
        <label className="invite-link"><span className="field-label">{messages.microsoftAdminApprovalLink}</span><input readOnly value={microsoft.admin_consent_url} /></label>
        <span className="button-row"><CopyButton value={microsoft.admin_consent_url} copiedText={messages.approvalLinkCopied}>{messages.copyApprovalLink}</CopyButton></span>
      </div>
    </div>
  </details>;
  // The button that starts a provider's sign-in as the block shows it now, which the outcome names.
  const buttonFor = (provider: MailboxProvider) => !connection ? providers[provider].connect
    : provider !== active ? messages.useInstead(providers[provider].label)
      : connection.status === 'failed' ? providers[provider].reconnect : providers[provider].useAnother;
  // What the operator must set for a provider; with no provider at all it stays open, as it always was.
  const setup = (provider: MailboxProvider, folded: boolean): ReactNode => {
    const missing = provider === 'google' ? gmail.missing_settings : microsoft?.missing_settings ?? [];
    const body = <>
      <p role="note" className="warning-note">{providers[provider].notAvailable(missing)}</p>
      {canManage && !connection && <label className="invite-link"><span className="field-label">{providers[provider].redirectUri}</span>
        <input readOnly value={provider === 'google' ? gmail.redirect_uri : microsoft!.redirect_uri} /></label>}
      {canManage && !connection && provider === 'microsoft' && <label className="invite-link"><span className="field-label">{messages.microsoftAdminConsentRedirectUri}</span>
        <input readOnly value={microsoft!.admin_consent_redirect_uri} /></label>}
    </>;
    return folded ? <details key={provider} className="email-channel-dns-folded"><summary>{providers[provider].notSetUpSummary}</summary>{body}</details> : <div key={provider}>{body}</div>;
  };
  return (
    <section ref={block} className="gmail-connection" aria-labelledby="gmail-connection-heading">
      <h3 id="gmail-connection-heading">{messages.mailboxHeading}</h3>
      <p className="muted-copy">{connection ? text.connectedDescription : messages.mailboxDescription}</p>
      {returned && <p role={returned.kind === 'error' ? 'alert' : 'status'} className={returned.kind === 'error' ? 'error-copy' : 'muted-copy'}>{returnText(returned, buttonFor(returned.provider ?? 'google'))}
        {returned.reason === 'address_mismatch' && returned.provider === 'microsoft' && <> {messages.microsoftAliasNotSupported(supportAddress)}</>}
        {returned.reason === 'address_mismatch' && <> {providers[returned.provider ?? 'google'].signInAs(supportAddress, buttonFor(returned.provider ?? 'google'))}</>}
        {returned.channel && <> <a href={`/settings/channels/${encodeURIComponent(returned.channel)}?tab=replies`}>{messages.gmailOpenHolder}</a></>}</p>}
      {error && <p role="alert" className="error-copy">{error}</p>}
      {/* The connected provider lost its settings: said plainly. Without a connection, the providers this
          server lacks are folded for the admin, or shown as before when there is none at all. */}
      {active && !available(active) && setup(active, false)}
      {!connection && offered.length === 0 && unconnected.map((provider) => setup(provider, provider !== 'google'))}
      {!connection && offered.length > 0 && canManage && unconnected.filter((provider) => !available(provider)).map((provider) => setup(provider, true))}
      {/* Before an admin leaves for a provider's sign-in, how Cloud handles that provider's data. */}
      {canManage && gmail.available && (active !== 'microsoft' || other === 'google') && <GmailDataDisclosure />}
      {canManage && microsoft?.available && (active !== 'google' || other === 'microsoft') && <MicrosoftDataDisclosure />}
      {!connection && offered.length > 0 && (canManage
        ? <>
            <span className="button-row">{offered.map((provider) => connectButton(provider, providers[provider].connect, offered.length === 1))}</span>
            {offered.includes('microsoft') && approval}
          </>
        : <p className="muted-copy">{microsoft ? messages.mailboxAdminOnly : messages.gmailAdminOnly}</p>)}
      {connection?.status === 'failed' && <div role="alert" className="warning-note" aria-labelledby="gmail-failed-heading">
        <strong id="gmail-failed-heading">{text.failedTitle}</strong>
        <p>{text.failedBody(failureReason, formatDate(connection.failed_at ?? connection.connected_at, timezone))}</p>
        {canManage && available(active!) && <span className="button-row">{connectButton(active!, text.reconnect, true)}</span>}
      </div>}
      {connection?.status === 'connected' && available(active!) && <p className="muted-copy">{text.connected(connection.address, formatDate(connection.connected_at, timezone))}</p>}
      {connection && active === 'microsoft' && microsoft?.server_credentials_refused && canManage &&
        <p role="note" className="warning-note">{messages.microsoftServerCredentials}</p>}
      {connection && settings.address_matches === false
        ? <p role="note" className="warning-note">{text.addressMismatch(connection.address, supportAddress)}</p>
        : connection && connection.address !== supportAddress.toLowerCase() && <p className="muted-copy">{messages.gmailOtherAddress(connection.address, supportAddress)}</p>}
      {/* Microsoft asked for an admin's approval while switching or using another account, or a reconnect after
          a sign-in policy blocked the account may need it again. */}
      {connection && canManage && microsoft?.available && (approvalOpen || (connection.status === 'failed' && active === 'microsoft')) && approval}
      {connection && canManage && !confirming && <span className="button-row">
        {connection.status === 'connected' && available(active!) && connectButton(active!, text.useAnother)}
        {other && <button type="button" ref={switchButton} disabled={Boolean(pending)} onClick={() => setConfirming('switch')}>
          {pending === other ? providers[other].connecting : messages.useInstead(providers[other].label)}</button>}
        <button type="button" className="danger-action" ref={disconnectButton} onClick={() => setConfirming('disconnect')}>{text.disconnect}</button>
      </span>}
      {connection && canManage && confirming === 'switch' && other && <div className="confirm-panel" role="group" aria-labelledby="switch-mailbox-question"
        onKeyDown={(event) => { if (event.key === 'Escape') cancel(); }}>
        <p><strong id="switch-mailbox-question">{messages.switchQuestion(providers[other].label)}</strong> {messages.switchConsequence(providers[other].label, text.label)}</p>
        <span className="button-row">
          <button type="button" className="primary-action" onClick={() => { setConfirming(null); onConnect?.(other); }}>{messages.continueTo(providers[other].site)}</button>
          <button type="button" autoFocus onClick={cancel}>{messages.cancel}</button>
        </span>
      </div>}
      {connection && canManage && confirming === 'disconnect' && <div className="confirm-panel" role="group" aria-labelledby="disconnect-gmail-question"
        onKeyDown={(event) => { if (event.key === 'Escape') cancel(); }}>
        <p><strong id="disconnect-gmail-question">{text.disconnectQuestion}</strong> {active === 'microsoft'
          ? <>{messages.disconnectMicrosoftConsequence(connection.address)}{' '}
              <a href={messages.myAppsURL} target="_blank" rel="noopener noreferrer">{messages.disconnectMicrosoftWork}</a> {messages.disconnectMicrosoftWorkHint},{' '}
              <a href={messages.personalConsentURL} target="_blank" rel="noopener noreferrer">{messages.disconnectMicrosoftPersonal}</a> {messages.disconnectMicrosoftPersonalHint}.{' '}
              {messages.disconnectMicrosoftAfter}</>
          : messages.disconnectGmailConsequence(connection.address)}</p>
        <span className="button-row">
          <button type="button" className="danger-action" onClick={() => { setConfirming(null); onDisconnect?.(active!); }}>{text.confirmDisconnect}</button>
          <button type="button" autoFocus onClick={cancel}>{messages.cancel}</button>
        </span>
      </div>}
    </section>
  );
}

// The block was "Send from Gmail"; its callers and tests keep this name.
export const GmailConnection = MailboxConnection;
