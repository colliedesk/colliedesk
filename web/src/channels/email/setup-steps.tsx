import { leaveTo } from '../../lib/leave-to';
import { useEffect, useId, useState } from 'react';
import type { ReactNode } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, InvalidRequestError } from '../../api/client';
import { channelsApi } from '../../api/channels';
import type { Channel } from '../../api/channels';
import type { EmailChannelSettings } from '../../api/schemas';
import { messages } from '../../messages';
import { CopyButton } from '../../features/settings/copy-button';
import { EmailChannelSection, gmailPart } from '../../features/settings/email-channel-section';
import { GmailDataDisclosure, MicrosoftDataDisclosure } from '../../features/settings/gmail-connection';
import type { MailboxReturn } from '../../features/settings/gmail-return';
import { NameForm } from '../../features/settings/name-form';
import { saveState } from '../../features/settings/save-feedback';
import { settingsMessages } from '../../features/settings/settings-messages';
import { panelID, tabID, Tabs } from '../../ui/tabs';
import { providerSteps } from './provider-instructions';
import { clockTime, recommended } from './setup-state';
import type { Provider, RepliesChoice } from './setup-state';
import { setupMessages as m } from './setup-messages';
import { useInboundSecret, useMailbox } from './email-shared';
import { GmailConfirmation } from './forwarding-confirmation';

// useRefetchOnActivity reloads the channel whenever the app's inbox poll (useInboxActivity) brings a new or
// changed conversation: the first forwarded email shows here without a second poll. Only while active.
export function useRefetchOnActivity(channelID: string, active: boolean) {
  const client = useQueryClient();
  useEffect(() => {
    if (!active) return undefined;
    return client.getQueryCache().subscribe((event) => {
      if (event.type !== 'updated' || event.action.type !== 'success' || event.query.queryKey[0] !== 'inbox-activity') return;
      const data = event.query.state.data as { new_conversations?: unknown[]; updated?: unknown[] } | undefined;
      if (!data?.new_conversations?.length && !data?.updated?.length) return;
      void client.invalidateQueries({ queryKey: ['channel', channelID] });
      void client.invalidateQueries({ queryKey: ['channels'] });
      void client.invalidateQueries({ queryKey: ['forwarding-confirmation', channelID] });
    });
  }, [client, channelID, active]);
}

// ForwardingName is the workspace address name every hosted forwarding address carries: changing it
// changes them all, so it sits folded where the forwarding address is changed.
export function ForwardingName() {
  const client = useQueryClient();
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const save = useMutation({ mutationFn: api.updateWorkspaceSlug,
    // On the workspace's own host the desk moves with the name: continue on the new host.
    onSuccess: (renamed) => renamed.continue_url ? leaveTo(renamed.continue_url)
      : Promise.all(['me', 'email-channel'].map((key) => client.invalidateQueries({ queryKey: [key] }))) });
  if (!me.data) return null;
  return <NameForm label={settingsMessages.workspaceAddressName} saveLabel={settingsMessages.saveAddressName} hint={settingsMessages.addressNameWarning} value={me.data.workspace.slug} canManage
    maxLength={63} save={saveState(save)} onSave={(slug) => save.mutate(slug)} />;
}

interface ForwardProps {
  channel: Channel;
  settings: EmailChannelSettings;
  canManage: boolean;
  timezone: string;
  provider: Provider | undefined;
  onProvider: (provider: Provider) => void;
  onContinue: () => void;
  onSkip: () => void;
}

export function ForwardStep({ channel, settings, canManage, timezone, provider, onProvider, onContinue, onSkip }: ForwardProps) {
  const client = useQueryClient();
  const refresh = () => Promise.all(['email-channel', 'channel', 'channels'].map((key) => client.invalidateQueries({ queryKey: key === 'channel' ? [key, channel.id] : [key] })));
  const hosted = settings.hosted_address;
  const tab: Provider = provider ?? (hosted ? 'gmail' : 'other');
  const shown = providerSteps.find((item) => item.id === tab) ?? providerSteps[0];
  const prefix = useId();
  const secret = useInboundSecret(channel.id, settings.channel?.secret_set === true, refresh);
  const received = channel.status.state === 'working' || Boolean(channel.last_inbound_at);
  return <div className="setup-body">
    {hosted && <div className="copy-field setup-copy">
      <label className="invite-link"><span className="field-label">{m.forwardTo}</span><input readOnly value={hosted} /></label>
      <span className="button-row"><CopyButton className="primary-action" value={hosted} copiedText={settingsMessages.addressCopied}>{m.copy}</CopyButton></span>
    </div>}
    <Tabs label={m.providerTabs} prefix={prefix} value={tab} onChange={onProvider} tabs={providerSteps.map(({ id, label }) => ({ id, label }))} />
    <div role="tabpanel" id={panelID(prefix, tab)} aria-labelledby={tabID(prefix, tab)} className="setup-provider">
      {!hosted && tab !== 'other'
        ? <p className="muted-copy">{m.needsForwarder(shown.label)} <button type="button" className="link-action" onClick={() => onProvider('other')}>{m.openOther}</button></p>
        : <ol className="setup-instructions">{shown.steps.map((text, index) => <li key={text}>
            {shown.warning?.before === index && <p role="note" className="warning-note">{shown.warning.text}</p>}
            <span>{text}</span>
            {index === 0 && shown.open && <> <a href={shown.open.href} target="_blank" rel="noreferrer">{shown.open.label} ↗</a></>}
          </li>)}</ol>}
      {tab === 'other' && !hosted && <>
        {secret.feedback}
        <EmailChannelSection part="arrival" settings={settings} timezone={timezone} canManage={canManage} createdSecret={secret.created} onRotate={secret.onRotate} />
      </>}
    </div>
    {(tab === 'gmail' || tab === 'workspace') && <GmailConfirmation channelID={channel.id} timezone={timezone} waiting={!received} />}
    <p role="status" className={`setup-live ${received ? 'setup-live-ok' : ''}`}>
      {received ? m.firstEmail(channel.last_inbound_at ? clockTime(channel.last_inbound_at, timezone) : '') : m.waitingFirstEmail}</p>
    <div className="setup-actions">
      <button type="button" className="primary-action" disabled={!received} onClick={onContinue}>{m.continue}</button>
      {!received && <><small className="muted-copy">{m.continueLocked}</small>
        <button type="button" className="link-action" onClick={onSkip}>{m.skip}</button></>}
    </div>
    {hosted && canManage && <details className="email-home-fold setup-fold"><summary>{m.changeForwarding}</summary><ForwardingName /></details>}
  </div>;
}

interface RepliesProps {
  channelID: string;
  settings: EmailChannelSettings;
  canManage: boolean;
  timezone: string;
  provider: Provider | undefined;
  done: boolean;
  onContinue: () => void;
  onSkip: () => void;
  // beforeLeave runs just before the browser leaves for a mailbox provider's sign-in.
  beforeLeave: () => void;
  // returned: the outcome that sign-in came back with, shown in the mailbox block (why it failed, the approval
  // link a Microsoft 365 admin needs).
  returned?: MailboxReturn;
}

const choices: RepliesChoice[] = ['gmail', 'microsoft', 'colliedesk'];

export function RepliesStep({ channelID, settings, canManage, timezone, provider, done, onContinue, onSkip, beforeLeave, returned }: RepliesProps) {
  const client = useQueryClient();
  const refresh = () => Promise.all(['email-channel', 'channel', 'channels'].map((key) => client.invalidateQueries({ queryKey: key === 'channel' ? [key, channelID] : [key] })));
  const best = recommended(provider);
  // The choice open first: the provider a sign-in just came back from, else the mailbox already connected,
  // else the recommended one. Its block says what happened.
  const connected: RepliesChoice | undefined = settings.microsoft.connection ? 'microsoft' : settings.gmail.connection ? 'gmail' : undefined;
  const initial: RepliesChoice = returned ? (returned.provider === 'microsoft' ? 'microsoft' : 'gmail') : connected ?? best;
  const [choice, setChoice] = useState<RepliesChoice>(initial);
  useEffect(() => setChoice(initial), [initial]);
  useEffect(gmailPart.preload, []);
  const mailbox = useMailbox(channelID, refresh, beforeLeave);
  const verifyDomain = useMutation({ mutationFn: () => channelsApi.emailVerifyDomain(channelID), onSettled: refresh });
  const connection = settings.gmail.connection ?? settings.microsoft.connection;
  const domain = settings.channel?.domain ?? '';
  const name = useId();
  const describe: Record<RepliesChoice, { title: string; text: string }> = {
    gmail: { title: m.gmail, text: m.gmailText },
    microsoft: { title: m.microsoft, text: m.microsoftText },
    colliedesk: { title: m.colliedesk, text: m.colliedeskText(domain) },
  };
  const mailboxFor = choice === 'microsoft' ? 'microsoft' : 'google';
  const available = mailboxFor === 'microsoft' ? settings.microsoft.available : settings.gmail.available;
  return <div className="setup-body">
    <p className="muted-copy">{m.repliesIntro}</p>
    <EmailChannelSection part="sending" settings={settings} timezone={timezone} canManage={canManage} />
    <fieldset className="setup-choices">
      <legend className="sr-only">{m.repliesLegend}</legend>
      {choices.map((id) => <label key={id} className="setup-choice">
        <input type="radio" name={name} value={id} checked={choice === id} onChange={() => setChoice(id)} />
        <span><strong>{describe[id].title}</strong>{id === best && <span className="setup-badge">{m.recommended}</span>}<small>{describe[id].text}</small></span>
      </label>)}
    </fieldset>
    <div className="setup-choice-body">
      {choice === 'colliedesk'
        ? <>{verifyDomain.isError && !(verifyDomain.error instanceof InvalidRequestError) && <p role="alert" className="error-copy">{messages.settingsActionFailed}</p>}
            <EmailChannelSection part="domain" settings={settings} timezone={timezone} canManage={canManage} onVerifyDomain={() => verifyDomain.mutate()} verifyPending={verifyDomain.isPending}
              verifyError={verifyDomain.error instanceof InvalidRequestError ? verifyDomain.error.message : undefined} /></>
        : !connection && available && canManage && !returned
          ? <>{mailboxFor === 'google' ? <GmailDataDisclosure /> : <MicrosoftDataDisclosure />}
              {mailbox.error && <p role="alert" className="error-copy">{mailbox.error}</p>}
              <span className="button-row"><button type="button" className="primary-action" disabled={Boolean(mailbox.pending)} onClick={() => mailbox.connect(mailboxFor)}>
                {mailbox.pending ? m.opening : mailboxFor === 'google' ? m.connectGmail : m.connectMicrosoft}</button></span></>
          : <EmailChannelSection part="mailbox" settings={settings} timezone={timezone} canManage={canManage} onConnectMailbox={mailbox.connect} onDisconnectMailbox={mailbox.disconnect}
              mailboxPending={mailbox.pending} mailboxError={mailbox.error} mailboxReturned={returned} />}
    </div>
    <div className="setup-actions">
      {done && <button type="button" className="primary-action" onClick={onContinue}>{m.continue}</button>}
      {!done && <button type="button" className="link-action" onClick={onSkip}>{settings.sending_from ? m.skipReplies(settings.sending_from) : m.skipNoReplies}</button>}
    </div>
  </div>;
}
