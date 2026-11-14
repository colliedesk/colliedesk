// The console's language: the operator's choice (colliedesk.operator.lang), else the browser's first English or
// Vietnamese preference, else English. English strings stay the source (messages.ts, fields.ts and the lazy
// parts' message files); another language is a lazy part that rewrites them in place before the first render,
// so the console's first bundle carries none of it and an English console waits for nothing.
import type { am } from './ai-provider-messages';
import type { cloudMessages } from './cloud-messages';

export type Lang = 'en' | 'vi';
const key = 'colliedesk.operator.lang';

export let lang: Lang = 'en';

// The words of the lazy parts in the chosen language, kept here until their part loads and reads them.
// coded rewrites an API answer's coded texts in the chosen language (codes.ts); English has none.
export const lazyWords: { am?: Translation<typeof am>; cloud?: Translation<typeof cloudMessages>; coded?: (body: Record<string, unknown>) => void } = {};

export function pickLang(): Lang {
  try {
    const stored = localStorage.getItem(key);
    if (stored === 'en' || stored === 'vi') return stored;
  } catch { /* private window: follow the browser */ }
  return /^vi/i.test(navigator.languages.find((tag) => /^(en|vi)/i.test(tag)) ?? '') ? 'vi' : 'en';
}

// locale formats dates in the chosen language; English keeps the browser's own format.
export const locale = () => (lang === 'vi' ? 'vi-VN' : undefined);

export function loadLang(): Promise<void> {
  lang = pickLang();
  if (lang === 'en') return Promise.resolve();
  return import('./vi').then((vi) => vi.apply(), () => { lang = 'en'; });
}

// switchLang keeps the choice and loads the console again in it (an unsaved change stays in this tab).
export function switchLang() {
  try {
    localStorage.setItem(key, lang === 'vi' ? 'en' : 'vi');
  } catch { /* private window: nothing to keep */ }
  window.location.reload();
}

// Translation is a message file's shape with any wording: the same keys, the same arguments.
export type Translation<T> = T extends string ? string
  : T extends (...args: infer A) => infer R ? (...args: A) => Translation<R>
    : { -readonly [K in keyof T]: Translation<T[K]> };
