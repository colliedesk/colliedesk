// An unsent reply is kept in this browser per workspace, agent and conversation, so a reload, Back or a
// trip to Settings does not lose it. Only the text, whether it is a reply or a note, and when it was last
// written are kept; never files. A draft older than a week is dropped when read, and signing out forgets
// every draft, so a shared browser does not hand one agent's private notes to the next.
// This module is in the first bundle, which has a size budget: keep it small.
export const draftPrefix = 'colliedesk.reply-draft:';
const prefix = draftPrefix;
const maxAgeMs = 7 * 24 * 60 * 60 * 1000;

export interface StoredDraft { kind: 'outgoing' | 'note'; body: string }

export function readDraft(key: string | undefined): StoredDraft | undefined {
  try {
    // JSON.parse reads a missing item (null) as null.
    const value = key && JSON.parse(localStorage.getItem(prefix + key) as string) as Partial<StoredDraft & { saved_at: number }> | null;
    // A missing or non-numeric saved_at gives NaN, which is never fresh. A stale draft is not shown, and
    // the composer that reads it then writes its empty box, which removes it.
    if (value && Date.now() - (value.saved_at as number) <= maxAgeMs && typeof value.body === 'string') return { kind: value.kind === 'note' ? 'note' : 'outgoing', body: value.body };
  } catch {
    // Storage off (a private window) or a value written by something else: start empty.
  }
  return undefined;
}

// writeDraft keeps the draft, or forgets it when the box is empty (sent or cleared).
export function writeDraft(key: string | undefined, draft: StoredDraft): void {
  try {
    if (!key) return;
    if (draft.body.trim()) localStorage.setItem(prefix + key, JSON.stringify({ ...draft, saved_at: Date.now() }));
    else localStorage.removeItem(prefix + key);
  } catch {
    // A full or disabled storage only means the draft is not kept past this page.
  }
}

// forgetAllDrafts removes every draft this browser keeps, whoever wrote it; called on sign-out.
export function forgetAllDrafts(): void {
  try {
    for (const key of Object.keys(localStorage)) if (key.startsWith(prefix)) localStorage.removeItem(key);
  } catch {
    // Storage off: nothing was kept.
  }
}
