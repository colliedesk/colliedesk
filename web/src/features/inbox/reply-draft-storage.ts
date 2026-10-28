// An unsent reply is kept in this browser per workspace, agent and conversation, so a reload, Back or a
// trip to Settings does not lose it. Only the text, whether it is a reply or a note, and when it was last
// written are kept; never files. A draft older than a week is dropped when read, and signing out forgets
// every draft, so a shared browser does not hand one agent's private notes to the next.
const prefix = 'colliedesk.reply-draft:';
const maxAgeMs = 7 * 24 * 60 * 60 * 1000;

export interface StoredDraft { kind: 'outgoing' | 'note'; body: string }

interface Saved extends StoredDraft { saved_at: number }

export function readDraft(key: string | undefined): StoredDraft | undefined {
  if (!key) return undefined;
  try {
    const raw = window.localStorage.getItem(prefix + key);
    if (!raw) return undefined;
    const value = JSON.parse(raw) as Partial<Saved>;
    const fresh = typeof value.saved_at === 'number' && Date.now() - value.saved_at <= maxAgeMs;
    if (!fresh || typeof value.body !== 'string' || (value.kind !== 'outgoing' && value.kind !== 'note')) {
      window.localStorage.removeItem(prefix + key);
      return undefined;
    }
    return { kind: value.kind, body: value.body };
  } catch {
    // Storage off (a private window) or a value written by something else: start empty.
    return undefined;
  }
}

// writeDraft keeps the draft, or forgets it when the box is empty (sent or cleared).
export function writeDraft(key: string | undefined, draft: StoredDraft): void {
  if (!key) return;
  try {
    if (draft.body.trim()) {
      const saved: Saved = { kind: draft.kind, body: draft.body, saved_at: Date.now() };
      window.localStorage.setItem(prefix + key, JSON.stringify(saved));
    } else window.localStorage.removeItem(prefix + key);
  } catch {
    // A full or disabled storage only means the draft is not kept past this page.
  }
}

// forgetAllDrafts removes every draft this browser keeps, whoever wrote it; called on sign-out.
export function forgetAllDrafts(): void {
  try {
    const storage = window.localStorage;
    const keys: string[] = [];
    for (let i = 0; i < storage.length; i++) {
      const key = storage.key(i);
      if (key?.startsWith(prefix)) keys.push(key);
    }
    for (const key of keys) storage.removeItem(key);
  } catch {
    // Storage off: nothing was kept.
  }
}
