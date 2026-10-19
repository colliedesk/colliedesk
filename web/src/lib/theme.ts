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

// The page background of each theme (`--surface-0`), for the browser chrome. index.html carries the
// same dark value in its boot script; a test keeps all three equal to tokens.css.
export const themeColors: Record<Theme, string> = { light: '#fafafa', dark: '#0a0a0a' };

export function applyTheme(theme: Theme, root: HTMLElement = document.documentElement) {
  root.dataset.theme = theme;
  root.ownerDocument.querySelector('meta[name="theme-color"]')?.setAttribute('content', themeColors[theme]);
}

export function applyStoredTheme() {
  applyTheme(storedTheme());
}

// Blocked storage (private windows, strict policies) must not stop the switch on this page.
export function saveTheme(theme: Theme, storage: ThemeStorage | undefined = defaultStorage()) {
  applyTheme(theme);
  try { storage?.setItem(themeStorageKey, theme); } catch { /* remembered for this page only */ }
}

// onThemeChange calls listener when another tab of this origin saves a theme, and returns the way to stop.
export function onThemeChange(listener: (theme: Theme) => void): () => void {
  const onStorage = (event: StorageEvent) => {
    if (event.key === themeStorageKey) listener(event.newValue === 'dark' ? 'dark' : 'light');
  };
  window.addEventListener('storage', onStorage);
  return () => window.removeEventListener('storage', onStorage);
}
