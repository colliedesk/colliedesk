import { useEffect, useId, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ConflictError, InvalidRequestError } from '../../api/client';
import { channelsApi } from '../../api/channels';
import { useCreateMutation } from '../../api/use-create';
import { Dialog } from '../../ui/dialog';
import { messages } from '../../messages';
import { settingsMessages } from '../../features/settings/settings-messages';
import { EmailChannelSection, gmailPart } from '../../features/settings/email-channel-section';
import { mailboxReturn } from '../../features/settings/gmail-return';
import type { MailboxProvider } from '../../features/settings/gmail-return';
import { NameForm } from '../../features/settings/name-form';
import { saveState, useSaveFeedback } from '../../features/settings/save-feedback';
import type { SaveState } from '../../features/settings/save-feedback';
import type { ChannelPanelProps, ConnectProps } from '../registry';

// One email channel of several: its settings and actions go through /api/v1/channels/{id}/email… and
// PATCH /api/v1/channels/{id}; the list, the channel page (its state and notice) and its history show
// what changes, so every action reloads them.
function useEmailSettings(channelID: string) {
  const client = useQueryClient();
  const settings = useQuery({ queryKey: ['email-channel', channelID], queryFn: () => channelsApi.email(channelID) });
  const refresh = () => Promise.all(['email-channel', 'channels', 'channel', 'channel-events']
    .map((key) => client.invalidateQueries({ queryKey: key === 'channel' ? [key, channelID] : [key] })));
  return { settings, refresh };
}

// A link to the channel that has a value refused here.
const conflictLink = (conflict: NonNullable<SaveState['conflict']>) =>
  <a href={`/settings/channels/${encodeURIComponent(conflict.channel_id)}`}>{emailMessages.openChannel(conflict.channel_name)}</a>;

function Connection({ channel, canManage, timezone, navigate }: ChannelPanelProps) {
  const { settings, refresh } = useEmailSettings(channel.id);
  const saveEmail = useMutation({ mutationFn: (address: string) => channelsApi.update(channel.id, { config: { support_address: address } }), onSuccess: refresh });
  const rotateSecret = useMutation({ mutationFn: () => channelsApi.emailSecret(channel.id), onSuccess: refresh });
  // A new secret stops the old one at once (a forwarder still using it fails), so replacing one asks first.
  const [askingRotate, setAskingRotate] = useState(false);
  const secretSet = settings.data?.channel?.secret_set === true;
  return <>
    {settings.data && <TestMessage channelID={channel.id} ready={Boolean(settings.data.channel) || Boolean(channel.config && 'support_address' in channel.config && channel.config.support_address)} navigate={navigate}
      forwarding={secretSet || channel.status.reason_code !== 'no_inbound_secret'} />}
    {rotateSecret.isError && <p role="alert" className="error-copy">{rotateSecret.error instanceof InvalidRequestError ? rotateSecret.error.message : messages.settingsActionFailed}</p>}
    <EmailChannelSection part="connection" settings={settings.data} timezone={timezone} canManage={canManage}
      addressSave={saveState(saveEmail)} onSave={(address) => saveEmail.mutate(address)} addressConflictLink={conflictLink}
      createdSecret={rotateSecret.data?.inbound_secret} onRotate={() => (secretSet ? setAskingRotate(true) : rotateSecret.mutate())} />
    {askingRotate && <Dialog alert title={emailMessages.rotateTitle} closeLabel={messages.cancel} onClose={() => setAskingRotate(false)}
      onSubmit={() => { setAskingRotate(false); rotateSecret.mutate(); }}
      footer={<><button type="button" autoFocus onClick={() => setAskingRotate(false)}>{messages.cancel}</button>
        <button type="submit" className="danger-action">{emailMessages.rotateConfirm}</button></>}>
      <p>{emailMessages.rotateBody}</p>
    </Dialog>}
  </>;
}

// TestMessage sends the signed-in person a test email on this channel, the way a customer's arrives, and
// opens the conversation it started, as the inbox's action does: the whole loop without a forwarder or a
// terminal. Every press ends in something on screen: that conversation, why it could not, or what to do first.
function TestMessage({ channelID, ready, forwarding, navigate }: { channelID: string; ready: boolean; forwarding: boolean; navigate?: (href: string) => void }) {
  const client = useQueryClient();
  const [notReady, setNotReady] = useState(false);
  // A press while one is in flight sends nothing; one sent again after a failure goes under the same key.
  const send = useCreateMutation({ create: (_nothing: void, key: string) => channelsApi.emailTestMessage(channelID, key),
    onSuccess: (sent) => {
      navigate?.(`/conversations/${encodeURIComponent(sent.conversation_id)}`);
      return Promise.all(['conversations', 'channel', 'channels'].map((key) => client.invalidateQueries({ queryKey: [key] })));
    } });
  const result = useRef<HTMLParagraphElement>(null);
  const shown = send.isSuccess || send.isError || notReady;
  useEffect(() => { if (shown) result.current?.focus(); }, [shown, send.data, send.error]);
  const press = () => {
    if (!ready) { setNotReady(true); return; }
    setNotReady(false);
    send.mutate();
  };
  return <div className="email-channel-test">
    <p><button type="button" className="primary-action" disabled={send.isPending} onClick={press}>{send.isPending ? emailMessages.testSending : emailMessages.testSend}</button></p>
    <p className="muted-copy">{emailMessages.testHelp}</p>
    {notReady && <p role="status" tabIndex={-1} ref={result}>{emailMessages.testNeedsAddress}</p>}
    {send.data && !notReady && <p role="status" tabIndex={-1} ref={result}>{emailMessages.testSent(send.data.sent_to)}{' '}
      {forwarding ? emailMessages.testWorks : emailMessages.testWorksForwardNext}{' '}
      <a href={`/conversations/${encodeURIComponent(send.data.conversation_id)}`}>{emailMessages.testOpen}</a></p>}
    {send.isError && !notReady && <p role="alert" className="error-copy" tabIndex={-1} ref={result}>{send.error instanceof InvalidRequestError ? send.error.message : emailMessages.testFailed}</p>}
  </div>;
}

// Words of the email tabs that the Settings page never had.
const emailMessages = {
  testSend: 'Send yourself a test message',
  testHelp: 'It arrives from your own address like a customer\'s email and opens a conversation. Answer it: the reply is emailed to you, so you see what customers receive.',
  testSending: 'Sending the test message…',
  testSent: (address: string) => `Test message sent from ${address}.`,
  testWorks: 'Your inbox and replies work.',
  testWorksForwardNext: 'Your inbox and replies work. Customer email arrives once your support mailbox forwards here with an inbound secret (below).',
  testNeedsAddress: 'Save a support address first, then send the test message.',
  testOpen: 'Open the test conversation',
  testFailed: 'The test message could not be sent. Check your connection and select Send yourself a test message again.',
  rotateTitle: 'Rotate the inbound secret?',
  rotateBody: 'The current secret stops working at once: mail your forwarder sends with it is refused until you give it the new one, shown once after this.',
  rotateConfirm: 'Rotate secret',
  openChannel: (name: string) => `Open ${name}`,
  name: 'Name',
  nameHint: 'Tells this mailbox apart in the inbox and in Settings. Leave it empty to use the support address.',
};

function Replies({ channel, canManage, timezone }: ChannelPanelProps) {
  const { settings, refresh } = useEmailSettings(channel.id);
  const saveSenderName = useMutation({ mutationFn: (name: string) => channelsApi.update(channel.id, { config: { sender_name: name } }), onSuccess: refresh });
  // A refused check still reloads: the provider's per-record status changed.
  const verifyDomain = useMutation({ mutationFn: () => channelsApi.emailVerifyDomain(channel.id), onSettled: refresh });
  // Connecting a mailbox leaves for the provider's sign-in page (Google, Microsoft); the provider sends the
  // browser back here with the outcome.
  const [mailboxReturned] = useState(mailboxReturn);
  useEffect(gmailPart.preload, []);
  const connectMailbox = useMutation({
    mutationFn: (provider: MailboxProvider) => (provider === 'microsoft' ? channelsApi.emailConnectMicrosoft : channelsApi.emailConnectGmail)(channel.id),
    onSuccess: ({ url }) => window.location.assign(url),
  });
  const disconnectMailbox = useMutation({
    mutationFn: (provider: MailboxProvider) => (provider === 'microsoft' ? channelsApi.emailDisconnectMicrosoft : channelsApi.emailDisconnectGmail)(channel.id),
    onSettled: refresh,
  });
  const mailboxFailure = connectMailbox.error ?? disconnectMailbox.error;
  const mailboxError = mailboxFailure ? (mailboxFailure instanceof InvalidRequestError ? mailboxFailure.message : messages.settingsActionFailed) : undefined;
  return <>
    {verifyDomain.isError && !(verifyDomain.error instanceof InvalidRequestError) && <p role="alert" className="error-copy">{messages.settingsActionFailed}</p>}
    <EmailChannelSection part="replies" settings={settings.data} timezone={timezone} canManage={canManage}
      senderNameSave={saveState(saveSenderName)} onSaveSenderName={(name) => saveSenderName.mutate(name)}
      onVerifyDomain={() => verifyDomain.mutate()} verifyPending={verifyDomain.isPending}
      verifyError={verifyDomain.error instanceof InvalidRequestError ? verifyDomain.error.message : undefined}
      onConnectMailbox={(provider) => connectMailbox.mutate(provider)} onDisconnectMailbox={(provider) => disconnectMailbox.mutate(provider)}
      mailboxPending={connectMailbox.isPending || connectMailbox.isSuccess ? connectMailbox.variables : undefined} mailboxError={mailboxError}
      mailboxReturned={mailboxReturned} />
  </>;
}

// Connect asks a name and the support address; the channel exists once they are saved. A name left empty
// is the address (the first mailbox is called Email).
function Connect({ canManage, create }: ConnectProps) {
  const client = useQueryClient();
  const [name, setName] = useState('');
  const [address, setAddress] = useState('');
  const save = useMutation({
    mutationFn: () => create({ ...(name.trim() ? { name: name.trim() } : {}), config: { support_address: address.trim() } }),
    onSuccess: () => client.invalidateQueries({ queryKey: ['email-channel'] }),
  });
  const state = saveState(save);
  const errorID = useId();
  const nameHint = useId();
  // The refusal is about the field it names: the name or the support address. That field takes the
  // focus, and the words stand right after it.
  const about = save.error instanceof ConflictError && save.error.detail?.code === 'channel_name_taken' ? 'name' : 'address';
  const nameField = useRef<HTMLInputElement>(null);
  const addressField = useRef<HTMLInputElement>(null);
  const feedback = useSaveFeedback(state, errorID, about === 'name' ? nameField : addressField, about === 'name' ? name : address);
  // A teammate reads what is set, as on a connected channel.
  if (!canManage) return <p><strong>{settingsMessages.supportAddress}: </strong><span>{settingsMessages.emailNotConnected}</span></p>;
  const invalid = (field: 'name' | 'address') => (feedback.invalid && about === field) || undefined;
  const refusal = (field: 'name' | 'address') => about === field && <>
    {feedback.status}
    {feedback.invalid && state.conflict && <p>{conflictLink(state.conflict)}</p>}
  </>;
  return <form className="settings-body" onSubmit={(event) => { event.preventDefault(); if (!feedback.pending) save.mutate(); }}>
    <label className="field"><span>{emailMessages.name}</span>
      <input ref={nameField} value={name} maxLength={80} placeholder={address} aria-invalid={invalid('name')} aria-describedby={invalid('name') ? `${errorID} ${nameHint}` : nameHint}
        onChange={(event) => { feedback.touch(); setName(event.target.value); }} /></label>
    <small id={nameHint}>{emailMessages.nameHint}</small>
    {refusal('name')}
    <label className="field"><span>{settingsMessages.supportAddress}</span>
      <input ref={addressField} type="email" required value={address} aria-invalid={invalid('address')} aria-describedby={invalid('address') ? errorID : undefined}
        onChange={(event) => { feedback.touch(); setAddress(event.target.value); }} /></label>
    {refusal('address')}
    <span className="button-row"><button className="primary-action" type="submit" disabled={feedback.pending}>{feedback.saveLabel(settingsMessages.saveSupportAddress)}</button></span>
  </form>;
}

// Overview is the workspace address every hosted forwarding address carries, on the channel list: one
// field for all the mailboxes, so it is changed knowing it changes them all.
function Overview({ defaultChannelID }: { defaultChannelID: string }) {
  const client = useQueryClient();
  const settings = useQuery({ queryKey: ['email-channel', defaultChannelID], queryFn: () => channelsApi.email(defaultChannelID) });
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const save = useMutation({ mutationFn: api.updateWorkspaceSlug,
    onSuccess: () => Promise.all(['me', 'email-channel'].map((key) => client.invalidateQueries({ queryKey: [key] }))) });
  if (!settings.data?.hosted_address || !me.data) return null;
  return <section className="settings-card" aria-labelledby="ch-address-heading">
    <div className="section-heading"><h2 id="ch-address-heading">{overviewMessages.heading}</h2><p>{overviewMessages.help}</p></div>
    <NameForm label={settingsMessages.workspaceAddressName} saveLabel={settingsMessages.saveAddressName} hint={settingsMessages.addressNameWarning} value={me.data.workspace.slug} canManage
      maxLength={63} save={saveState(save)} onSave={(slug) => save.mutate(slug)} />
  </section>;
}

const overviewMessages = {
  heading: 'Forwarding addresses',
  help: 'Your workspace address is part of the forwarding address of every mailbox: changing it changes them all, and mail sent to the old ones is refused.',
};

export const Tabs = { connection: Connection, replies: Replies };
export { Connect, Overview };
