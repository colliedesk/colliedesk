// Text typed into an article and not yet on the server, kept in this browser until a save is
// confirmed. The key names the workspace, the signed-in person and the article, so a copy is only
// ever offered back to the person who typed it, in the workspace it belongs to.
export type UnsavedText = { title: string; body: string; at: string };

export function unsavedKey(workspaceId: string, agentId: string, articleId: string): string {
  return `colliedesk:kb-unsaved:${workspaceId}:${agentId}:${articleId}`;
}

export function readUnsaved(key: string): UnsavedText | undefined {
  try {
    const raw = localStorage.getItem(key);
    if (!raw) return undefined;
    const value: unknown = JSON.parse(raw);
    if (typeof value !== 'object' || value === null) return undefined;
    const { title, body, at } = value as Record<string, unknown>;
    if (typeof title !== 'string' || typeof body !== 'string' || typeof at !== 'string' || Number.isNaN(Date.parse(at))) return undefined;
    return { title, body, at };
  } catch {
    return undefined;
  }
}

export function keepUnsaved(key: string, text: { title: string; body: string }, at = new Date().toISOString()) {
  try {
    localStorage.setItem(key, JSON.stringify({ title: text.title, body: text.body, at }));
  } catch {
    // A full or blocked storage keeps nothing; the server copy and the open page still hold the text.
  }
}

// Removes the copy once the server has it. With `saved`, a copy typed after that save stays.
export function clearUnsaved(key: string, saved?: { title: string; body: string }) {
  try {
    if (saved) {
      const kept = readUnsaved(key);
      if (kept && (kept.title.trim() !== saved.title.trim() || kept.body !== saved.body)) return;
    }
    localStorage.removeItem(key);
  } catch {
    // Nothing to remove when storage is blocked.
  }
}
