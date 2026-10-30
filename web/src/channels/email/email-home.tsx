import { useMutation, useQueries } from '@tanstack/react-query';
import { InvalidRequestError } from '../../api/client';
import { channelsApi } from '../../api/channels';
import type { Channel } from '../../api/channels';
import type { EmailChannelSettings } from '../../api/schemas';
import { formatDate } from '../../lib/format-date';
import { messages } from '../../messages';
import { PlusIcon } from '../../ui/icons';
import { EmailChannelSection } from '../../features/settings/email-channel-section';
import { saveState } from '../../features/settings/save-feedback';
import type { HomeProps } from '../registry';
import { TestMessage, useEmailSettings } from './email-shared';
import { ForwardingName } from './setup-steps';
import { setupMessages as m } from './setup-messages';
import { addressSaved, readMemory, repliesThrough, writeMemory } from './setup-state';
import { EmailSetup } from './setup-wizard';
import './email-setup.css';

const statusLabel: Record<Channel['status']['state'], string> = {
  working: 'Working', waiting: m.waitingStatus, off: 'Off', needs_attention: 'Needs attention', not_connected: 'Not connected', removed: 'Removed',
};
const statusClass: Record<Channel['status']['state'], string> = { working: 'ch-st-ok', waiting: 'ch-st-warn', off: 'ch-st-off', needs_attention: 'ch-st-warn', not_connected: 'ch-st-off', removed: 'ch-st-off' };

// EmailHome is Settings → Email: without an address, the setup; otherwise every support address with how
// it receives, how its replies leave and its state, and below it the settings people rarely change, folded.
export function EmailHome(props: HomeProps) {
  const { list, canManage, timezone, navigate } = props;
  const email = list.items.filter((item) => item.type === 'email');
  // Every row's settings at once, in parallel; the channel page reads the same cache.
  const settings = useQueries({ queries: email.map((item) => ({ queryKey: ['email-channel', item.id], queryFn: () => channelsApi.email(item.id) })) });
  const defaultID = list.types.find((info) => info.type === 'email')?.default_channel_id ?? email[0]?.id;
  if (email.length === 0) return <EmailSetup {...props} />;
  // A new cloud workspace's only channel still has its forwarding address as the support address: its
  // setup has not started, so the page is that setup.
  if (email.length === 1) {
    if (!settings[0].data) return <div className="ch-loading" aria-busy="true" />;
    if (!addressSaved(settings[0].data)) return <EmailSetup {...props} channel={email[0]} />;
  }
  const lastEmail = email.map((item) => item.last_inbound_at).filter((at): at is string => Boolean(at)).sort().at(-1);
  const main = email.find((item) => item.id === defaultID) ?? email[0];
  const mainSettings = settings[email.indexOf(main)]?.data;
  return <section className="settings-card email-home" aria-labelledby="email-home-heading">
    <div className="email-home-head">
      <div className="section-heading"><h2 id="email-home-heading">{m.addresses}</h2>{lastEmail && <p>{m.lastEmail(formatDate(lastEmail, timezone))}</p>}</div>
      {canManage && <a className="button ch-add" href="/settings/channels/new/email"><PlusIcon /><span>{m.addAnother}</span></a>}
    </div>
    <div className="scroll-x-fade"><table className="email-home-table" aria-label={m.addresses}>
      <thead><tr><th>{m.colAddress}</th><th>{m.colReceives}</th><th>{m.colReplies}</th><th>{m.colStatus}</th><th><span className="sr-only">{m.colAction}</span></th></tr></thead>
      <tbody>{email.map((item, index) => <Row key={item.id} channel={item} settings={settings[index]?.data} isDefault={email.length > 1 && item.id === defaultID} />)}</tbody>
    </table></div>
    {mainSettings?.channel && <Folds channel={main} settings={mainSettings} several={email.length > 1} canManage={canManage} timezone={timezone} navigate={navigate} channels={email} />}
  </section>;
}

function Row({ channel, settings, isDefault }: { channel: Channel; settings?: EmailChannelSettings; isDefault: boolean }) {
  const address = settings?.channel?.support_address ?? channel.summary;
  const unfinished = channel.status.state === 'waiting' || (settings !== undefined && !addressSaved(settings));
  return <tr className={channel.enabled ? undefined : 'ch-row-off'}>
    <td data-label={m.colAddress}><strong>{address}</strong>{isDefault && <> <span className="ch-default">{m.defaultMark}</span></>}
      {channel.name !== address && <small className="email-home-name">{channel.name}</small>}</td>
    <td data-label={m.colReceives}><code>{settings ? settings.hosted_address ?? m.inboundURL : '…'}</code></td>
    <td data-label={m.colReplies}>{settings ? m.through[repliesThrough(settings)] : '…'}</td>
    <td data-label={m.colStatus}><span className={`status ${statusClass[channel.status.state]}`}>{statusLabel[channel.status.state]}</span>
      {channel.held_count > 0 && <small className="email-home-name">{m.held(channel.held_count)}</small>}</td>
    <td className="email-home-action">{unfinished
      ? <a className="button primary-action" href={`/settings/channels/${encodeURIComponent(channel.id)}/setup`} aria-label={m.continueSetupOf(address)}>{m.continueSetup}</a>
      : <a className="button" href={`/settings/channels/${encodeURIComponent(channel.id)}`} aria-label={m.manageOf(address)}>{m.manage.split(' ')[0]}</a>}</td>
  </tr>;
}

interface FoldsProps { channel: Channel; settings: EmailChannelSettings; several: boolean; canManage: boolean; timezone: string; navigate: HomeProps['navigate']; channels: Channel[] }

// Folds are the default address's settings people rarely change; the dangerous ones come last.
function Folds({ channel, settings, several, canManage, timezone, navigate, channels }: FoldsProps) {
  const { refresh } = useEmailSettings(channel.id);
  const saveSenderName = useMutation({ mutationFn: (name: string) => channelsApi.update(channel.id, { config: { sender_name: name } }), onSuccess: refresh });
  const verifyDomain = useMutation({ mutationFn: () => channelsApi.emailVerifyDomain(channel.id), onSettled: refresh });
  const config = settings.channel!;
  const verified = config.domain_verified_at ? formatDate(config.domain_verified_at, timezone) : null;
  const which = several && <p className="muted-copy">{m.forWhich(config.support_address)}</p>;
  return <div className="email-home-folds">
    <details className="email-home-fold"><summary>{m.foldSender}</summary>{which}
      <EmailChannelSection part="sender" settings={settings} timezone={timezone} canManage={canManage} senderNameSave={saveState(saveSenderName)} onSaveSenderName={(name) => saveSenderName.mutate(name)} />
    </details>
    <details className="email-home-fold"><summary>{m.foldDomain(config.domain, verified)}</summary>{which}
      {verifyDomain.isError && !(verifyDomain.error instanceof InvalidRequestError) && <p role="alert" className="error-copy">{messages.settingsActionFailed}</p>}
      <EmailChannelSection part="domain" settings={settings} timezone={timezone} canManage={canManage} onVerifyDomain={() => verifyDomain.mutate()} verifyPending={verifyDomain.isPending}
        verifyError={verifyDomain.error instanceof InvalidRequestError ? verifyDomain.error.message : undefined} />
    </details>
    <details className="email-home-fold"><summary>{m.foldTest}</summary>{which}
      <TestMessage channelID={channel.id} ready={addressSaved(settings)} forwarding={channel.status.state === 'working'} navigate={navigate}
        onSent={(conversation) => writeMemory(channel.id, { ...readMemory(channel.id), testConversation: conversation })} />
    </details>
    {canManage && settings.hosted_address && <details className="email-home-fold email-home-danger"><summary>{m.foldChange}</summary>
      <ForwardingName />
    </details>}
    {/* One sentence and a link each: shown as they are, a fold would only hide them. */}
    <div className="email-home-disconnect">
      <p className="muted-copy">{m.disconnectHelp}</p>
      <ul className="email-home-manage" aria-label={m.disconnectLinks}>{channels.map((item) => <li key={item.id}>
        <a href={`/settings/channels/${encodeURIComponent(item.id)}`}>{m.manageOf(item.summary || item.name)}</a></li>)}</ul>
    </div>
  </div>;
}
