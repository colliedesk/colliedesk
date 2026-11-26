import type { SetupStepKey } from '../../api/schemas';

// Words of the setup flow (/setup). They load with its chunk, never in the first bundle.
export const flowMessages = {
  exit: 'Exit setup',
  stepOf: (n: number, total: number) => `Step ${n} of ${total}`,
  progressLabel: 'Setup progress',
  // The main button names the step it opens; the last one ends at the summary.
  next: (title: string) => `Next: ${title}`,
  finish: 'Finish setup',
  later: 'I\'ll do this later',
  back: 'Back',
  flowNav: 'Setup flow',
  loadFailed: 'The setup could not be loaded.',
  // The step's short name beside the progress: "Step 3 of 6 · AI drafts".
  short: { email: 'Email', import: 'Import', ai_drafts: 'AI drafts', reply_targets: 'Reply targets', agent: 'AI agent', invite: 'Team', first_reply: 'First reply' } satisfies Record<SetupStepKey, string>,
  // A step whose screen title says more than its name in the guide.
  heading: { ai_drafts: 'Let AI draft your replies', reply_targets: 'Choose when a reply is due' } as Partial<Record<SetupStepKey, string>>,
  lead: {
    email: 'Customers keep writing to your address. Each email becomes a conversation here.',
    import: 'Bring your tickets, contacts and saved replies over. Your Freshdesk account is not changed.',
    ai_drafts: 'A draft waits in each new conversation. You read it, change it if needed, and send it. AI never sends on its own.',
    reply_targets: 'Today puts the conversation closest to its deadline first, so your team answers in the right order. Time counts only in business hours.',
    agent: 'Your AI app reads the inbox, drafts replies and tags conversations as you. It signs in with your Colliedesk account: no key to copy.',
    invite: 'Send a teammate a one-time sign-in link. They join with the role you choose.',
    first_reply: 'Answer a customer from the inbox. The reply goes out by email.',
  } satisfies Record<SetupStepKey, string>,
  // What is missing before Continue, by the state the server reports; a state without words has none. AI drafts
  // and reply targets have their own main button instead.
  missing: {
    email: { none: 'Save your support address above to continue.', needs_attention: 'Your email needs attention. Finish the steps above to continue.' },
    import: { none: 'Read your Freshdesk account above to continue.' },
    ai_drafts: {},
    reply_targets: {},
    agent: { none: 'Connect an AI app above to continue.' },
    invite: { alone: 'Create an invite above to continue.' },
    first_reply: { none: 'Send your first reply to continue.' },
  } satisfies Record<SetupStepKey, Record<string, string>>,
  noMailbox: 'Conversations arrive by email or import.',
  setUpEmail: 'Set up email first',
  doneTitle: 'You\'re ready to answer customers',
  doneAll: 'Every step is done.',
  doneSome: 'Finish the rest any time; the setup guide keeps track.',
  stepsLabel: 'Setup steps',
  stateDone: 'Done',
  stateTodo: 'To do',
  stateAttention: 'Needs attention',
  openInbox: 'Open the inbox',
};

// The AI agent step: Claude's directory on Colliedesk Cloud, the address any app signs in at, then a token.
export const agentStepMessages = {
  claudeTitle: 'Claude',
  claudeText: 'Colliedesk is in Claude\'s connectors directory. Choose Connect there, then Allow on the Colliedesk page that opens.',
  addToClaude: 'Add to Claude',
  newTab: '(opens in a new tab)',
  anyAppTitle: 'Claude, ChatGPT, Cursor or another app',
  anyAppText: 'Add a custom connector (a remote MCP server) with this address. Leave any token field empty: the app opens a Colliedesk page where you sign in and choose Allow.',
  address: 'MCP address',
  copy: 'Copy',
  copyAddress: 'Copy MCP address',
  tokenRoute: 'Use an agent token instead',
  tokenText: 'For an app that cannot sign in with OAuth.',
  guide: 'Read the AI agent guide',
  connected: 'An AI agent is connected.',
  manageBefore: 'See or disconnect it in ',
  apps: 'Connected AI apps',
  manageBetween: ' or ',
  tokens: 'agent tokens',
  manageAfter: '.',
};
