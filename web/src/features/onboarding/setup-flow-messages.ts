import type { SetupStepKey } from '../../api/schemas';

// Words of the setup flow (/setup). They load with its chunk, never in the first bundle.
export const flowMessages = {
  exit: 'Exit setup',
  stepOf: (n: number, total: number) => `Step ${n} of ${total}`,
  progressLabel: 'Setup progress',
  continue: 'Continue',
  later: 'I\'ll do this later',
  back: 'Back',
  flowNav: 'Setup flow',
  loadFailed: 'The setup could not be loaded.',
  lead: {
    email: 'Customers keep writing to your address. Each email becomes a conversation here.',
    import: 'Bring your tickets, contacts and saved replies over. Your Freshdesk account is not changed.',
    invite: 'Send a teammate a one-time sign-in link. They join with the role you choose.',
    first_reply: 'Answer a customer from the inbox. The reply goes out by email.',
  } satisfies Record<SetupStepKey, string>,
  // What is missing before Continue, by the state the server reports; a state without words has none.
  missing: {
    email: { none: 'Save your support address above to continue.', needs_attention: 'Your email needs attention. Finish the steps above to continue.' },
    import: { none: 'Read your Freshdesk account above to continue.' },
    invite: { alone: 'Create an invite above to continue.' },
    first_reply: { none: 'Send your first reply to continue.' },
  } satisfies Record<SetupStepKey, Record<string, string>>,
  noMailbox: 'Replies start from the inbox once email or an import brings conversations.',
  setUpEmail: 'Set up email first',
  doneTitle: 'You\'re ready to answer customers',
  doneAll: 'Every step is done.',
  doneSome: 'Finish the rest when you are ready. The setup guide in the inbox keeps track.',
  stepsLabel: 'Setup steps',
  stateDone: 'Done',
  stateTodo: 'To do',
  stateAttention: 'Needs attention',
  openInbox: 'Open the inbox',
};
