import type { ChannelEvent } from '../../../api/channels';
import { channelUI } from '../../../channels/registry';

// The words of the Channels pages. They load with those pages, not in the first bundle.
export const channelsMessages = {
  heading: 'Channels',
  description: 'Where customers reach you. Each channel opens conversations in the same inbox; turn one off to stop it without losing anything.',
  tabs: 'Channels pages',
  tabChannels: 'Channels',
  tabHistory: 'History',
  loading: 'Loading channels…',
  loadFailed: 'Channels could not be loaded.',
  tryAgain: 'Try again',
  emptyTitle: 'No channel yet',
  emptyBody: 'Connect the first way customers reach you. You can add more later.',
  addChannel: 'Add channel',
  addChannelHelp: 'Choose how customers will reach you.',
  notAvailableYet: 'Not available yet',
  trackedIn: (issue: number) => `Follow #${issue}`,
  close: 'Close',
  cancel: 'Cancel',
  continue: 'Continue',
  configure: 'Configure',
  configureName: (name: string) => `Configure ${name}`,
  backToChannels: 'All channels',
  statusWorking: 'Working',
  statusWaiting: 'Waiting for first message',
  // A waiting channel's page says how to prove the way in works.
  waitingHelp: (type: string) => type === 'email'
    ? 'Nothing has arrived yet. Send a test message to your support address (on your own server, post one as the Connection tab shows): it opens a conversation in the inbox, and this channel then reads Working.'
    : 'Nothing has arrived yet. Send a test message through it: it opens a conversation in the inbox, and this channel then reads Working.',
  statusOff: 'Off',
  statusNeedsAttention: 'Needs attention',
  statusNotConnected: 'Not connected',
  statusRemoved: 'Removed',
  defaultLabel: 'Default',
  defaultOf: (type: string) => `Default ${type} channel`,
  makeDefault: 'Make default',
  madeDefault: (name: string) => `${name} is now the default. A new conversation that names no channel, and the older links and addresses, use it.`,
  defaultHelp: 'The default channel is the one a conversation started without choosing a channel leaves from, and the one older links and addresses reach.',
  nameLabel: 'Name',
  saveName: 'Save name',
  nameSaved: 'Name saved.',
  openTaken: (name: string) => `Open ${name}`,
  removeDefaultNext: (next: string) => `${next} becomes the default in its place.`,
  removeDefaultNone: (type: string) => `No ${type} channel will be left.`,

  saving: 'Saving…',
  turnedOn: 'On',
  turnedOff: 'Off',
  switchFailed: 'The channel could not be changed. Try again.',
  actionFailed: 'The action failed. Try again.',
  turnOffTitle: (name: string) => `Turn ${name} off?`,
  turnOffBody: 'It stops receiving and sending for the whole team. What arrives meanwhile is kept here for you to open or discard; replies are saved but not sent. Its settings and conversations stay.',
  turnOff: 'Turn off',
  needsAttention: 'Needs attention',
  fix: 'Fix it',
  reason: (code: string | null, message: string | null): string => message ?? reasons[code ?? ''] ?? 'Check its settings.',
  held: (count: number, name: string) => `${count} ${count === 1 ? 'message' : 'messages'} arrived while ${name} was off.`,
  openHeld: 'Open them as conversations',
  discardHeld: (count: number) => `Discard ${count === 1 ? 'it' : `${count} messages`}…`,
  discardTitle: (count: number) => `Discard ${count} ${count === 1 ? 'message' : 'messages'}?`,
  discardBody: 'They are deleted for good and never open as conversations. This cannot be undone; History keeps that they were discarded.',
  discard: 'Discard',
  heldOpened: (count: number, skipped: number) => `${count} ${count === 1 ? 'message' : 'messages'} opened in the inbox.`
    + (skipped > 0 ? ` ${skipped} ${skipped === 1 ? 'was' : 'were'} already there or set aside by the channel's rules.` : ''),
  viewInInbox: 'View them in the inbox',
  heldDiscarded: (count: number) => `${count} ${count === 1 ? 'message' : 'messages'} discarded.`,
  unsent: (count: number, name: string) => `${count} ${count === 1 ? 'reply was' : 'replies were'} not sent while ${name} was off. Retry them from the conversation.`,
  seeThem: 'See them',
  remove: 'Remove channel',
  removeTitle: (name: string) => `Remove ${name}?`,
  removeBody: 'It stops receiving and sending, and its settings and secrets are deleted. Its conversations stay and keep its name. Adding it again makes a new channel.',
  removeConfirm: 'Yes, remove',
  removed: (name: string) => `${name} was removed.`,
  removeHeld: (count: number) => `It holds ${count} ${count === 1 ? 'message' : 'messages'} that arrived while it was off: open or discard ${count === 1 ? 'it' : 'them'} first.`,
  notFound: 'This channel was removed or does not exist.',
  unknownType: 'This kind of channel is not available on this server.',
  connectHeading: (label: string) => `Connect ${label}`,
  adminsOnly: 'Only an admin can add or change channels.',
  capabilities: 'Carries',
  historyHeading: 'History',
  historyEmpty: 'Nothing has changed yet.',
  historyHelp: 'Who added, changed, turned on or off and removed a channel. Secrets are never shown.',
  someone: 'Someone',
  viaToken: (label: string) => ` (agent token ${label})`,
  showMore: 'Show more',
  event: (event: ChannelEvent): string => {
    const ui = channelUI(event.channel_type);
    const names = (event.fields ?? []).map((field) => ui?.fieldLabel?.(field) ?? (field === 'settings' ? 'rules' : field.replaceAll('_', ' ')));
    const what = names.length > 0 ? names.join(' and ') : undefined;
    const count = event.count ?? undefined;
    return eventWords[event.kind](event.channel_name, what, count);
  },
};

// reasons explains a needs-attention code (Channel.status.reason_code) when the server gave no message.
const reasons: Record<string, string> = {
  sending_paused: 'Sending is paused.',
  cannot_send: 'Replies cannot be sent yet.',
  gmail_broken: 'The Gmail connection stopped working.',
  microsoft_broken: 'The Microsoft 365 connection stopped working.',
  domain_unverified: 'The domain is not verified yet.',
  no_inbound_secret: 'No inbound secret is set, so no mail can arrive.',
  no_origins: 'No website is allowed to post to it yet.',
};

const messagesOf = (count: number | undefined) => (count === undefined ? 'messages' : `${count} ${count === 1 ? 'message' : 'messages'}`);
const eventWords: Record<ChannelEvent['kind'], (name: string, what?: string, count?: number) => string> = {
  added: (name) => `added ${name}`,
  renamed: (name) => `renamed a channel to ${name}`,
  enabled: (name) => `turned ${name} on`,
  disabled: (name) => `turned ${name} off`,
  config_changed: (name, what) => `changed the ${what ?? 'settings'} of ${name}`,
  credential_changed: (name, what) => (what ? `replaced the ${what} of ${name}` : `changed a secret of ${name}`),
  removed: (name) => `removed ${name}`,
  default_changed: (name) => `made ${name} the default`,
  held_replayed: (name, _what, count) => `opened ${messagesOf(count)} that arrived while ${name} was off`,
  held_discarded: (name, _what, count) => `discarded ${messagesOf(count)} that arrived while ${name} was off`,
};
