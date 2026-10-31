// Strings of the agent requests page; they load with that page, not with the first screen.
export type NoticeWhen = 'now' | 'done' | 'never';

export const requestsMessages = {
  requestsDescription: 'What AI agents asked to do and could not do alone: an action above their token\'s tier, or one that always needs a person (new tokens, export, billing, the email channel, the workspace address). Approving runs it as you, with your permissions. A request expires after seven days.',
  requestsPending: 'Waiting',
  requestsAll: 'All',
  requestsFilter: 'Show requests',
  noPendingRequests: 'Nothing is waiting. When an agent asks for something it may not do alone, it shows up here.',
  noRequests: 'No agent has asked for anything yet.',
  requestsFailed: 'Requests could not be loaded.',
  requestedBy: (person: string, token: string, scope: string) => `Asked by ${person} via ${token} (${scope})`,
  tokenHeld: (count: number) => `What the token may do (${count} ${count === 1 ? 'permission' : 'permissions'})`,
  requestExpires: (date: string) => `Expires ${date}`,
  requestCall: 'Runs when approved',
  approve: 'Approve',
  reject: 'Reject',
  rejectReason: 'Reason (optional)',
  requestActionFailed: 'That did not work. Reload and try again.',
  tokenRevoked: 'The token that asked was revoked.',
  requestStatus: { pending: 'Waiting', approved: 'Approved', failed: 'Failed', rejected: 'Rejected', cancelled: 'Cancelled', expired: 'Expired' },
  requestOutcome: (verb: string, person: string, status: number | null) => `${verb} by ${person}${status === null ? '' : `; the call answered ${status}`}`,
  requestDecisionVerb: { approved: 'Approved', failed: 'Approved', rejected: 'Rejected', cancelled: 'Cancelled', expired: 'Expired', pending: '' },
  requestResultOnce: 'The answer, shown only now. Copy what you need: it is not kept.',
  openResult: (url: string) => `Open ${url}`,
  editedBy: (person: string) => `Edited by ${person} before sending`,
  // What approving a status change would tell customers, one block per chat channel.
  // when: the request still waits (now), was approved (done), or never ran (never).
  // Once approved it is what the change was to send as it ran: the server keeps no record of each message.
  noticeSends: (channel: string, when: NoticeWhen) => `${channel} ${when === 'now' ? 'will send' : when === 'done' ? 'was to send' : 'would have sent'}`,
  noticeEach: (channel: string, told: number, ref: string, when: NoticeWhen) => {
    const verb = when === 'now' ? 'tells' : when === 'done' ? 'was to tell' : 'would have told';
    const after = when === 'done' ? ' a few seconds after approval (not one whose status changed again before then)' : '';
    return told === 1 ? `${channel} ${verb} 1 customer${after}:` : `${channel} ${verb} ${told} customers${after}, each with their own code. For example, ${ref}:`;
  },
  noticeNoneSent: 'No customer was told: the status was already set, or the change did not go through.',
  noticeMoreChannels: (count: number) => `And ${count} more ${count === 1 ? 'channel' : 'channels'}.`,
  onlyAdminApproves: 'Only an admin can approve this.',
  onlyOwnerApproves: 'Only the person who owns this token can approve it: it changes their own settings.',
} as const;
