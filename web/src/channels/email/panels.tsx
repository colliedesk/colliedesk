import { useEffect, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { InvalidRequestError } from '../../api/client';
import { channelsApi } from '../../api/channels';
import { messages } from '../../messages';
import { EmailChannelSection, gmailPart } from '../../features/settings/email-channel-section';
import { mailboxReturn } from '../../features/settings/gmail-return';
import { saveState } from '../../features/settings/save-feedback';
import type { ChannelPanelProps, ConnectProps } from '../registry';
import { AddressForm } from './address-form';
import { conflictLink, TestMessage, useEmailSettings, useInboundSecret, useMailbox } from './email-shared';
import { EmailHome } from './email-home';
import { EmailSetup } from './setup-wizard';

function Connection({ channel, canManage, timezone, navigate }: ChannelPanelProps) {
  const { settings, refresh } = useEmailSettings(channel.id);
  const saveEmail = useMutation({ mutationFn: (address: string) => channelsApi.update(channel.id, { config: { support_address: address } }), onSuccess: refresh });
  const secretSet = settings.data?.channel?.secret_set === true;
  const secret = useInboundSecret(channel.id, secretSet, refresh);
  return <>
    {settings.data && <TestMessage channelID={channel.id} ready={Boolean(settings.data.channel) || Boolean(channel.config && 'support_address' in channel.config && channel.config.support_address)} navigate={navigate}
      forwarding={secretSet || channel.status.reason_code !== 'no_inbound_secret'} />}
    {secret.feedback}
    <EmailChannelSection part="connection" settings={settings.data} timezone={timezone} canManage={canManage}
      addressSave={saveState(saveEmail)} onSave={(address) => saveEmail.mutate(address)} addressConflictLink={conflictLink}
      createdSecret={secret.created} onRotate={secret.onRotate} />
  </>;
}

function Replies({ channel, canManage, timezone }: ChannelPanelProps) {
  const { settings, refresh } = useEmailSettings(channel.id);
  const saveSenderName = useMutation({ mutationFn: (name: string) => channelsApi.update(channel.id, { config: { sender_name: name } }), onSuccess: refresh });
  // A refused check still reloads: the provider's per-record status changed.
  const verifyDomain = useMutation({ mutationFn: () => channelsApi.emailVerifyDomain(channel.id), onSettled: refresh });
  // Connecting a mailbox leaves for the provider's sign-in page (Google, Microsoft); the provider sends the
  // browser back here with the outcome.
  const [mailboxReturned] = useState(mailboxReturn);
  useEffect(gmailPart.preload, []);
  const mailbox = useMailbox(channel.id, refresh);
  return <>
    {verifyDomain.isError && !(verifyDomain.error instanceof InvalidRequestError) && <p role="alert" className="error-copy">{messages.settingsActionFailed}</p>}
    <EmailChannelSection part="replies" settings={settings.data} timezone={timezone} canManage={canManage}
      senderNameSave={saveState(saveSenderName)} onSaveSenderName={(name) => saveSenderName.mutate(name)}
      onVerifyDomain={() => verifyDomain.mutate()} verifyPending={verifyDomain.isPending}
      verifyError={verifyDomain.error instanceof InvalidRequestError ? verifyDomain.error.message : undefined}
      onConnectMailbox={mailbox.connect} onDisconnectMailbox={mailbox.disconnect}
      mailboxPending={mailbox.pending} mailboxError={mailbox.error}
      mailboxReturned={mailboxReturned} />
  </>;
}

// Connect asks a name and the support address; the channel exists once they are saved. A name left empty
// is the address (the first mailbox is called Email).
function Connect({ canManage, create }: ConnectProps) {
  const client = useQueryClient();
  return <AddressForm canManage={canManage} named save={({ name, address }) => create({ ...(name ? { name } : {}), config: { support_address: address } })}
    onSaved={() => void client.invalidateQueries({ queryKey: ['email-channel'] })} />;
}

export const Tabs = { connection: Connection, replies: Replies };
export { Connect, EmailHome as Home, EmailSetup as Setup };
