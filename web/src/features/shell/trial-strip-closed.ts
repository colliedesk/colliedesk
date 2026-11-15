// The trial strip has two stages: the plain end date, then the last days when the workspace is about
// to lock. Closing it is remembered per workspace and per stage in this browser, so a strip closed
// early comes back once, when the lock gets close.
export type TrialStripStage = 'early' | 'ending';

const prefix = 'colliedesk.trial-strip-closed.';

export function isTrialStripClosed(workspaceId: string, stage: TrialStripStage): boolean {
  try {
    return window.localStorage.getItem(prefix + workspaceId) === stage;
  } catch {
    // Storage is blocked: the strip shows, and closing it hides it until the page reloads.
    return false;
  }
}

export function closeTrialStrip(workspaceId: string, stage: TrialStripStage): void {
  try {
    window.localStorage.setItem(prefix + workspaceId, stage);
  } catch {
    // Nothing is remembered.
  }
}
