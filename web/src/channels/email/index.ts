import { settingsMessages } from '../../features/settings/settings-messages';
import { MailIcon } from '../../ui/icons';
import { lazyPart } from '../../ui/lazy-part';
import type { ChannelUI } from '../registry';

const capabilities: Record<string, string> = {
  threads: 'Keeps replies in one conversation', attachments_in: 'Receives files', attachments_out: 'Sends files', cc: 'Copies other people (Cc)', html: 'Formatted text',
};

export const emailChannel: ChannelUI = {
  type: 'email',
  label: 'Email',
  description: 'Mail to your support address opens conversations; replies go out by email.',
  connectLabel: 'Connect your support email',
  Icon: MailIcon,
  capabilityLabel: (code) => capabilities[code],
  tabs: [{ id: 'connection', label: 'Connection' }, { id: 'replies', label: 'Replies' }],
  panels: lazyPart(() => import('./panels')),
  removal: { label: settingsMessages.disconnectEmail, title: settingsMessages.disconnectEmailQuestion, confirm: settingsMessages.confirmDisconnectEmail,
    body: (channel) => settingsMessages.disconnectEmailConsequence(channel.summary) },
  caches: ['email-channel'],
  waitingHelp: 'Nothing has arrived yet. Press Send yourself a test message on the Connection tab, or email your support address from a different address than the support address (mail from the support address itself never opens a conversation): it opens a conversation in the inbox, and this channel then reads Working.',
  fieldLabel: (field) => ({ support_address: 'support address', sender_name: 'sender name', inbound_secret: 'inbound secret' })[field],
};
