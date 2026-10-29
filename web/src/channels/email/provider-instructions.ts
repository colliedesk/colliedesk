import type { Provider } from './setup-state';

// How each mail provider forwards a copy of every message, as numbered steps. Written from the providers'
// own help pages; a step is plain words, the names of buttons are quoted as the provider shows them.
export interface ProviderSteps {
  id: Provider;
  label: string;
  steps: string[];
  // A link that opens the provider's forwarding settings in a new tab.
  open?: { label: string; href: string };
  // What can stop forwarding at this provider, said before the step it affects.
  warning?: { before: number; text: string };
}

export const providerSteps: ProviderSteps[] = [
  {
    id: 'gmail', label: 'Gmail',
    open: { label: 'Open Gmail', href: 'https://mail.google.com/mail/u/0/#settings/fwdandpop' },
    steps: [
      'In Gmail, open Settings → See all settings → Forwarding and POP/IMAP.',
      'Select Add a forwarding address, paste the address above, then select Next → Proceed.',
      'Gmail sends a confirmation email to Colliedesk. Its code and confirm link show below within a few seconds.',
      'Back in Gmail, choose Forward a copy of incoming mail to the address above, then Save Changes.',
    ],
  },
  {
    id: 'workspace', label: 'Google Workspace',
    open: { label: 'Open Gmail', href: 'https://mail.google.com/mail/u/0/#settings/fwdandpop' },
    warning: { before: 0, text: 'Your Google Workspace admin may have turned off automatic forwarding. If Add a forwarding address is missing, ask them to allow it in the Admin console: Apps → Google Workspace → Gmail → End User Access → Automatic forwarding.' },
    steps: [
      'Sign in to Gmail as the support mailbox, then open Settings → See all settings → Forwarding and POP/IMAP.',
      'Select Add a forwarding address, paste the address above, then select Next → Proceed.',
      'Google sends a confirmation email to Colliedesk. Its code and confirm link show below within a few seconds.',
      'Back in Gmail, choose Forward a copy of incoming mail to the address above, then Save Changes.',
    ],
  },
  {
    id: 'microsoft', label: 'Microsoft 365',
    open: { label: 'Open Outlook', href: 'https://outlook.office.com/mail/options/mail/forwarding' },
    warning: { before: 3, text: 'Microsoft 365 often blocks forwarding outside your organization: the forwarded mail bounces with 5.7.520. Your admin allows it for this mailbox in the Microsoft Defender outbound spam policy (automatic forwarding: On).' },
    steps: [
      'Sign in to Outlook on the web as the support mailbox.',
      'Open Settings → Mail → Forwarding.',
      'Turn on Enable forwarding and paste the address above.',
      'Tick Keep a copy of forwarded messages, then select Save.',
    ],
  },
  {
    id: 'other', label: 'Other / own server',
    steps: [
      'Open the forwarding or routing settings of your mail provider or mail server.',
      'Forward every message sent to your support address to the address above, keeping a copy in the mailbox.',
      'If your provider asks to confirm the address, its confirmation email opens a conversation in your inbox here.',
      'Send a message to your support address from another address to check.',
    ],
  },
];
