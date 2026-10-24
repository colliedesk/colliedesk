import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, InvalidRequestError } from '../../api/client';
import { Dialog } from '../../ui/dialog';
import { messages } from '../../messages';
import { EmailChannelSection, gmailPart } from '../../features/settings/email-channel-section';
import { gmailReturn } from '../../features/settings/gmail-return';
import { saveState } from '../../features/settings/save-feedback';
import type { ChannelPanelProps, ConnectProps } from '../registry';

// The email channel's settings and actions keep their own routes (/api/v1/channels/email…): the list,
// the channel page (its state and notice) and its history show what changes, so every action reloads them.
function useEmailSettings(channelID?: string) {
  const client = useQueryClient();
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const settings = useQuery({ queryKey: ['email-channel'], queryFn: api.emailChannel });
  const refresh = () => Promise.all(['email-channel', 'channels', 'channel', 'channel-events']
    .map((key) => client.invalidateQueries({ queryKey: key === 'channel' && channelID ? [key, channelID] : [key] })));
  return { me, settings, refresh };
}

function Connection({ channel, canManage, timezone }: ChannelPanelProps) {
  const client = useQueryClient();
  const { me, settings, refresh } = useEmailSettings(channel.id);
  const saveEmail = useMutation({ mutationFn: (address: string) => api.saveEmailChannel(address), onSuccess: refresh });
  const workspaceSlug = useMutation({ mutationFn: api.updateWorkspaceSlug, onSuccess: () => { void client.invalidateQueries({ queryKey: ['me'] }); void refresh(); } });
  const rotateSecret = useMutation({ mutationFn: api.rotateEmailSecret, onSuccess: refresh });
  // A new secret stops the old one at once (a forwarder still using it fails), so replacing one asks first.
  const [askingRotate, setAskingRotate] = useState(false);
  const secretSet = settings.data?.channel?.secret_set === true;
  return <>
    {rotateSecret.isError && <p role="alert" className="error-copy">{rotateSecret.error instanceof InvalidRequestError ? rotateSecret.error.message : messages.settingsActionFailed}</p>}
    <EmailChannelSection part="connection" settings={settings.data} timezone={timezone} canManage={canManage} workspaceSlug={me.data?.workspace.slug}
      addressSave={saveState(saveEmail)} slugSave={saveState(workspaceSlug)} onSave={(address) => saveEmail.mutate(address)}
      onSaveSlug={(slug) => workspaceSlug.mutate(slug)} createdSecret={rotateSecret.data?.inbound_secret}
      onRotate={() => (secretSet ? setAskingRotate(true) : rotateSecret.mutate())} />
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
};

function Replies({ channel, canManage, timezone }: ChannelPanelProps) {
  const { settings, refresh } = useEmailSettings(channel.id);
  const saveSenderName = useMutation({ mutationFn: (name: string) => api.saveEmailSenderName(name), onSuccess: refresh });
  // A refused check still reloads: the provider's per-record status changed.
  const verifyDomain = useMutation({ mutationFn: api.verifyEmailDomain, onSettled: refresh });
  // Connecting Gmail leaves for Google's consent page; Google sends the browser back here with the outcome.
  const [gmailReturned] = useState(gmailReturn);
  useEffect(gmailPart.preload, []);
  const connectGmail = useMutation({ mutationFn: api.connectGmail, onSuccess: ({ url }) => window.location.assign(url) });
  const disconnectGmail = useMutation({ mutationFn: api.disconnectGmail, onSettled: refresh });
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

// Connect is the support address form; the channel exists once it is saved.
function Connect({ canManage, timezone, create }: ConnectProps) {
  const client = useQueryClient();
  const { me, settings, refresh } = useEmailSettings();
  const save = useMutation({ mutationFn: (address: string) => create({ config: { support_address: address } }), onSuccess: refresh });
  const workspaceSlug = useMutation({ mutationFn: api.updateWorkspaceSlug, onSuccess: () => { void client.invalidateQueries({ queryKey: ['me'] }); void refresh(); } });
  return <EmailChannelSection part="connection" settings={settings.data} timezone={timezone} canManage={canManage} workspaceSlug={me.data?.workspace.slug}
    addressSave={saveState(save)} onSave={(address) => save.mutate(address)} slugSave={saveState(workspaceSlug)} onSaveSlug={(slug) => workspaceSlug.mutate(slug)} />;
}

export const Tabs = { connection: Connection, replies: Replies };
export { Connect };
