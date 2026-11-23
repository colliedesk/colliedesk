import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import type { HelpCenterWindow, Results } from '../../api/results';
import { hardwareCenter, json, primaryCenter, serve } from '../kb/kb.test.fixture';
import { ResultsHelpCenterChoice } from './results-help-center-choice';

afterEach(() => { vi.unstubAllGlobals(); });

const quiet = (views: number, requests: number, title: string): HelpCenterWindow => ({
  from: '2026-11-13T23:00:00Z', to: '2026-11-20T23:00:00Z', views, requests, score: null, weeks: [], days: [],
  articles: [{ id: '33333333-3333-4333-8333-333333333333', number: 3, title, views, helpful: 0, votes: 0, helpful_percent: null, uses: 0 }], articles_total: 1,
});
const block = (window: HelpCenterWindow): Results['help_center'] => ({ timezone: 'UTC', windows: { '30d': window, '7d': window } });
const answer = (window: HelpCenterWindow): Results => ({
  help_center: block(window), current: null, baseline: null, baseline_state: 'none', hours_saved: {}, error: null, weekly_email: null,
});
const workspace = block(quiet(130, 5, 'Clear a paper jam'));

function open(readsHelpCenters = true) {
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
    <ResultsHelpCenterChoice workspace={workspace} window="7d" readsHelpCenters={readsHelpCenters} links={{
      requests: (range, children, search) => <a href={`/?created_after=${range.from}${search ? `&search=${search}` : ''}`}>{children}</a>,
      article: (_id, children) => children, conversation: (_id, children) => children,
    }} /></QueryClientProvider>);
  return screen.getByRole('region', { name: 'Help center' });
}

test('with two help centers the section reads one of them, and its requests open the inbox on that help center', async () => {
  const calls = serve({
    'GET /api/v1/help-centers': () => json(200, { help_centers: [primaryCenter, hardwareCenter], limit: 20 }),
    'GET /api/v1/results': (call) => json(200, answer(call.search.get('help_center_id') === hardwareCenter.id
      ? quiet(30, 2, 'Swap a cable') : quiet(100, 3, 'Clear a paper jam'))),
  });
  const box = open();
  const choice = await within(box).findByRole('combobox', { name: 'Help center' });
  expect(within(choice).getAllByRole('option').map((option) => option.textContent)).toEqual(['All help centers', 'Acme help', 'Hardware']);
  expect(within(box).getByRole('link', { name: '5' })).toHaveAttribute('href', '/?created_after=2026-11-13T23:00:00Z');
  expect(calls.some((call) => call.path === '/api/v1/results')).toBe(false);

  fireEvent.change(choice, { target: { value: hardwareCenter.id } });
  expect(await within(box).findByRole('link', { name: '2' })).toHaveAttribute('href', '/?created_after=2026-11-13T23:00:00Z&search=help_center:hardware');
  expect(within(box).getByText('Swap a cable')).toBeVisible();
  expect(within(box).queryByText('Clear a paper jam')).toBeNull();

  fireEvent.change(choice, { target: { value: primaryCenter.id } });
  expect(await within(box).findByRole('link', { name: '3' })).toHaveAttribute('href', '/?created_after=2026-11-13T23:00:00Z&search=help_center:primary');
  expect(calls.filter((call) => call.path === '/api/v1/results').map((call) => call.search.get('help_center_id'))).toEqual([hardwareCenter.id, primaryCenter.id]);

  fireEvent.change(choice, { target: { value: '' } });
  expect(await within(box).findByRole('link', { name: '5' })).toBeVisible();
});

test('one help center, or a role that cannot read help centers, shows the workspace without a choice', async () => {
  const calls = serve({ 'GET /api/v1/help-centers': () => json(200, { help_centers: [primaryCenter], limit: 20 }) });
  let box = open();
  await vi.waitFor(() => expect(calls.some((call) => call.path === '/api/v1/help-centers')).toBe(true));
  expect(within(box).queryByRole('combobox', { name: 'Help center' })).toBeNull();
  expect(within(box).getByRole('link', { name: '5' })).toBeVisible();
  cleanup();

  calls.length = 0;
  box = open(false);
  expect(within(box).getByRole('link', { name: '5' })).toBeVisible();
  expect(within(box).queryByRole('combobox', { name: 'Help center' })).toBeNull();
  expect(calls).toEqual([]);
});
