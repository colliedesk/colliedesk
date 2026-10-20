import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { HelpCenterSettings } from './help-center-card';
import { json, serve } from './kb.test.fixture';

afterEach(() => vi.unstubAllGlobals());

const off = { enabled: false, title: '', locale: 'pt-BR', content_version: 3, updated_at: null, url: 'https://desk.example.com/help/acme' };

function open(canManage: boolean) {
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><HelpCenterSettings canManage={canManage} workspaceName="Acme" /></QueryClientProvider>);
}

test('an admin turns the help center on with a title; the stored language is sent back unchanged', async () => {
  const calls = serve({ 'GET /api/v1/help-center': () => json(200, off), 'PUT /api/v1/help-center': (call) => json(200, { ...off, ...(call.body as object), content_version: 4 }) });
  open(true);
  expect(await screen.findByText(/The help center is off/)).toHaveAttribute('role', 'status');
  fireEvent.click(screen.getByRole('checkbox', { name: 'Publish the help center' }));
  fireEvent.change(screen.getByRole('textbox', { name: 'Title' }), { target: { value: ' Acme help ' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save help center' }));
  await waitFor(() => expect(screen.getAllByRole('status')[0]).toHaveTextContent('The help center is on'));
  expect(calls.find((call) => call.method === 'PUT')?.body).toEqual({ enabled: true, title: 'Acme help', locale: 'pt-BR' });
});

test('an agent sees whether it is on but cannot change it', async () => {
  serve({ 'GET /api/v1/help-center': () => json(200, { ...off, enabled: true }) });
  open(false);
  expect(await screen.findByText(/The help center is on/)).toHaveAttribute('role', 'status');
  expect(screen.queryByRole('checkbox')).not.toBeInTheDocument();
  expect(screen.getByText('Only admins can turn the help center on or off.')).toBeInTheDocument();
});

test('everyone sees the public address as a link, also while the help center is off', async () => {
  serve({ 'GET /api/v1/help-center': () => json(200, off) });
  open(false);
  const link = await screen.findByRole('link', { name: 'https://desk.example.com/help/acme' });
  expect(link).toHaveAttribute('href', 'https://desk.example.com/help/acme');
  expect(link).toHaveAttribute('rel', 'noopener noreferrer');
});

test('Settings opened at #help-center-heading (the reply box links there) scrolls to the card once it is there', async () => {
  serve({ 'GET /api/v1/help-center': () => json(200, off) });
  const scrolled = vi.fn();
  Element.prototype.scrollIntoView = scrolled;
  window.history.replaceState(null, '', '/settings#help-center-heading');
  try {
    render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <h2 id="help-center-heading">Help center</h2><HelpCenterSettings canManage workspaceName="Acme" /></QueryClientProvider>);
    await screen.findByText(/The help center is off/);
    await waitFor(() => expect(scrolled).toHaveBeenCalledOnce());
    expect(scrolled.mock.contexts[0]).toBe(screen.getByRole('heading', { name: 'Help center' }));
  } finally {
    window.history.replaceState(null, '', '/');
    delete (Element.prototype as { scrollIntoView?: unknown }).scrollIntoView;
  }
});
