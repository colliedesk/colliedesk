import { ChatIcon } from '../../ui/icons';
import { lazyPart } from '../../ui/lazy-part';
import type { ChannelUI } from '../registry';

const capabilities: Record<string, string> = {
  help_center_chat: 'Chat on the help center',
  site_chat: 'Chat on your site',
  handoff: 'Talk to a person',
};

export const helpChatChannel: ChannelUI = {
  type: 'help_chat',
  label: 'Help chat',
  description: 'An AI answers from your help articles; Talk to a person opens a conversation here.',
  connectLabel: 'Add help chat',
  Icon: ChatIcon,
  capabilityLabel: (code) => capabilities[code],
  tabs: [{ id: 'setup', label: 'Setup' }, { id: 'embed', label: 'Your site' }],
  panels: lazyPart(() => import('./panels')),
  fieldLabel: (field) => ({ greeting: 'greeting', suggestions: 'suggested questions', instructions: 'extra instructions', widget_origins: 'sites' })[field],
  defaultHelp: 'A help chat answers customers with AI on your help center or your site and sends nothing out; Talk to a person opens a conversation here. The default one is the chat of the help center\'s own pages.',
  defaultBadge: 'Default: the help center\'s chat',
  waitingHelp: 'Open your help center\'s chat page, ask a question, then select Talk to a person: a conversation opens in your inbox.',
};
