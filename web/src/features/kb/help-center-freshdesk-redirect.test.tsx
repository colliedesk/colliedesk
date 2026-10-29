import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { HelpCenterFreshdeskRedirectSection } from './help-center-freshdesk-redirect';
import { json, serve } from './kb.test.fixture';

afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

const base = { freshdesk_domain: 'acme.freshdesk.com', address: 'https://acme.help.example.net', keep_weeks: 4, snippet: null, mapped: null };
const snippet = '<script>\n(function () { var target = "https://acme.help.example.net"; })();\n</script>\n';

function open() {
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><HelpCenterFreshdeskRedirectSection /></QueryClientProvider>);
}

test('a workspace without a Freshdesk import shows nothing', async () => {
  const calls = serve({ 'GET /api/v1/help-center/freshdesk-redirect': () => json(200, { ...base, state: 'no_import', freshdesk_domain: null }) });
  open();
  await waitFor(() => expect(calls).toHaveLength(1));
  expect(screen.queryByRole('heading', { name: /Freshdesk/ })).not.toBeInTheDocument();
});

test('it asks for the Freshdesk plan, says plainly that Growth cannot redirect, and hands Pro the snippet with the 4 weeks advice', async () => {
  const writeText = vi.fn(async () => undefined);
  vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } });
  const calls = serve({
    'GET /api/v1/help-center/freshdesk-redirect': (call) => {
      const plan = call.search.get('plan');
      if (plan === 'growth') return json(200, { ...base, state: 'growth' });
      if (plan === 'pro') return json(200, { ...base, state: 'ready', snippet, mapped: { articles: 12, folders: 3, categories: 2, embedded: true } });
      return json(200, { ...base, state: 'ask_plan' });
    },
  });
  open();
  expect(await screen.findByRole('heading', { name: 'Old Freshdesk links' })).toBeInTheDocument();
  expect(screen.getByText(/acme\.freshdesk\.com/)).toBeInTheDocument();
  expect(screen.queryByRole('textbox', { name: 'Snippet for the Freshdesk portal head' })).not.toBeInTheDocument();

  fireEvent.click(screen.getByRole('radio', { name: 'Growth' }));
  expect(await screen.findByText(/cannot be redirected/)).toBeInTheDocument();
  expect(screen.queryByRole('textbox', { name: 'Snippet for the Freshdesk portal head' })).not.toBeInTheDocument();

  fireEvent.click(screen.getByRole('radio', { name: 'Pro' }));
  const box = await screen.findByRole('textbox', { name: 'Snippet for the Freshdesk portal head' });
  expect(box).toHaveValue(snippet);
  expect(screen.getByText(/at least 4 weeks/)).toBeInTheDocument();
  expect(screen.getByText(/not a 301/)).toBeInTheDocument();
  expect(screen.getByText(/12 articles, 3 folders and 2 categories/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Copy snippet' }));
  await waitFor(() => expect(writeText).toHaveBeenCalledWith(snippet));
  expect(calls.map((call) => call.search.get('plan'))).toEqual([null, 'growth', 'pro']);
});

test('a team with its own old domain is sent to DNS, and a help center that is off is told to turn on', async () => {
  serve({ 'GET /api/v1/help-center/freshdesk-redirect': () => json(200, { ...base, state: 'own_domain' }) });
  open();
  expect(await screen.findByText(/301 at your DNS provider/)).toBeInTheDocument();
  expect(screen.queryByRole('radio', { name: 'Pro' })).not.toBeInTheDocument();
  cleanup();
  serve({ 'GET /api/v1/help-center/freshdesk-redirect': () => json(200, { ...base, state: 'help_center_off' }) });
  open();
  expect(await screen.findByText(/Turn the help center on/)).toBeInTheDocument();
});
