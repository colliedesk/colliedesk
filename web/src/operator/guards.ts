import type { Setting } from './api';

// What saving a change asks for, worked out from the settings it touches, so the save bar says it before the
// person presses Save and the review dialog asks for exactly that. The server checks the same.
export interface Draft {
  set: Record<string, string>;
  unset: string[];
  secrets: Record<string, string>;
}

export interface Needs {
  reauth: boolean;
  reenter: string[];
  confirm: Record<string, string>;
  preview: string[];
  changed: string[];
}

export function emptyDraft(): Draft {
  return { set: {}, unset: [], secrets: {} };
}

// The unsaved changes outlive the page (a new sign-in link loads it again) in this tab's session storage.
// Secrets typed into the draft are never written there: they are asked for again.
const draftKey = 'colliedesk.operator.draft';

export function storedDraft(): Draft {
  try {
    const stored = JSON.parse(window.sessionStorage.getItem(draftKey) ?? 'null') as Partial<Draft> | null;
    if (!stored || typeof stored.set !== 'object' || !Array.isArray(stored.unset)) return emptyDraft();
    const set = Object.fromEntries(Object.entries(stored.set ?? {}).filter((entry): entry is [string, string] => typeof entry[1] === 'string'));
    return { set, unset: stored.unset.filter((key): key is string => typeof key === 'string'), secrets: {} };
  } catch {
    return emptyDraft();
  }
}

export function storeDraft(draft: Draft) {
  try {
    if (Object.keys(draft.set).length === 0 && draft.unset.length === 0) window.sessionStorage.removeItem(draftKey);
    else window.sessionStorage.setItem(draftKey, JSON.stringify({ set: draft.set, unset: draft.unset }));
  } catch {
    // Without storage the draft lives only as long as the page.
  }
}

export function changedKeys(draft: Draft): string[] {
  return [...new Set([...Object.keys(draft.set), ...draft.unset, ...Object.keys(draft.secrets)])].sort();
}

export function needs(draft: Draft, settings: Setting[]): Needs {
  const byKey = new Map(settings.map((setting) => [setting.key, setting]));
  const changed = changedKeys(draft);
  const result: Needs = { reauth: false, reenter: [], confirm: {}, preview: [], changed };
  for (const key of changed) {
    const setting = byKey.get(key);
    if (!setting) continue;
    if (setting.secret && key in draft.secrets) result.reauth = true;
    if (setting.guards.includes('reauth')) result.reauth = true;
    if (setting.guards.includes('preview')) result.preview.push(key);
    if (setting.guards.includes('reenter_secret') && setting.reenter) {
      const secret = byKey.get(setting.reenter);
      if (secret?.set && !(setting.reenter in draft.secrets) && !result.reenter.includes(setting.reenter)) result.reenter.push(setting.reenter);
    }
    if (setting.guards.includes('type_to_confirm')) result.confirm[key] = draft.set[key] || key;
  }
  return result;
}

// confirmMatches compares a typed phrase as the server does: trimmed, case-insensitive.
export function confirmMatches(typed: string, phrase: string): boolean {
  return typed.trim().toLowerCase() === phrase.trim().toLowerCase();
}

export function validCode(code: string): boolean {
  return /^\d{6}$/.test(code.replace(/\s/g, ''));
}

// missing lists what the review still lacks, for the line beside the disabled button.
export function missing(need: Needs, code: string, typed: Record<string, string>, reentered: Record<string, string>, totp: boolean): string[] {
  const out: string[] = [];
  for (const secret of need.reenter) if (!reentered[secret]?.trim()) out.push(`enter ${secret} again`);
  for (const [key, phrase] of Object.entries(need.confirm)) if (!confirmMatches(typed[key] ?? '', phrase)) out.push(`type ${phrase}`);
  if (need.reauth && totp && !validCode(code)) out.push('your 6-digit code');
  return out;
}
