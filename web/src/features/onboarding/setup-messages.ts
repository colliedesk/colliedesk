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
  viewHelpCenter: 'View help center',
  turnOnHelpCenter: 'Turn on the help center',
  searchSubmitted: 'Verification kept and sitemap submitted',
  skipDomain: 'No custom domain needed',
  restoreDomain: 'Use a custom domain',
  updateFailed: 'The move step could not be saved. Try again.',
  prerequisites: { import: 'Import the knowledge base first', enable: 'Turn on the help center first', verification_present: 'Verification tag already in place; check it on your domain and submit the sitemap' } as Record<string, string>,
  title: {
    email: 'Receive customer email',
    import: 'Import from Freshdesk',
    invite: 'Invite your team',
    first_reply: 'Answer your first email',
    custom_domain: 'Verify your domain by TXT', plan: 'Choose a plan',
    cname: 'Change the CNAME', search_console: 'Keep Search Console verification and submit the sitemap',
  } satisfies Record<SetupStepKey, string>,
  // Minutes it takes, shown while a step is still to do.
  time: { email: '5 min', import: '10 min', invite: '1 min', first_reply: '2 min', custom_domain: '5 min', plan: '2 min', cname: '5 min', search_console: '3 min' } satisfies Record<SetupStepKey, string>,
  // The words under a step's title, by the state the server reports. Unknown states say nothing.
  state: {
    email: { none: 'Connect the address your customers write to', waiting: 'Waiting for the first email', working: 'Done', needs_attention: 'Needs attention' },
    import: { none: 'Tickets, contacts and the knowledge base with images; old article links keep working', running: 'Importing', done: 'Done' },
    invite: { alone: 'Just you so far', invited: 'Invite sent', joined: 'Done' },
    first_reply: { none: 'Not yet', sent: 'Done' },
    custom_domain: { none: 'Add TXT first; visitors still reach Freshdesk', done: 'Done', skipped: 'Skipped' },
    plan: { none: 'A paid plan serves the help center on your domain', done: 'Done', skipped: 'Skipped' },
    cname: { none: 'Use the target in the domain card, then Check now', done: 'Done', skipped: 'Skipped' },
    search_console: { none: 'Keep verification on your domain; submit its sitemap in Search Console', done: 'Done', skipped: 'Skipped' },
  } satisfies Record<SetupStepKey, Record<string, string>>,
  mcp: { title: 'Connect an AI agent', state: 'Let Claude or Codex work the queue', time: '3 min' },
};
