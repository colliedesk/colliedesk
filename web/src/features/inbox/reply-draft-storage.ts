// An unsent reply is kept in this browser per workspace and conversation, so a reload, Back or a trip to
// Settings does not lose it. Only the text and whether it is a reply or a note are kept; never files.
const prefix = 'colliedesk.reply-draft:';

export interface StoredDraft { kind: 'outgoing' | 'note'; body: string }

export function readDraft(key: string | undefined): StoredDraft | undefined {
  if (!key) return undefined;
  try {
    const raw = window.localStorage.getItem(prefix + key);
    if (!raw) return undefined;
    const value = JSON.parse(raw) as Partial<StoredDraft>;
    if (typeof value.body !== 'string' || (value.kind !== 'outgoing' && value.kind !== 'note')) return undefined;
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
    if (draft.body.trim()) window.localStorage.setItem(prefix + key, JSON.stringify(draft));
    else window.localStorage.removeItem(prefix + key);
  } catch {
    // A full or disabled storage only means the draft is not kept past this page.
  }
}
