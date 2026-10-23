import { fireEvent, render, screen, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type { Results, ResultsWindow } from '../../api/results';
import { resultsSchema } from '../../api/results';
import { ResultsPage } from './results-page';
import type { ResultsPageProps } from './results-page';

const now = new Date('2026-10-23T12:00:00Z');

function window(overrides: Partial<ResultsWindow> = {}): ResultsWindow {
  return {
    created: { from: '2026-09-23T12:00:00Z', to: '2026-10-23T12:00:00Z' },
    resolved_created: { from: '2026-09-16T12:00:00Z', to: '2026-10-16T12:00:00Z' },
    available_on: null,
    first_response: { median_seconds: 41 * 60, p90_seconds: 2 * 3600 + 5 * 60, answered: 388, waiting: 8 },
    resolution: { median_seconds: 19 * 3600, resolved: 301, unresolved: 5, without_resolve_time: 0 },
    reopened: { rate: 0.06, reopened: 18, resolutions: 301 },
    volume: { count: 396, from: '2026-09-23T12:00:00Z', days: 30, per_week: 92.4 },
    handle: null,
    ai: { sent_unchanged: 64, sent_edited: 112, discarded: 31, conversations_sent_unchanged: 52 },
    ...overrides,
  };
}

const baselineWindow = window({
  created: { from: '2026-08-02T12:00:00Z', to: '2026-09-01T12:00:00Z' },
  resolved_created: { from: '2026-07-26T12:00:00Z', to: '2026-08-25T12:00:00Z' },
  first_response: { median_seconds: 3 * 3600 + 12 * 60, p90_seconds: 9 * 3600 + 40 * 60, answered: 412, waiting: 6 },
  resolution: { median_seconds: 28 * 3600, resolved: 371, unresolved: 19, without_resolve_time: 4 },
  reopened: null, ai: null,
  volume: { count: 418, from: '2026-08-02T12:00:00Z', days: 30, per_week: 98 },
  handle: { median_minutes: 15, conversations: 212 },
});

function results(overrides: Partial<Results> = {}): Results {
  return resultsSchema.parse({
    current: { id: '11111111-1111-4111-8111-111111111111', computed_at: '2026-10-23T11:20:00Z', measured_at: '2026-10-23T11:20:00Z',
      history_since: '2026-06-01T00:00:00Z', stats_read: 0, windows: { '30d': window(), '7d': window() } },
    baseline: { id: '22222222-2222-4222-8222-222222222222', computed_at: '2026-09-01T09:20:00Z', measured_at: '2026-09-01T09:12:00Z',
      source: 'freshdesk:account:7', import_status: 'OK', end: '2026-09-01T09:00:00Z', stats_read: 412, windows: { '30d': baselineWindow, '7d': baselineWindow } },
    baseline_state: 'ready',
    hours_saved: { '30d': { hours: 13, conversations: 52, median_handle_minutes: 15 }, '7d': null },
    error: null,
    ...overrides,
  });
}

function renderPage(props: Partial<ResultsPageProps> = {}) {
  const onWindow = vi.fn();
  render(<ResultsPage results={results()} loading={false} failed={false} onRetry={() => {}} window="30d" onWindow={onWindow} timezone="Europe/Berlin"
    isAdmin now={now} link={({ result, className, children }) => <a href={`/?status=all&result=${result}`} className={className}>{children}</a>}
    importLink={<a href="/settings/import-freshdesk">Open the import</a>} {...props} />);
  return { onWindow };
}

describe('ResultsPage', () => {
  it('shows each number next to the baseline, how it is counted, and the conversations it counts', () => {
    const { onWindow } = renderPage();
    const first = screen.getByRole('region', { name: 'First response time' });
    expect(within(first).getByText(/Time from a customer’s first message/)).toBeVisible();
    expect(within(first).getByRole('link', { name: '3 h 12 min' })).toHaveAttribute('href', '/?status=all&result=22222222-2222-4222-8222-222222222222:30d.first_response');
    expect(within(first).getByRole('link', { name: '41 min' })).toHaveAttribute('href', '/?status=all&result=11111111-1111-4111-8111-111111111111:30d.first_response');
    expect(within(first).getByRole('link', { name: '8 waiting for a first reply' })).toHaveAttribute('href', '/?status=all&result=11111111-1111-4111-8111-111111111111:30d.first_response_waiting');
    expect(within(first).getByText('2 h 31 min faster')).toBeVisible();
    expect(within(first).getByText('79% less time')).toBeVisible();

    const reopened = screen.getByRole('region', { name: 'Reopened within 7 days' });
    expect(within(reopened).getByText('Freshdesk does not share reopen history, so there is nothing to compare.')).toBeVisible();
    expect(within(reopened).getByRole('link', { name: '6%' })).toBeVisible();
    expect(within(screen.getByRole('region', { name: 'Handle time' })).getByText('Colliedesk does not record time yet (issue #119).')).toBeVisible();
    expect(within(screen.getByRole('region', { name: 'Conversations per week' })).getByText('6% fewer')).toBeVisible();

    const ai = screen.getByRole('region', { name: 'AI drafts' });
    expect(within(ai).getByRole('link', { name: '64' })).toHaveAttribute('href', '/?status=all&result=11111111-1111-4111-8111-111111111111:30d.ai_sent_unchanged');
    expect(within(ai).getByText(/52 conversations with a draft sent nearly unchanged × 15 min/)).toBeVisible();
    expect(screen.getByText('Updated 40 min ago')).toBeVisible();

    fireEvent.click(screen.getByRole('button', { name: 'Last 7 days' }));
    expect(onWindow).toHaveBeenCalledWith('7d');
  });

  it('offers the import when there is nothing to compare with', () => {
    renderPage({ results: results({ baseline: null, baseline_state: 'none', hours_saved: { '30d': null, '7d': null } }) });
    expect(screen.getByRole('heading', { name: 'Nothing from Freshdesk to compare with yet' })).toBeVisible();
    expect(screen.getByRole('link', { name: 'Open the import' })).toBeVisible();
    expect(screen.queryByText('Freshdesk, before you moved')).toBeNull();
    expect(screen.getByText('Needs handle time from Freshdesk. Import from Freshdesk to see it.')).toBeVisible();
  });

  it('never shows numbers from a failed import, and labels an unverified one', () => {
    const failed = results();
    failed.baseline!.import_status = 'FAILED';
    renderPage({ results: failed });
    expect(screen.getByText(/the import FAILED/)).toBeVisible();
    expect(screen.queryByRole('link', { name: '3 h 12 min' })).toBeNull();
    expect(screen.getAllByText('Not shown: the import failed.').length).toBeGreaterThan(0);
  });

  it('labels the numbers of an unverified import', () => {
    const unverified = results();
    unverified.baseline!.import_status = 'UNVERIFIED';
    renderPage({ results: unverified });
    expect(screen.getByText(/UNVERIFIED import/)).toBeVisible();
    expect(screen.getByRole('link', { name: '3 h 12 min' })).toBeVisible();
  });

  it('says when resolutions will be available and when a sample is too small', () => {
    const early = results();
    early.current!.windows['30d'] = window({ available_on: '2026-11-08T00:00:00Z', resolution: null, reopened: null,
      first_response: { median_seconds: null, p90_seconds: null, answered: 3, waiting: 0 } });
    early.current!.history_since = '2026-10-02T00:00:00Z';
    renderPage({ results: early });
    expect(screen.getAllByText('Available on Nov 8').length).toBe(2);
    expect(screen.getByText(/start on Nov 8/)).toBeVisible();
    expect(screen.getByText('Too few (3)')).toBeVisible();
    expect(screen.getByText('Answered conversations are 3. A median needs at least 5.')).toBeVisible();
  });

  it('asks to run an earlier import again for resolve times', () => {
    const old = results();
    old.baseline!.stats_read = 0;
    renderPage({ results: old });
    expect(screen.getByText(/needs one more import/)).toBeVisible();
    expect(screen.getByText('Your earlier import did not read resolve times from Freshdesk.')).toBeVisible();
  });

  it('shows the compute error to admins only', () => {
    const failing = results({ error: { message: 'statement timeout', at: '2026-10-23T11:00:00Z' } });
    renderPage({ results: failing });
    expect(screen.getByRole('alert')).toHaveTextContent('statement timeout');
  });

  it('says what to do when the page cannot load', () => {
    const onRetry = vi.fn();
    renderPage({ results: undefined, failed: true, onRetry });
    expect(screen.getByRole('alert')).toHaveTextContent('Results did not load');
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(onRetry).toHaveBeenCalled();
  });
});
