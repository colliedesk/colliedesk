import { useEffect, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '../../api/client';
import { channelsApi } from '../../api/channels';
import type { ChannelCreate, ChannelType } from '../../api/channels';
import type { SetupStepKey } from '../../api/schemas';
import { useCreateAction } from '../../api/use-create';
import { emailChannel } from '../../channels/email';
import type { SetupProps } from '../../channels/registry';
import { messages } from '../../messages';
import { lazyPart, Part, PartUnavailable } from '../../ui/lazy-part';
import { InvitesSection } from '../settings/invites-section';
import { useInvites } from '../settings/use-invites';
import { flowMessages as m } from './setup-flow-messages';

// The import page is a chunk of its own, shared with Settings → Import.
const importPart = lazyPart(() => import('../settings/import-route'));

interface StepProps {
  admin: boolean;
  timezone: string;
  navigate: (href: string, replace?: boolean) => void;
}

// StepBody is the step done in place, with the same parts Settings uses: no second copy of any form.
export function StepBody({ step, ...props }: StepProps & { step: SetupStepKey }) {
  switch (step) {
    case 'email': return <EmailStep {...props} />;
    case 'import': return <Part part={importPart} failed={<PartUnavailable />} render={({ ImportRoute }) => <ImportRoute embedded />} />;
    case 'invite': return <InviteStep {...props} />;
    default: return <FirstReplyStep {...props} />;
  }
}

// useSetupFollows reloads the setup progress whenever data a step changes reloads (not on its first load), so
// Continue follows a save at once instead of at the next poll.
function useSetupFollows(updatedAt: number) {
  const client = useQueryClient();
  const seen = useRef(updatedAt);
  useEffect(() => {
    const before = seen.current;
    seen.current = updatedAt;
    if (before !== 0 && before !== updatedAt) void client.invalidateQueries({ queryKey: ['setup'] });
  }, [client, updatedAt]);
}

function LoadFailed({ onRetry }: { onRetry: () => void }) {
  return <p role="alert" className="error-copy">{m.loadFailed}{' '}
    <button type="button" className="link-action" onClick={onRetry}>{messages.tryAgain}</button></p>;
}

const loading = <div className="ch-loading" aria-busy="true"><span className="sr-only">{messages.loadingList}</span></div>;

// EmailStep is the email setup of Settings → Email for the workspace's default mailbox, or a new one. A new
// mailbox's setup goes on here, where Settings would open the channel's own page.
function EmailStep({ admin, timezone, navigate }: StepProps) {
  const client = useQueryClient();
  const channels = useQuery({ queryKey: ['channels'], queryFn: channelsApi.list });
  useSetupFollows(channels.dataUpdatedAt);
  const createChannel = useCreateAction((input: ChannelCreate, key: string) => channelsApi.create(input, key));
  const create: SetupProps['create'] = async (input) => {
    const created = await createChannel.run({ type: 'email' as ChannelType, ...input });
    await client.invalidateQueries({ queryKey: ['channels'] });
    return created;
  };
  if (channels.isError) return <LoadFailed onRetry={() => void channels.refetch()} />;
  if (!channels.data) return loading;
  const list = channels.data;
  const email = list.items.filter((item) => item.type === 'email');
  const defaultID = list.types.find((info) => info.type === 'email')?.default_channel_id;
  const channel = email.find((item) => item.id === defaultID) ?? email[0];
  // key: the mailbox just created starts its setup afresh, at the step after the address.
  return <div className="setup-flow-email"><Part part={emailChannel.panels} failed={<PartUnavailable />} render={({ Setup }) => Setup
    && <Setup key={channel?.id ?? 'new'} channel={channel} list={list} canManage={admin} timezone={timezone} navigate={navigate} create={create} />} /></div>;
}

function InviteStep({ admin, timezone }: StepProps) {
  const { section, revokeFailed } = useInvites();
  return <div className="setup-flow-invites">
    {revokeFailed && <p role="alert" className="error-copy">{messages.settingsActionFailed}</p>}
    <InvitesSection {...section} timezone={timezone} canCreateInvites={admin} />
  </div>;
}

// FirstReplyStep gives a workspace with a mailbox a first conversation to answer: a test message from the
// person's own address, opened at once, as the empty inbox does.
function FirstReplyStep({ admin, navigate }: StepProps) {
  const client = useQueryClient();
  const counts = useQuery({ queryKey: ['channels', 'counts'], queryFn: api.channelCounts });
  const test = useCreateAction((_nothing: void, key: string) => api.sendTestMessage(undefined, key));
  const [failed, setFailed] = useState<string>();
  if (counts.isError) return <LoadFailed onRetry={() => void counts.refetch()} />;
  if (!counts.data) return loading;
  const mailboxes = counts.data.types.find((info) => info.type === 'email')?.count ?? 0;
  if (mailboxes === 0) return <p className="muted-copy">{m.noMailbox}{admin && <>{' '}<a href="/setup/email">{m.setUpEmail}</a></>}</p>;
  const send = () => {
    setFailed(undefined);
    test.run().then((sent) => {
      void client.invalidateQueries({ queryKey: ['conversations'] });
      navigate(`/conversations/${encodeURIComponent(sent.conversation_id)}`);
    }, (error: unknown) => setFailed(error instanceof Error && !error.message.startsWith('API ') ? error.message : messages.testMessageFailed));
  };
  return <div className="setup-flow-test">
    <p><button type="button" disabled={test.pending} onClick={send}>{messages.sendTestMessage}</button></p>
    <p className="muted-copy">{messages.sendTestMessageHint}</p>
    {failed && <p role="alert" className="error-copy">{failed}</p>}
  </div>;
}
