// A sign-in a channel's setup sends the browser to (a mailbox provider) returns to the channel page, where
// the server's redirect points. markSetupReturn says, for this tab, which channel's setup to go back to;
// takeSetupReturn reads it once.
const returnKey = 'colliedesk.email-setup.return';

export function markSetupReturn(channelID: string) {
  try { sessionStorage.setItem(returnKey, channelID); } catch { /* no storage: the channel page shows the outcome */ }
}

export function takeSetupReturn(channelID: string): boolean {
  try {
    if (sessionStorage.getItem(returnKey) !== channelID) return false;
    sessionStorage.removeItem(returnKey);
    return true;
  } catch { return false; }
}
