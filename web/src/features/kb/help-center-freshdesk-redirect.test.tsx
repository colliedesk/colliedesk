import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
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
  expect(screen.getByText('Your help center came from acme.freshdesk.com.')).toBeInTheDocument();
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

// Each row keeps one short line; the how and why sit in the tip next to it.
const readyWith = (mapped: { articles: number; folders: number; categories: number; embedded: boolean }) =>
  serve({ 'GET /api/v1/help-center/freshdesk-redirect': () => json(200, { ...base, state: 'ready', snippet, mapped }) });

test('the old links card says where the help center came from in one line, and why a snippet is needed in a tip', async () => {
  serve({ 'GET /api/v1/help-center/freshdesk-redirect': () => json(200, { ...base, state: 'ask_plan' }) });
  open();
  const section = (await screen.findByRole('heading', { name: 'Old Freshdesk links' })).closest('section') as HTMLElement;
  expect(section).toHaveTextContent('Your help center came from acme.freshdesk.com.');
  expect(within(section).getByRole('button', { name: 'About Old Freshdesk links' })).toHaveAccessibleDescription(
    'Search engines and readers still know its old addresses. Freshdesk cannot answer 301 from a acme.freshdesk.com address, so a snippet in your Freshdesk portal sends them to the new pages.');
  expect(section).toHaveTextContent('Only Pro and Enterprise can add code to the portal.');
});

test('the snippet comes with one line each on where to paste it, what kind of redirect it is and how long to keep the account, and the rest in tips', async () => {
  readyWith({ articles: 12, folders: 3, categories: 2, embedded: true });
  open();
  fireEvent.click(await screen.findByRole('radio', { name: 'Pro' }));
  const box = (await screen.findByRole('textbox', { name: 'Snippet for the Freshdesk portal head' })).closest('.copy-field') as HTMLElement;
  expect(box).toHaveTextContent('Paste it in the head of your Freshdesk portal.');
  const steps = within(box).getByRole('button', { name: 'About pasting the snippet' });
  expect(steps).toHaveAccessibleDescription('In Freshdesk, open Admin → Channels → Portals, choose the portal, then Customize portal → Layout & Pages → Head, paste the snippet and save.');
  fireEvent.click(steps);
  expect(within(box).getByRole('link', { name: 'Learn more' })).toHaveAttribute('href', 'https://colliedesk.com/docs/import-freshdesk#old-freshdesk-com-addresses');
  expect(box).toHaveTextContent('A JavaScript redirect, not a 301.');
  expect(within(box).getByRole('button', { name: 'About the redirect type' })).toHaveAccessibleDescription(
    'It moves readers and search engines that run scripts. Only help pages move; sign-in and tickets stay on Freshdesk.');
  expect(box).toHaveTextContent('Keep the account and snippet for at least 4 weeks.');
  expect(within(box).getByRole('button', { name: 'About keeping the account' })).toHaveAccessibleDescription(
    'The weeks count from the switch. It works only while the Freshdesk account exists. Freshdesk suspends a cancelled account after 24 hours and deletes it after 14 days.');
});

test('a knowledge base too large to list says so in one line, and how old addresses are found in a tip', async () => {
  readyWith({ articles: 9000, folders: 400, categories: 40, embedded: false });
  open();
  fireEvent.click(await screen.findByRole('radio', { name: 'Pro' }));
  const box = (await screen.findByRole('textbox', { name: 'Snippet for the Freshdesk portal head' })).closest('.copy-field') as HTMLElement;
  expect(box).toHaveTextContent('Your knowledge base is too large to list in the snippet.');
  expect(within(box).getByRole('button', { name: 'About the address list' })).toHaveAccessibleDescription(
    'It sends each old address by its id to the help center, which finds the new page.');
});
