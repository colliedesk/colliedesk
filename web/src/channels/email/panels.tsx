import { useEffect, useId, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ConflictError, InvalidRequestError } from '../../api/client';
import { channelsApi } from '../../api/channels';
import { Dialog } from '../../ui/dialog';
import { messages } from '../../messages';
import { EmailChannelSection, gmailPart } from '../../features/settings/email-channel-section';
import { gmailReturn } from '../../features/settings/gmail-return';
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

function Connection({ channel, canManage, timezone }: ChannelPanelProps) {
  const { settings, refresh } = useEmailSettings(channel.id);
  const saveEmail = useMutation({ mutationFn: (address: string) => channelsApi.update(channel.id, { config: { support_address: address } }), onSuccess: refresh });
  const rotateSecret = useMutation({ mutationFn: () => channelsApi.emailSecret(channel.id), onSuccess: refresh });
  // A new secret stops the old one at once (a forwarder still using it fails), so replacing one asks first.
  const [askingRotate, setAskingRotate] = useState(false);
  const secretSet = settings.data?.channel?.secret_set === true;
  return <>
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

// Words of the email tabs that the Settings page never had.
const emailMessages = {
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
  // Connecting Gmail leaves for Google's consent page; Google sends the browser back here with the outcome.
  const [gmailReturned] = useState(gmailReturn);
  useEffect(gmailPart.preload, []);
  const connectGmail = useMutation({ mutationFn: () => channelsApi.emailConnectGmail(channel.id), onSuccess: ({ url }) => window.location.assign(url) });
  const disconnectGmail = useMutation({ mutationFn: () => channelsApi.emailDisconnectGmail(channel.id), onSettled: refresh });
  const gmailFailure = connectGmail.error ?? disconnectGmail.error;
  const gmailError = gmailFailure ? (gmailFailure instanceof InvalidRequestError ? gmailFailure.message : messages.settingsActionFailed) : undefined;
  return <>
    {verifyDomain.isError && !(verifyDomain.error instanceof InvalidRequestError) && <p role="alert" className="error-copy">{messages.settingsActionFailed}</p>}
    <EmailChannelSection part="replies" settings={settings.data} timezone={timezone} canManage={canManage}
      senderNameSave={saveState(saveSenderName)} onSaveSenderName={(name) => saveSenderName.mutate(name)}
      onVerifyDomain={() => verifyDomain.mutate()} verifyPending={verifyDomain.isPending}
      verifyError={verifyDomain.error instanceof InvalidRequestError ? verifyDomain.error.message : undefined}
      onConnectGmail={() => connectGmail.mutate()} onDisconnectGmail={() => disconnectGmail.mutate()}
      gmailPending={connectGmail.isPending || connectGmail.isSuccess} gmailError={gmailError} gmailReturned={gmailReturned} />
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
  const feedback = useSaveFeedback(state, errorID);
  // The refusal is about the field it names: the name or the support address.
  const about = save.error instanceof ConflictError && save.error.detail?.code === 'channel_name_taken' ? 'name' : 'address';
  // A teammate reads what is set, as on a connected channel.
  if (!canManage) return <p><strong>{messages.supportAddress}: </strong><span>{messages.emailNotConnected}</span></p>;
  const invalid = (field: 'name' | 'address') => (feedback.invalid && about === field) || undefined;
  return <form className="settings-body" onSubmit={(event) => { event.preventDefault(); if (!feedback.pending) save.mutate(); }}>
    <label className="field"><span>{emailMessages.name}</span>
      <input value={name} maxLength={80} placeholder={address} aria-invalid={invalid('name')} aria-describedby={invalid('name') ? `${errorID} ${nameHint}` : nameHint}
        onChange={(event) => { feedback.touch(); setName(event.target.value); }} /></label>
    <small id={nameHint}>{emailMessages.nameHint}</small>
    <label className="field"><span>{messages.supportAddress}</span>
      <input type="email" required value={address} aria-invalid={invalid('address')} aria-describedby={invalid('address') ? errorID : undefined}
        onChange={(event) => { feedback.touch(); setAddress(event.target.value); }} /></label>
    <span className="button-row"><button className="primary-action" type="submit" disabled={feedback.pending}>{feedback.saveLabel(messages.saveSupportAddress)}</button></span>
    {feedback.status}
    {feedback.invalid && state.conflict && conflictLink(state.conflict)}
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
    <NameForm label={messages.workspaceAddressName} saveLabel={messages.saveAddressName} hint={messages.addressNameWarning} value={me.data.workspace.slug} canManage
      maxLength={63} save={saveState(save)} onSave={(slug) => save.mutate(slug)} />
  </section>;
}

const overviewMessages = {
  heading: 'Forwarding addresses',
  help: 'Your workspace address is part of the forwarding address of every mailbox: changing it changes them all, and mail sent to the old ones is refused.',
};

export const Tabs = { connection: Connection, replies: Replies };
export { Connect, Overview };
