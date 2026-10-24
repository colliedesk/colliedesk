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
  removesItself: true,
  caches: ['email-channel'],
};
