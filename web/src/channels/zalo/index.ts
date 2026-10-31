import { ChatIcon } from '../../ui/icons';
import { lazyPart } from '../../ui/lazy-part';
import type { ChannelUI } from '../registry';

const capabilities: Record<string, string> = { threads: 'One-to-one chats and groups', attachments_in: 'Receives pictures and files', attachments_out: 'Sends pictures' };

const fields: Record<string, string> = {
  oa_id: 'OA ID', app_id: 'app ID', staff_user_ids: 'staff accounts', groups: 'groups',
  app_secret: 'app secret', oa_secret_key: 'OA secret key', access_token: 'access token', refresh_token: 'refresh token',
  commands: 'commands', notices: 'notices',
};

export const zaloChannel: ChannelUI = {
  type: 'zalo',
  label: 'Zalo OA',
  description: 'Messages to your Zalo Official Account, one-to-one and in groups, come to your team here.',
  connectLabel: 'Connect Zalo OA',
  Icon: ChatIcon,
  capabilityLabel: (code) => capabilities[code],
  tabs: [{ id: 'connection', label: 'Connection' }, { id: 'groups', label: 'Groups' }, { id: 'staff', label: 'Staff' }, { id: 'messages', label: 'Messages' }],
  panels: lazyPart(() => import('./panels')),
  fieldLabel: (field) => fields[field],
  waitingHelp: 'Send a message to the OA from Zalo: a conversation opens in your inbox.',
  removal: { label: 'Remove Zalo OA', title: 'Remove this Zalo OA?', confirm: 'Remove',
    body: () => 'Colliedesk stops receiving and sending on this OA and erases its secrets. Its conversations stay in the inbox.' },
  defaultHelp: 'Each Zalo Official Account is a channel of its own: its one-to-one chats and its groups come in, and replies go back where they were said. New conversations you start go out from the default channel.',
  defaultBadge: 'Default Zalo OA',
  caches: ['zalo-threads'],
  pausable: true,
};
