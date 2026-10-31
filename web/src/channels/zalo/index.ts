import { ChatIcon } from '../../ui/icons';
import { lazyPart } from '../../ui/lazy-part';
import type { ChannelUI } from '../registry';

const capabilities: Record<string, string> = {
  threads: 'Keeps a customer’s chat in one conversation',
  attachments_in: 'Receives pictures and files',
};

// The Zalo OA type until its settings pages are designed: its words and one tab that says how it is set up.
export const zaloChannel: ChannelUI = {
  type: 'zalo',
  label: 'Zalo OA',
  description: 'Customers write to your Zalo Official Account, alone or in its groups; replies go back the same way.',
  connectLabel: 'Add Zalo OA',
  Icon: ChatIcon,
  capabilityLabel: (code) => capabilities[code],
  tabs: [{ id: 'setup', label: 'Setup' }],
  panels: lazyPart(() => import('./panels')),
};
