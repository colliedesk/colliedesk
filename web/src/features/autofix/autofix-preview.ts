// Autofix is in preview: its screens show only in a browser that turned it on, with
// localStorage.setItem('colliedesk.autofix', 'on') in the console and a reload.
const key = 'colliedesk.autofix';

export function autofixPreviewOn(): boolean {
  try { return localStorage.getItem(key) === 'on'; } catch { return false; }
}

// GitHub sends a person back to Settings → Autofix after they install the App (`?github=…` or
// `?github_error=…`). Often they started from an AI agent's link, in a browser that never turned
// the preview on: coming back turns it on, so the page that tells them the outcome is there.
export function turnAutofixPreviewOnAfterGitHub(search: Record<string, unknown>): void {
  if (!('github' in search) && !('github_error' in search)) return;
  try { localStorage.setItem(key, 'on'); } catch { /* A browser without storage shows the page for this visit only. */ }
}
