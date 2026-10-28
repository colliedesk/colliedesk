import { FormIcon } from '../../ui/icons';
import { lazyPart } from '../../ui/lazy-part';
import type { ChannelUI } from '../registry';

const capabilities: Record<string, string> = { form: 'A form on your website', attachments_in: 'Receives files' };

export const webFormChannel: ChannelUI = {
  type: 'web_form',
  label: 'Web form',
  description: 'A form on your website opens a conversation here.',
  connectLabel: 'Add a web form',
  Icon: FormIcon,
  capabilityLabel: (code) => capabilities[code],
  tabs: [{ id: 'embed', label: 'Embed' }],
  panels: lazyPart(() => import('./panels')),
  caches: ['web-form'],
  fieldLabel: (field) => ({ allowed_origins: 'allowed origins' })[field],
};
