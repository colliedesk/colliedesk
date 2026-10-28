// The outcome a mailbox provider's redirect left in the address: `?gmail=connected`, `?gmail_error=<reason>`,
// `?microsoft=connected|admin_consent_done` or `?microsoft_error=<reason>`. It is read on the Settings page
// itself (the "Send from your mailbox" block loads in its own chunk and explains the reason).
export type MailboxProvider = 'google' | 'microsoft';
export interface MailboxReturn { kind: 'connected' | 'admin_consent_done' | 'error'; reason?: string; channel?: string; provider?: MailboxProvider }
export type GmailReturn = MailboxReturn;

// The query parameter each provider's outcome comes in.
const keys: Record<MailboxProvider, string> = { google: 'gmail', microsoft: 'microsoft' };

// mailboxReturn reads the outcome and cleans the address, so a reload does not repeat it. A Gmail outcome
// leaves out the provider, as it always did.
export function mailboxReturn(): MailboxReturn | undefined {
  const params = new URLSearchParams(window.location.search);
  for (const provider of ['google', 'microsoft'] as const) {
    const key = keys[provider];
    const done = params.get(key);
    const reason = params.get(`${key}_error`);
    const kind = done === 'connected' ? 'connected' : provider === 'microsoft' && done === 'admin_consent_done' ? 'admin_consent_done' : undefined;
    if (!kind && reason === null) continue;
    params.delete(key);
    params.delete(`${key}_error`);
    // account_in_use names the channel that sends through the account.
    const channel = params.get(`${key}_channel`) ?? undefined;
    params.delete(`${key}_channel`);
    const rest = params.toString();
    window.history.replaceState(null, '', `${window.location.pathname}${rest ? `?${rest}` : ''}${window.location.hash}`);
    const named = provider === 'microsoft' ? { provider } : {};
    return kind ? { kind, ...named } : { kind: 'error', reason: reason!, channel, ...named };
  }
  return undefined;
}

export const gmailReturn = mailboxReturn;
