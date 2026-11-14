import base from './playwright.config';
export default { ...base, use: { ...base.use, launchOptions: { executablePath: '/home/chinh/.cache/ms-playwright/chromium_headless_shell-1254/chrome-headless-shell-linux64/chrome-headless-shell' } } };
