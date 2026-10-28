import { messages } from '../../messages';
import { MailIcon } from '../../ui/icons';
import { lazyPart } from '../../ui/lazy-part';
import type { ChannelUI } from '../registry';

const capabilities: Record<string, string> = {
  threads: 'Threads replies', attachments_in: 'Attachments in', attachments_out: 'Attachments out', cc: 'Cc', html: 'Formatted mail',
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
  removal: { label: messages.disconnectEmail, title: messages.disconnectEmailQuestion, confirm: messages.confirmDisconnectEmail,
    body: (channel) => messages.disconnectEmailConsequence(channel.summary) },
  caches: ['email-channel'],
  waitingHelp: 'Nothing has arrived yet. Send a test message to your support address (on your own server, post one as the Connection tab shows): it opens a conversation in the inbox, and this channel then reads Working.',
  fieldLabel: (field) => ({ support_address: 'support address', sender_name: 'sender name', inbound_secret: 'inbound secret' })[field],
};
