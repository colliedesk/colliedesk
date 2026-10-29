import type { Channel } from '../../api/channels';
import type { EmailChannelSettings, Message } from '../../api/schemas';

// The four steps of connecting a support address, in order. Each step's state comes from what the server
// already says about the channel; only what the server cannot know (the provider tab a person picked, a
// step skipped, the conversation of their test message) is remembered in this browser, per channel.
export const steps = ['address', 'forward', 'replies', 'test'] as const;
export type StepID = typeof steps[number];
export type StepState = 'done' | 'skipped' | 'todo';
export type Provider = 'gmail' | 'workspace' | 'microsoft' | 'other';
export type RepliesChoice = 'gmail' | 'microsoft' | 'colliedesk';

export interface SetupMemory { provider?: Provider; skipped?: StepID[]; testConversation?: string }

const key = (channelID: string) => `colliedesk.email-setup.${channelID}`;

export function readMemory(channelID: string | undefined): SetupMemory {
  if (!channelID) return {};
  try {
    const value: unknown = JSON.parse(localStorage.getItem(key(channelID)) ?? '{}');
    return value && typeof value === 'object' ? value as SetupMemory : {};
  } catch { return {}; }
}

export function writeMemory(channelID: string, memory: SetupMemory) {
  try { localStorage.setItem(key(channelID), JSON.stringify(memory)); } catch { /* storage off: the setup still works, it forgets on reload */ }
}

// clockTime is the time of day an event happened, in the agent's timezone.
export const clockTime = (value: string, timeZone: string) =>
  new Intl.DateTimeFormat('en-US', { hour: 'numeric', minute: '2-digit', timeZone }).format(new Date(value));

const same = (a: string | null | undefined, b: string | null | undefined) => Boolean(a && b && a.toLowerCase() === b.toLowerCase());

// addressSaved: the channel has a support address of the team's own. A new cloud workspace's channel starts
// at its forwarding address, which is not one customers write to.
export const addressSaved = (settings: EmailChannelSettings | undefined) =>
  Boolean(settings?.channel?.support_address) && !same(settings?.channel?.support_address, settings?.hosted_address);

// firstEmail: something arrived on the channel, so forwarding works.
export const firstEmail = (channel: Pick<Channel, 'status' | 'last_inbound_at'> | undefined) =>
  Boolean(channel && (channel.status.state === 'working' || channel.last_inbound_at));

// repliesThrough names how this channel's replies leave now.
export function repliesThrough(settings: EmailChannelSettings | undefined): 'gmail' | 'microsoft' | 'domain' | 'server' | 'none' {
  const connected = settings?.gmail.connection ?? settings?.microsoft.connection;
  if (connected?.status === 'connected') return connected.provider === 'microsoft' ? 'microsoft' : 'gmail';
  if (!settings?.channel) return 'none';
  if (settings.channel.domain_verified_at || same(settings.sending_from, settings.channel.support_address)) return 'domain';
  return settings.sending_from ? 'server' : 'none';
}

// testReplied: an outgoing reply on the test conversation was delivered.
export const testReplied = (timeline: Pick<Message, 'kind' | 'delivery'>[] | undefined) =>
  Boolean(timeline?.some((message) => message.kind === 'outgoing' && message.delivery?.status === 'sent'));

export interface SetupFacts {
  channel?: Pick<Channel, 'status' | 'last_inbound_at'>;
  settings?: EmailChannelSettings;
  memory: SetupMemory;
  replied: boolean;
}

export function stepStates({ channel, settings, memory, replied }: SetupFacts): Record<StepID, StepState> {
  const through = repliesThrough(settings);
  const done: Record<StepID, boolean> = {
    address: addressSaved(settings),
    forward: firstEmail(channel),
    replies: through === 'gmail' || through === 'microsoft' || through === 'domain',
    test: Boolean(memory.testConversation) && replied,
  };
  return Object.fromEntries(steps.map((step) => [step, done[step] ? 'done' : memory.skipped?.includes(step) ? 'skipped' : 'todo'])) as Record<StepID, StepState>;
}

// nextStep is the first step after `after` (or from the start) still to do; undefined when none is.
export function nextStep(states: Record<StepID, StepState>, after?: StepID): StepID | undefined {
  const from = after ? steps.indexOf(after) + 1 : 0;
  return steps.slice(from).find((step) => states[step] === 'todo');
}

// recommended is the reply choice that fits the provider picked in step 2; there is no DNS lookup.
export function recommended(provider: Provider | undefined): RepliesChoice {
  if (provider === 'microsoft') return 'microsoft';
  if (provider === 'other') return 'colliedesk';
  return 'gmail';
}
