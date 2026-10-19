// The outcome Google's redirect left in the address: `?gmail=connected` or `?gmail_error=<reason>`. It is read
// on the Settings page itself (the "Send from Gmail" block loads in its own chunk and explains the reason).
export interface GmailReturn { kind: 'connected' | 'error'; reason?: string }

// gmailReturn reads the outcome and cleans the address, so a reload does not repeat it.
export function gmailReturn(): GmailReturn | undefined {
  const params = new URLSearchParams(window.location.search);
  const connected = params.get('gmail') === 'connected';
  const reason = params.get('gmail_error');
  if (!connected && reason === null) return undefined;
  params.delete('gmail');
  params.delete('gmail_error');
  const rest = params.toString();
  window.history.replaceState(null, '', `${window.location.pathname}${rest ? `?${rest}` : ''}${window.location.hash}`);
  return connected ? { kind: 'connected' } : { kind: 'error', reason: reason! };
}
