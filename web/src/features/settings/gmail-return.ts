// The outcome a mailbox provider's redirect left in the address: `?gmail=connected`, `?gmail_error=<reason>`,
// `?microsoft=connected|admin_consent_done` or `?microsoft_error=<reason>`. It is read on the Settings page
// itself (the "Send from your mailbox" block loads in its own chunk and explains the reason).
export type MailboxProvider = 'google' | 'microsoft';
export interface MailboxReturn { kind: 'connected' | 'admin_consent_done' | 'error'; reason?: string; channel?: string; provider?: MailboxProvider }
export type GmailReturn = MailboxReturn;

// The query parameter each provider's outcome comes in.
const keys: Record<MailboxProvider, string> = { google: 'gmail', microsoft: 'microsoft' };

// The router's key of the history entry being viewed (@tanstack/history keeps it in the entry's state).
const entryKey = (): unknown => (window.history.state as { __TSR_key?: unknown } | null)?.__TSR_key;

// The outcome already read on this visit: the address no longer carries it, and every part of the page (and a
// part mounted anew) still gets it. It belongs to that history entry and to this load of the app, so another
// page, a later visit and a reload do not show it again.
let read: { entry: unknown; outcome: MailboxReturn } | undefined;

// mailboxReturn reads the outcome and cleans the address, so a reload does not repeat it. The router's state of
// the entry is kept. A Gmail outcome leaves out the provider, as it always did.
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
    window.history.replaceState(window.history.state, '', `${window.location.pathname}${rest ? `?${rest}` : ''}${window.location.hash}`);
    const named = provider === 'microsoft' ? { provider } : {};
    const outcome: MailboxReturn = kind ? { kind, ...named } : { kind: 'error', reason: reason!, channel, ...named };
    // Without the router's key (a page outside the router) the outcome is read once, as before.
    const entry = entryKey();
    read = entry === undefined ? undefined : { entry, outcome };
    return outcome;
  }
  return read && read.entry === entryKey() ? read.outcome : undefined;
}

export const gmailReturn = mailboxReturn;
