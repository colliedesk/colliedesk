import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { HelpCenterSearchConsoleSection } from './help-center-search-console';
import { json, serve } from './kb.test.fixture';

afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

const google = 'Ab3dEf6hIj9lMn2pQr5tUv8xYz1-Ab3dEf6hIj9lMn2';
const bing = '0A1B2C3D4E5F60718293A4B5C6D7E8F9';
type Saved = { google_site_verification: string | null; bing_site_verification: string | null; content_version: number; updated_at: string | null };
const none: Saved = { google_site_verification: null, bing_site_verification: null, content_version: 0, updated_at: null };

function open(canManage: boolean) {
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
    <HelpCenterSearchConsoleSection canManage={canManage} /></QueryClientProvider>);
}

test('an admin pastes both tokens, saves them and sees them saved', async () => {
  let saved: Saved = none;
  const calls = serve({
    'GET /api/v1/help-center/search-console': () => json(200, saved),
    'PUT /api/v1/help-center/search-console': (call) => {
      saved = { ...none, ...(call.body as object), content_version: 1, updated_at: '2026-10-28T09:00:00Z' };
      return json(200, saved);
    },
  });
  open(true);
  expect(await screen.findByRole('heading', { name: 'Search Console' })).toBeInTheDocument();
  fireEvent.change(screen.getByRole('textbox', { name: 'Google Search Console token' }), { target: { value: ` ${google} ` } });
  fireEvent.change(screen.getByRole('textbox', { name: 'Bing Webmaster Tools token' }), { target: { value: bing } });
  fireEvent.click(screen.getByRole('button', { name: 'Save tokens' }));
  expect(await screen.findByText('Saved. The tags are on every page of the help center.')).toBeInTheDocument();
  expect(calls.find((call) => call.method === 'PUT')?.body).toEqual({ google_site_verification: google, bing_site_verification: bing });
});

test('an empty field removes its token, and a refused token shows its message next to the field', async () => {
  let attempt = 0;
  const calls = serve({
    'GET /api/v1/help-center/search-console': () => json(200, { ...none, google_site_verification: google, bing_site_verification: bing, content_version: 3 }),
    'PUT /api/v1/help-center/search-console': () => ++attempt === 1
      ? json(422, { error: { code: 'validation_failed', message: 'Check the highlighted fields. Nothing was saved.', fields: { google_site_verification: 'Paste only the content value of the meta tag: 16 to 100 letters, digits, - or _.' } } })
      : json(200, { ...none, google_site_verification: google, content_version: 4 }),
  });
  open(true);
  const bingField = await screen.findByRole('textbox', { name: 'Bing Webmaster Tools token' });
  expect(bingField).toHaveValue(bing);
  fireEvent.change(screen.getByRole('textbox', { name: 'Google Search Console token' }), { target: { value: '<meta name="google-site-verification" content="x">' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save tokens' }));
  expect(await screen.findByText(/Paste only the content value of the meta tag/)).toBeInTheDocument();
  fireEvent.change(screen.getByRole('textbox', { name: 'Google Search Console token' }), { target: { value: google } });
  fireEvent.change(bingField, { target: { value: '' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save tokens' }));
  expect(await screen.findByText('Saved. The tags are on every page of the help center.')).toBeInTheDocument();
  expect(calls.filter((call) => call.method === 'PUT').at(-1)?.body).toEqual({ google_site_verification: google, bing_site_verification: null });
});

test('an agent without admin rights reads the tokens but cannot change them', async () => {
  serve({ 'GET /api/v1/help-center/search-console': () => json(200, { ...none, google_site_verification: google }) });
  open(false);
  expect(await screen.findByRole('heading', { name: 'Search Console' })).toBeInTheDocument();
  expect(screen.getByText('Google Search Console: set.')).toBeInTheDocument();
  expect(screen.getByText('Bing Webmaster Tools: not set.')).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Save tokens' })).not.toBeInTheDocument();
  expect(screen.getByText(/Only an admin can change/)).toBeInTheDocument();
});
