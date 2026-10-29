// The console keeps its own theme choice (colliedesk.operator.theme): on the app's address it shares the app's
// storage, and each keeps what its person chose. public-operator/theme-boot.js applies it before first paint.
export type Theme = 'light' | 'dark';
const key = 'colliedesk.operator.theme';
const colors: Record<Theme, string> = { light: '#fafafa', dark: '#0a0a0a' };

export function storedTheme(): Theme {
  try {
    return localStorage.getItem(key) === 'dark' ? 'dark' : 'light';
  } catch {
    return 'light';
  }
}

export function setTheme(theme: Theme) {
  try {
    localStorage.setItem(key, theme);
  } catch { /* private window: the choice lasts this page */ }
  if (theme === 'dark') document.documentElement.dataset.theme = 'dark';
  else delete document.documentElement.dataset.theme;
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', colors[theme]);
}
