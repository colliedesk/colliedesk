// A sign-in a channel's setup sends the browser to (a mailbox provider) returns to the channel page, where
// the server's redirect points. markSetupReturn says, for this tab, which channel's setup to go back to and,
// for a setup shown elsewhere (the setup flow), the page it is on; takeSetupReturn reads it once.
const returnKey = 'colliedesk.email-setup.return';

export function markSetupReturn(channelID: string, to?: string) {
  try { sessionStorage.setItem(returnKey, JSON.stringify({ channel: channelID, to })); } catch { /* no storage: the channel page shows the outcome */ }
}

// takeSetupReturn answers whether the setup of channelID waits for this return, with its page when it has one.
export function takeSetupReturn(channelID: string): { to?: string } | undefined {
  try {
    const raw = sessionStorage.getItem(returnKey);
    if (raw === null) return undefined;
    const mark = parseMark(raw);
    if (mark.channel !== channelID) return undefined;
    sessionStorage.removeItem(returnKey);
    return { to: mark.to?.startsWith('/') ? mark.to : undefined };
  } catch { return undefined; }
}

// A tab marked before the page was named holds the channel ID alone.
function parseMark(raw: string): { channel?: string; to?: string } {
  try {
    const value: unknown = JSON.parse(raw);
    return typeof value === 'object' && value !== null ? value as { channel?: string; to?: string } : { channel: raw };
  } catch { return { channel: raw }; }
}
