import type { SetupStepKey } from '../../api/schemas';

export const setupMessages = {
  heading: 'Finish setting up',
  progress: (done: number, total: number) => `${done} of ${total}`,
  progressLabel: 'Setup progress',
  hide: 'Hide',
  minimize: 'Minimize',
  hideHint: 'Hide the setup guide. You can show it again in Preferences.',
  recommended: 'Recommended',
  done: 'Done',
  title: {
    email: 'Receive customer email',
    import: 'Import from Freshdesk',
    ai_drafts: 'Turn on AI drafts',
    reply_targets: 'Set reply targets',
    invite: 'Invite your team',
    first_reply: 'Answer your first email',
  } satisfies Record<SetupStepKey, string>,
  // Minutes it takes, shown while a step is still to do.
  time: { email: '5 min', import: '10 min', ai_drafts: '1 min', reply_targets: '1 min', invite: '1 min', first_reply: '2 min' } satisfies Record<SetupStepKey, string>,
  // The words under a step's title, by the state the server reports. Unknown states say nothing.
  state: {
    email: { none: 'Connect the address your customers write to', waiting: 'Waiting for the first email', working: 'Done', needs_attention: 'Needs attention' },
    import: { none: 'Moves your history over', running: 'Importing', done: 'Done' },
    ai_drafts: { none: 'A draft waits in each new conversation', nothing_to_read: 'Better after an import or an article', unavailable: 'Nothing to do: no built-in AI on this server', on: 'Done' },
    reply_targets: { none: 'Today puts the closest deadline first', on: 'Done' },
    invite: { alone: 'Just you so far', invited: 'Invite sent', joined: 'Done' },
    first_reply: { none: 'Not yet', sent: 'Done' },
  } satisfies Record<SetupStepKey, Record<string, string>>,
  mcp: { title: 'Connect an AI agent', state: 'Let Claude or Codex work the queue', time: '3 min' },
};
