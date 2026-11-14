// What the app asks before it ends the session (sign out) while a Settings page holds text that is not saved. The Settings
// chunk registers the page's own answer; without a Settings page open there is nothing to ask.
export const leaveUnsavedQuestion = 'You have unsaved changes. Leave anyway?';

let probe: (() => boolean) | undefined;

export function registerUnsavedProbe(unsaved: () => boolean): () => void {
  probe = unsaved;
  return () => { if (probe === unsaved) probe = undefined; };
}

// confirmDiscardUnsavedSettings says whether to go on: at once when nothing is unsaved, otherwise once the person agrees.
export function confirmDiscardUnsavedSettings(): boolean {
  return !probe?.() || window.confirm(leaveUnsavedQuestion);
}
