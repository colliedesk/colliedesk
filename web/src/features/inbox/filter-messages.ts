import { channelUI } from '../../channels/registry';

// Wording of the Filter menu, the filter chips and the saved views menu. It loads with those lazy parts
// of the inbox, not with the first screen; the words the first screen shows stay in src/messages.ts.
export const filterMessages = {
  // How a conversation started without a channel; a channel type's label is its registry entry's (channelLabel).
  origins: { imported: 'Imported', agent: 'Started by the team', problem_report: 'Problem report', article_feedback: 'Article feedback' } as Record<string, string>,
  priority: 'Priority',
  assignee: 'Assignee',
  anyoneAssigned: 'Assigned to anyone',
  tags: 'Tags',
  // A tag chip names one tag.
  tag: 'Tag',
  matchAllTags: 'Has every chosen tag',
  noTags: 'No tags yet.',
  awaitingOptions: { team: 'Waiting on us', customer: 'Waiting on the customer' },
  channel: 'Channel',
  // A type with two or more channels lists them, so one mailbox or form can be chosen.
  channelsOf: (type: string) => `${type} channels`,
  removedChannel: (name: string) => `${name} (removed)`,
  source: 'Source',
  sources: { imported: 'Imported', native: 'Created in Colliedesk' },
  attention: 'Needs attention',
  hasPendingDraft: 'Reply draft waiting to be sent',
  noPendingDraft: 'No reply draft waiting',
  deliveryFailed: 'Reply delivery failed',
  noDeliveryFailed: 'No failed delivery',
  organization: 'Organization',
  targets: 'Targets',
  targetStates: { overdue: 'Overdue', due_soon: 'Due soon' } as Record<'overdue' | 'due_soon', string>,
  reference: 'Reference',
  contact: 'Customer',
  created: 'Created',
  updated: 'Last activity',
  dateRange: (from?: string, to?: string) => (from && to ? `${from} to ${to}` : from ? `from ${from}` : `until ${to}`),
  active: 'Active filters',
  remove: (label: string) => `Remove filter ${label}`,
  clearAll: 'Clear',
  countedIn: (metric: string, window: string) => `Counted in: ${metric}, ${window}`,
  countedInSide: (metric: string, side: string, range: string) => `Counted in: ${metric}, ${side}, ${range}`,
  anyStatus: 'The conversations behind this number, in any status.',
  resultWindows: { '30d': 'last 30 days', '7d': 'last 7 days' } as Record<string, string>,
  resultMetrics: {
    first_response: 'First response', first_response_waiting: 'Waiting for a first reply', resolution: 'Time to resolve', unresolved: 'Not resolved or still settling',
    without_resolve_time: 'Without a resolve time', reopened: 'Reopened within 7 days', volume: 'Conversations per week', handle: 'Handle time',
    ai_sent_unchanged: 'AI drafts sent nearly unchanged', ai_sent_edited: 'AI drafts sent after edits', ai_discarded: 'AI drafts discarded',
  } as Record<string, string>,
  backToResults: 'Back to Reports',
  unknownAgent: 'Former teammate',
  unknownTag: 'Deleted tag',
  selectedContact: 'Selected customer',
} as const;

// channelLabel names how a conversation started: the channel type's label, or the origin's.
export const channelLabel = (value: string): string => channelUI(value)?.label ?? filterMessages.origins[value] ?? value;
