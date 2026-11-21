import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { HelpCenterAnalyticsSection } from './help-center-analytics';
import { json, serve } from './kb.test.fixture';

afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

const path = '/api/v1/help-center/analytics';
type Saved = {
  provider: 'ga4' | 'plausible' | null; enabled: boolean; ga4_measurement_id: string | null; ga4_api_secret_set: boolean;
  plausible_domain: string | null; plausible_base_url: string; last_sent_at: string | null; last_error: string | null;
  last_error_at: string | null; updated_at: string | null; updated_by_name: string | null;
};
const none: Saved = { provider: null, enabled: false, ga4_measurement_id: null, ga4_api_secret_set: false, plausible_domain: null,
  plausible_base_url: 'https://plausible.io', last_sent_at: null, last_error: null, last_error_at: null, updated_at: null, updated_by_name: null };

function open(canManage: boolean) {
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
    <HelpCenterAnalyticsSection canManage={canManage} timezone="Asia/Ho_Chi_Minh" /></QueryClientProvider>);
}

test('an admin chooses GA4, saves the measurement ID and secret, and the secret never shows again', async () => {
  let saved: Saved = none;
  const calls = serve({
    [`GET ${path}`]: () => json(200, saved),
    [`PUT ${path}`]: () => {
      saved = { ...none, provider: 'ga4', enabled: true, ga4_measurement_id: 'G-ABC123XYZ9', ga4_api_secret_set: true, updated_at: '2025-11-20T09:00:00Z', updated_by_name: 'Ada' };
      return json(200, saved);
    },
  });
  open(true);
  expect(await screen.findByRole('heading', { name: 'Analytics' })).toBeInTheDocument();
  expect(screen.getByText('Off: no page view is sent.')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('radio', { name: 'Google Analytics 4' }));
  fireEvent.change(screen.getByRole('textbox', { name: 'Measurement ID' }), { target: { value: ' G-ABC123XYZ9 ' } });
  fireEvent.change(screen.getByLabelText('API secret'), { target: { value: 'Mp_Secret_123' } });
  fireEvent.click(screen.getByRole('checkbox', { name: 'Send page views' }));
  fireEvent.click(screen.getByRole('button', { name: 'Save analytics' }));
  expect(await screen.findByText('Saved.')).toBeInTheDocument();
  expect(calls.find((call) => call.method === 'PUT')?.body).toEqual({
    provider: 'ga4', enabled: true, ga4_measurement_id: 'G-ABC123XYZ9', ga4_api_secret: 'Mp_Secret_123', plausible_domain: null, plausible_base_url: 'https://plausible.io',
  });
  expect(screen.getByLabelText('API secret')).toHaveValue('');
  expect(screen.getByText('A secret is saved. Leave this empty to keep it.')).toBeInTheDocument();
  expect(screen.getByText('On: no page view accepted yet. Send a test event to check.')).toBeInTheDocument();
});

test('saving GA4 again with the secret field empty keeps the saved secret', async () => {
  const calls = serve({
    [`GET ${path}`]: () => json(200, { ...none, provider: 'ga4', enabled: true, ga4_measurement_id: 'G-ABC123XYZ9', ga4_api_secret_set: true }),
    [`PUT ${path}`]: () => json(200, { ...none, provider: 'ga4', enabled: false, ga4_measurement_id: 'G-ABC123XYZ9', ga4_api_secret_set: true }),
  });
  open(true);
  fireEvent.click(await screen.findByRole('checkbox', { name: 'Send page views' }));
  fireEvent.click(screen.getByRole('button', { name: 'Save analytics' }));
  expect(await screen.findByText('Saved.')).toBeInTheDocument();
  const body = calls.find((call) => call.method === 'PUT')?.body as Record<string, unknown>;
  expect(body).not.toHaveProperty('ga4_api_secret');
  expect(body).toMatchObject({ provider: 'ga4', enabled: false });
});

test('a refused Plausible address shows its message next to the field', async () => {
  serve({
    [`GET ${path}`]: () => json(200, none),
    [`PUT ${path}`]: () => json(422, { error: { code: 'validation_failed', message: 'Check the highlighted fields. Nothing was saved.',
      fields: { plausible_base_url: '10.0.0.7 is a private address: a self-hosted Plausible must be on a public https address.' } } }),
  });
  open(true);
  fireEvent.click(await screen.findByRole('radio', { name: 'Plausible' }));
  expect(screen.getByRole('textbox', { name: 'Plausible address' })).toHaveValue('https://plausible.io');
  fireEvent.change(screen.getByRole('textbox', { name: 'Site domain' }), { target: { value: 'help.example.com' } });
  fireEvent.change(screen.getByRole('textbox', { name: 'Plausible address' }), { target: { value: 'https://10.0.0.7' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save analytics' }));
  expect(await screen.findByText(/10\.0\.0\.7 is a private address/)).toBeInTheDocument();
  expect(screen.getByRole('textbox', { name: 'Plausible address' })).toHaveAttribute('aria-invalid', 'true');
});

test('Send a test event shows what the service answered', async () => {
  const calls = serve({
    [`GET ${path}`]: () => json(200, { ...none, provider: 'plausible', enabled: true, plausible_domain: 'help.example.com', last_sent_at: '2025-11-20T09:04:00Z' }),
    [`POST ${path}/test`]: () => json(200, { ok: false, message: 'Plausible refused the event (400): domain not found.' }),
  });
  open(true);
  expect(await screen.findByText(/^Last page view accepted Nov 20, 2025, 4:04\sPM\.$/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Send a test event' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Plausible refused the event (400): domain not found.');
  expect(calls.filter((call) => call.method === 'POST')).toHaveLength(1);
});

test('the last error shows on the status line', async () => {
  serve({ [`GET ${path}`]: () => json(200, { ...none, provider: 'ga4', enabled: true, ga4_measurement_id: 'G-ABC123XYZ9', ga4_api_secret_set: true,
    last_error: 'GA4 did not answer within 5 seconds.', last_error_at: '2025-11-20T09:00:00Z' }) });
  open(true);
  expect(await screen.findByText(/^Last error Nov 20, 2025, 4:00\sPM: GA4 did not answer within 5 seconds\.$/)).toBeInTheDocument();
});

test('a box says what each service receives', async () => {
  serve({ [`GET ${path}`]: () => json(200, none) });
  open(true);
  fireEvent.click(await screen.findByRole('radio', { name: 'Google Analytics 4' }));
  const box = screen.getByRole('note', { name: 'What is sent' });
  expect(box).toHaveTextContent('page address, title, referrer, language and search words, with a visitor id that changes every day. Not the reader’s address or browser.');
  fireEvent.click(screen.getByRole('radio', { name: 'Plausible' }));
  expect(within(screen.getByRole('note', { name: 'What is sent' })).getByText(/the reader’s address and browser, which Plausible turns into its own daily visitor id/)).toBeInTheDocument();
});

test('an agent without admin rights sees where page views go but cannot change or test it', async () => {
  serve({ [`GET ${path}`]: () => json(200, { ...none, provider: 'plausible', enabled: true, plausible_domain: 'help.example.com', updated_by_name: 'Ada' }) });
  open(false);
  expect(await screen.findByText('Page views go to Plausible (help.example.com).')).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Save analytics' })).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Send a test event' })).not.toBeInTheDocument();
  expect(screen.getByText('Only an admin can change analytics.')).toBeInTheDocument();
});

test('an admin removes the saved GA4 secret, which stops sending to GA4', async () => {
  let saved: Saved = { ...none, provider: 'ga4', enabled: true, ga4_measurement_id: 'G-ABC123XYZ9', ga4_api_secret_set: true };
  const calls = serve({
    [`GET ${path}`]: () => json(200, saved),
    [`PUT ${path}`]: () => { saved = { ...saved, enabled: false, ga4_api_secret_set: false }; return json(200, saved); },
  });
  open(true);
  fireEvent.click(await screen.findByRole('button', { name: 'Remove secret' }));
  expect(await screen.findByText('Off: no page view is sent.')).toBeInTheDocument();
  expect(calls.find((call) => call.method === 'PUT')?.body).toEqual({
    provider: 'ga4', enabled: false, ga4_measurement_id: 'G-ABC123XYZ9', ga4_api_secret: '', plausible_domain: null, plausible_base_url: 'https://plausible.io',
  });
  expect(screen.queryByRole('button', { name: 'Remove secret' })).not.toBeInTheDocument();
});
