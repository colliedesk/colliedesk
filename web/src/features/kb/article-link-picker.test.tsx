import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { ArticleLinkPicker } from './article-link-picker';
import { json, refusal, serve, summary } from './kb.test.fixture';

afterEach(() => vi.unstubAllGlobals());

const center = (enabled: boolean) => ({ enabled, title: '', locale: 'en', content_version: 3, updated_at: null, url: 'https://desk.example.com/help/acme' });
const reset = summary(7, 'Reset your password', 'published');
const refund = summary(8, 'Refund steps', 'published');

function open(props: Partial<Parameters<typeof ArticleLinkPicker>[0]> = {}, client = new QueryClient({ defaultOptions: { queries: { retry: false } } })) {
  const onPick = vi.fn();
  render(<QueryClientProvider client={client}>
    <ArticleLinkPicker onPick={onPick} onClose={vi.fn()} {...props} />
  </QueryClientProvider>);
  return onPick;
}

test('lists only published articles, searches them on the server, and Enter picks one with its public address', async () => {
  const calls = serve({
    'GET /api/v1/help-center': () => json(200, center(true)),
    'GET /api/v1/kb/articles': (call) => json(200, { items: call.search.get('q') === 'refund' ? [refund] : [reset, refund], next_cursor: null }),
  });
  const onPick = open();
  expect(screen.getByRole('dialog', { name: 'Articles' })).toBeInTheDocument();
  expect(await screen.findByRole('option', { name: 'Reset your password', selected: true })).toBeInTheDocument();
  const search = screen.getByRole('combobox', { name: 'Search published articles' });
  expect(search).toHaveFocus();
  fireEvent.change(search, { target: { value: 'refund' } });
  await waitFor(() => expect(screen.getAllByRole('option').map((option) => option.textContent)).toEqual(['Refund steps']));
  fireEvent.keyDown(search, { key: 'Enter' });
  expect(onPick).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ title: 'Refund steps', url: 'https://desk.example.com/help/acme/a/8-refund-steps' }));
  const lists = calls.filter((call) => call.path === '/api/v1/kb/articles');
  expect(lists.every((call) => call.search.get('status') === 'published')).toBe(true);
  expect(lists.map((call) => call.search.get('q'))).toEqual([null, 'refund']);
});

test('while the help center is off it says why there is nothing to link, and an admin gets the way to Settings', async () => {
  const calls = serve({ 'GET /api/v1/help-center': () => json(200, center(false)) });
  const onManageHelpCenter = vi.fn();
  open({ onManageHelpCenter });
  expect(await screen.findByText(/The help center is off, so no article has a public page to link to\./)).toHaveAttribute('role', 'status');
  fireEvent.click(screen.getByRole('button', { name: 'Open help center settings' }));
  expect(onManageHelpCenter).toHaveBeenCalledOnce();
  expect(screen.queryByRole('option')).not.toBeInTheDocument();
  expect(calls.some((call) => call.path === '/api/v1/kb/articles')).toBe(false);
});

test('an agent is told who can turn it on', async () => {
  serve({ 'GET /api/v1/help-center': () => json(200, center(false)) });
  open();
  expect(await screen.findByText(/An admin can turn it on in Settings\./)).toHaveAttribute('role', 'status');
  expect(screen.queryByRole('button')).not.toBeInTheDocument();
});

test('says when nothing is published yet', async () => {
  serve({ 'GET /api/v1/help-center': () => json(200, center(true)), 'GET /api/v1/kb/articles': () => json(200, { items: [], next_cursor: null }) });
  open();
  expect(await screen.findByText(/No article is published yet\./)).toHaveAttribute('role', 'status');
});

test('a refused list is a failure, never "nothing published"', async () => {
  serve({ 'GET /api/v1/help-center': () => json(200, center(true)), 'GET /api/v1/kb/articles': () => refusal(402, 'workspace_locked', 'Locked') });
  open();
  expect(await screen.findByRole('alert')).toHaveTextContent('Articles could not be loaded.');
  expect(screen.queryByText('No article is published yet.')).not.toBeInTheDocument();
});

test('opening it asks again: the help center turned on in another tab, or an article just published, shows up', async () => {
  serve({ 'GET /api/v1/help-center': () => json(200, center(true)), 'GET /api/v1/kb/articles': () => json(200, { items: [reset], next_cursor: null }) });
  // The app keeps answers for 15 seconds; what this page saw a moment ago was the help center off.
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: 15_000 } } });
  client.setQueryData(['help-center'], center(false));
  open({}, client);
  expect(await screen.findByRole('option', { name: 'Reset your password' })).toBeInTheDocument();
});
