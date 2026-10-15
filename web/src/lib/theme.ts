// Light is the default for everyone; dark is an opt-in kept per browser. The OS preference is
// deliberately ignored so every new agent sees the same product first.
export type Theme = 'light' | 'dark';
export const themeStorageKey = 'colliedesk.theme';

type ThemeStorage = Pick<Storage, 'getItem' | 'setItem'>;

function defaultStorage(): ThemeStorage | undefined {
  try { return window.localStorage; } catch { return undefined; }
}

export function storedTheme(storage: Pick<Storage, 'getItem'> | undefined = defaultStorage()): Theme {
  try { return storage?.getItem(themeStorageKey) === 'dark' ? 'dark' : 'light'; } catch { return 'light'; }
}

export function applyTheme(theme: Theme, root: HTMLElement = document.documentElement) {
  root.dataset.theme = theme;
}

export function applyStoredTheme() {
  applyTheme(storedTheme());
}

// Blocked storage (private windows, strict policies) must not stop the switch on this page.
export function saveTheme(theme: Theme, storage: ThemeStorage | undefined = defaultStorage()) {
  applyTheme(theme);
  try { storage?.setItem(themeStorageKey, theme); } catch { /* remembered for this page only */ }
}
