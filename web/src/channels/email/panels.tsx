import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, InvalidRequestError } from '../../api/client';
import { channelsApi } from '../../api/channels';
import { messages } from '../../messages';
import { EmailChannelSection, gmailPart } from '../../features/settings/email-channel-section';
import { gmailReturn } from '../../features/settings/gmail-return';
import { saveState } from '../../features/settings/save-feedback';
import type { ChannelPanelProps, ConnectProps } from '../registry';

// The email channel's settings and actions keep their own routes (/api/v1/channels/email…): the list and
// the channel page show what changes, so every action reloads both.
function useEmailSettings() {
  const client = useQueryClient();
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const settings = useQuery({ queryKey: ['email-channel'], queryFn: api.emailChannel });
  const refresh = () => Promise.all([client.invalidateQueries({ queryKey: ['email-channel'] }), client.invalidateQueries({ queryKey: ['channels'] })]);
  return { me, settings, refresh };
}

function Connection({ channel, canManage, timezone, onRemoved }: ChannelPanelProps) {
  const client = useQueryClient();
  const { me, settings, refresh } = useEmailSettings();
  const saveEmail = useMutation({ mutationFn: (address: string) => api.saveEmailChannel(address), onSuccess: refresh });
  const workspaceSlug = useMutation({ mutationFn: api.updateWorkspaceSlug, onSuccess: () => { void client.invalidateQueries({ queryKey: ['me'] }); void refresh(); } });
  const rotateSecret = useMutation({ mutationFn: api.rotateEmailSecret, onSuccess: refresh });
  // Disconnecting removes the channel (its Gmail connection with it); one that holds mail is refused, with why.
  const disconnect = useMutation({ mutationFn: () => channelsApi.remove(channel.id), onSuccess: async () => { rotateSecret.reset(); await refresh(); onRemoved(); } });
  const failure = disconnect.error ?? rotateSecret.error;
  return <>
    {failure && <p role="alert" className="error-copy">{failure instanceof InvalidRequestError ? failure.message : messages.settingsActionFailed}</p>}
    <EmailChannelSection part="connection" settings={settings.data} timezone={timezone} canManage={canManage} workspaceSlug={me.data?.workspace.slug}
      addressSave={saveState(saveEmail)} slugSave={saveState(workspaceSlug)} onSave={(address) => saveEmail.mutate(address)}
      onSaveSlug={(slug) => workspaceSlug.mutate(slug)} createdSecret={rotateSecret.data?.inbound_secret}
      onRotate={() => rotateSecret.mutate()} onDisconnect={() => disconnect.mutate()} />
  </>;
}

function Replies({ canManage, timezone }: ChannelPanelProps) {
  const { settings, refresh } = useEmailSettings();
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
