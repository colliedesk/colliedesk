// Autofix is in preview: its screens show only in a browser that turned it on, with
// localStorage.setItem('colliedesk.autofix', 'on') in the console and a reload.
export function autofixPreviewOn(): boolean {
  try { return localStorage.getItem('colliedesk.autofix') === 'on'; } catch { return false; }
}
