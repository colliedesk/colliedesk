import { useEffect, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { InvalidRequestError } from '../../api/client';
import { channelsApi } from '../../api/channels';
import { useCreateMutation } from '../../api/use-create';
import { messages } from '../../messages';
import { Dialog } from '../../ui/dialog';
import type { MailboxProvider } from '../../features/settings/gmail-return';
import type { SaveState } from '../../features/settings/save-feedback';

// One email channel of several: its settings and actions go through /api/v1/channels/{id}/email… and
// PATCH /api/v1/channels/{id}; the list, the channel page (its state and notice) and its history show
// what changes, so every action reloads them.
export function useEmailSettings(channelID: string) {
  const client = useQueryClient();
  const settings = useQuery({ queryKey: ['email-channel', channelID], queryFn: () => channelsApi.email(channelID) });
  const refresh = () => Promise.all(['email-channel', 'channels', 'channel', 'channel-events']
    .map((key) => client.invalidateQueries({ queryKey: key === 'channel' ? [key, channelID] : [key] })));
  return { settings, refresh };
}

// useMailbox connects the channel's mailbox (Gmail or Microsoft 365), which leaves for the provider's
// sign-in page, and disconnects it. before runs just before the browser leaves.
export function useMailbox(channelID: string, refresh: () => Promise<unknown>, before?: () => void) {
  const connect = useMutation({
    mutationFn: (provider: MailboxProvider) => (provider === 'microsoft' ? channelsApi.emailConnectMicrosoft : channelsApi.emailConnectGmail)(channelID),
    onSuccess: ({ url }) => { before?.(); window.location.assign(url); },
  });
  const disconnect = useMutation({
    mutationFn: (provider: MailboxProvider) => (provider === 'microsoft' ? channelsApi.emailDisconnectMicrosoft : channelsApi.emailDisconnectGmail)(channelID),
    onSettled: refresh,
  });
  const failure = connect.error ?? disconnect.error;
  return {
    connect: (provider: MailboxProvider) => connect.mutate(provider),
    disconnect: (provider: MailboxProvider) => disconnect.mutate(provider),
    pending: connect.isPending || connect.isSuccess ? connect.variables : undefined,
    error: failure ? (failure instanceof InvalidRequestError ? failure.message : messages.settingsActionFailed) : undefined,
  };
}

// useInboundSecret creates the inbound secret a forwarder posts with, or replaces it after a question: a new
// secret stops the old one at once (a forwarder still using it fails). The new secret is shown once.
export function useInboundSecret(channelID: string, secretSet: boolean, refresh: () => Promise<unknown>) {
  const rotate = useMutation({ mutationFn: () => channelsApi.emailSecret(channelID), onSuccess: refresh });
  const [asking, setAsking] = useState(false);
  return {
    created: rotate.data?.inbound_secret,
    onRotate: () => (secretSet ? setAsking(true) : rotate.mutate()),
    feedback: <>
      {rotate.isError && <p role="alert" className="error-copy">{rotate.error instanceof InvalidRequestError ? rotate.error.message : messages.settingsActionFailed}</p>}
      {asking && <Dialog alert title={emailMessages.rotateTitle} closeLabel={messages.cancel} onClose={() => setAsking(false)}
        onSubmit={() => { setAsking(false); rotate.mutate(); }}
        footer={<><button type="button" autoFocus onClick={() => setAsking(false)}>{messages.cancel}</button>
          <button type="submit" className="danger-action">{emailMessages.rotateConfirm}</button></>}>
        <p>{emailMessages.rotateBody}</p>
      </Dialog>}
    </>,
  };
}

// A link to the channel that has a value refused here.
export const conflictLink = (conflict: NonNullable<SaveState['conflict']>) =>
  <a href={`/settings/channels/${encodeURIComponent(conflict.channel_id)}`}>{emailMessages.openChannel(conflict.channel_name)}</a>;

// TestMessage sends the signed-in person a test email on this channel, the way a customer's arrives, and
// opens the conversation it started, as the inbox's action does: the whole loop without a forwarder or a
// terminal. Every press ends in something on screen: that conversation, why it could not, or what to do first.
export function TestMessage({ channelID, ready, forwarding, navigate, onSent }: { channelID: string; ready: boolean; forwarding: boolean; navigate?: (href: string) => void;
  onSent?: (conversationID: string) => void }) {
  const client = useQueryClient();
  const [notReady, setNotReady] = useState(false);
  // A press while one is in flight sends nothing; one sent again after a failure goes under the same key.
  const send = useCreateMutation({ create: (_nothing: void, key: string) => channelsApi.emailTestMessage(channelID, key),
    onSuccess: (sent) => {
      onSent?.(sent.conversation_id);
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
export const emailMessages = {
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
