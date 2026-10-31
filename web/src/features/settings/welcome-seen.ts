const prefix = 'colliedesk.welcome-seen.';

// The welcome after paying opens once for a workspace: closing it, however it is closed, is remembered in
// this browser, so a reload of the page the checkout returned to does not open it again.
export function hasSeenWelcome(workspaceId: string): boolean {
  try {
    return window.localStorage.getItem(prefix + workspaceId) !== null;
  } catch {
    // Storage is blocked: the welcome then opens once per page load, never in a loop.
    return false;
  }
}

export function markWelcomeSeen(workspaceId: string): void {
  try {
    window.localStorage.setItem(prefix + workspaceId, String(Date.now()));
  } catch {
    // Nothing is remembered.
  }
}
