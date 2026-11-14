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
    weekly_email: null,
    ...overrides,
  });
}

// line finds a one-line paragraph by its whole text, links in it included.
const line = (text: string) => screen.getByText((_, element) => element?.tagName === 'P' && element.textContent === text);

function renderPage(props: Partial<ResultsPageProps> = {}) {
  const onWindow = vi.fn();
  render(<ResultsPage results={results()} loading={false} failed={false} onRetry={() => {}} window="30d" onWindow={onWindow} timezone="Europe/Berlin"
    isAdmin now={now} link={({ result, className, children }) => <a href={`/?status=all&result=${result}`} className={className}>{children}</a>}
    importLink={<a href="/settings/data">Open the import</a>} {...props} />);
  return { onWindow };
}

describe('ResultsPage', () => {
  it('shows each number next to the baseline, how it is counted, and the conversations it counts', () => {
    const { onWindow } = renderPage();
    const first = screen.getByRole('region', { name: 'First response time' });
    expect(within(first).getByRole('button', { name: 'About First response time' })).toHaveAccessibleDescription(/Time from a customer’s first message/);
    expect(within(first).getByRole('link', { name: '3 h 12 min' })).toHaveAttribute('href', '/?status=all&result=22222222-2222-4222-8222-222222222222:30d.first_response');
    expect(within(first).getByRole('link', { name: '41 min' })).toHaveAttribute('href', '/?status=all&result=11111111-1111-4111-8111-111111111111:30d.first_response');
    expect(within(first).getByRole('link', { name: '8 waiting' })).toHaveAttribute('href', '/?status=all&result=11111111-1111-4111-8111-111111111111:30d.first_response_waiting');
    expect(within(first).getByText('2 h 31 min faster')).toBeVisible();
    expect(within(first).getByText('79% less time')).toBeVisible();

    const reopened = screen.getByRole('region', { name: 'Reopened within 7 days' });
    expect(within(reopened).getByText('Freshdesk shares no reopen history.')).toBeVisible();
    expect(within(reopened).getByRole('link', { name: '6%' })).toBeVisible();
    const handle = screen.getByRole('region', { name: 'Handle time' });
    expect(within(handle).getByText('Colliedesk does not record time yet.')).toBeVisible();
    // Customers read this page: it never points them at the project's issue tracker.
    expect(handle).not.toHaveTextContent(/issue #\d+/);
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

  it('treats an import unverified only by the plan\'s archive limit as information, not a warning', () => {
    const archive = results();
    archive.baseline!.import_status = 'UNVERIFIED';
    archive.baseline!.import_cause = 'archive_refused';
    renderPage({ results: archive });
    const note = screen.getByText(/Freshdesk doesn’t let your plan read archived tickets/);
    expect(note.closest('.results-notice-warn')).toBeNull();
    expect(screen.queryByText(/UNVERIFIED import/)).toBeNull();
    expect(screen.queryByText('Unverified import')).toBeNull();
    expect(screen.getByRole('link', { name: '3 h 12 min' })).toBeVisible();
  });

  it('says when resolutions will be available and when a sample is too small', () => {
    const early = results();
    early.current!.windows['30d'] = window({ available_on: '2026-11-08T00:00:00Z', resolution: null, reopened: null,
      first_response: { median_seconds: null, p90_seconds: null, answered: 3, waiting: 0 } });
    early.current!.history_since = '2026-10-02T00:00:00Z';
    renderPage({ results: early });
    // The window needs status history from Sep 16 (its first day, 7 days before the resolve window opens); history starts Oct 2.
    expect(screen.getAllByText('From Nov 8 · needs resolve history since Sep 16').length).toBe(2);
    expect(screen.getByText(/the comparison appears on Nov 8/)).toBeVisible();
    expect(screen.getByText(/Colliedesk has recorded when conversations are resolved since Oct 2/)).toBeVisible();
    expect(screen.queryByText(/start on Nov 8/)).toBeNull();
    const first = screen.getByRole('region', { name: 'First response time' });
    expect(line('Needs 5 answered conversations · 3 so far · 0 waiting')).toBeVisible();
    expect(within(first).queryByText(/Too few/)).toBeNull();
  });

  it('a metric with too little data says what it needs in one line, with the count so far', () => {
    const thin = results({ baseline: null, baseline_state: 'none', hours_saved: { '30d': null, '7d': null } });
    thin.current!.windows['30d'] = window({ first_response: { median_seconds: null, p90_seconds: null, answered: 0, waiting: 0 },
      reopened: { rate: null, reopened: 0, resolutions: 2 }, resolution: { median_seconds: null, resolved: 4, unresolved: 1, without_resolve_time: 0 } });
    renderPage({ results: thin });
    expect(line('Needs 5 answered conversations · 0 so far · 0 waiting')).toBeVisible();
    expect(line('Needs 5 resolved · 4 so far · 1 open or settling')).toBeVisible();
    expect(line('Needs 5 resolutions · 2 so far')).toBeVisible();
  });

  it('keeps the links to the conversations waiting for a reply and to the unresolved ones while a median is not there yet', () => {
    const thin = results({ baseline: null, baseline_state: 'none', hours_saved: { '30d': null, '7d': null } });
    thin.current!.windows['30d'] = window({ first_response: { median_seconds: null, p90_seconds: null, answered: 3, waiting: 8 },
      resolution: { median_seconds: null, resolved: 2, unresolved: 6, without_resolve_time: 0 } });
    renderPage({ results: thin });
    const current = '11111111-1111-4111-8111-111111111111';
    const first = screen.getByRole('region', { name: 'First response time' });
    expect(within(first).getByRole('link', { name: '8 waiting' })).toHaveAttribute('href', `/?status=all&result=${current}:30d.first_response_waiting`);
    expect(within(first).getByRole('link', { name: '3 so far' })).toHaveAttribute('href', `/?status=all&result=${current}:30d.first_response`);
    const resolve = screen.getByRole('region', { name: 'Time to resolve' });
    expect(within(resolve).getByRole('link', { name: '6 open or settling' })).toHaveAttribute('href', `/?status=all&result=${current}:30d.unresolved`);
  });

  it('keeps a line with a large count to 60 characters, and the waiting link wins over the prose', () => {
    const busy = results({ baseline: null, baseline_state: 'none', hours_saved: { '30d': null, '7d': null } });
    busy.current!.windows['30d'] = window({ first_response: { median_seconds: null, p90_seconds: null, answered: 4, waiting: 123456 },
      resolution: { median_seconds: null, resolved: 4, unresolved: 12345, without_resolve_time: 0 } });
    renderPage({ results: busy });
    for (const name of ['First response time', 'Time to resolve']) {
      const row = screen.getByRole('region', { name });
      for (const text of Array.from(row.querySelectorAll('.results-muted')).map((node) => node.textContent ?? '')) expect(text.length, text).toBeLessThanOrEqual(60);
    }
    expect(within(screen.getByRole('region', { name: 'First response time' })).getByRole('link', { name: '123456 waiting' })).toBeVisible();
    expect(within(screen.getByRole('region', { name: 'Time to resolve' })).getByRole('link', { name: '12345 open or settling' })).toBeVisible();
  });

  it('a phone card names the days its own number counts: resolve rows start 7 days before the others', () => {
    renderPage();
    // Created Sep 23 – Oct 23 for what customers started; Sep 16 – Oct 16 for what had 7 days to settle.
    const labelOf = (name: string) => Array.from(screen.getByRole('region', { name }).querySelectorAll('.results-cell-label .results-narrow')).map((node) => node.textContent);
    expect(labelOf('First response time')).toEqual(['Created Aug 2 – Sep 1', 'Created Sep 23 – Oct 23']);
    expect(labelOf('Conversations per week')).toEqual(['Created Aug 2 – Sep 1', 'Created Sep 23 – Oct 23']);
    for (const name of ['Time to resolve', 'Reopened within 7 days', 'Handle time']) expect(labelOf(name), name).toEqual(['Created Jul 26 – Aug 25', 'Created Sep 16 – Oct 16']);
  });

  it('keeps how a number is counted in a tip beside its name, not in the row', () => {
    renderPage();
    for (const [name, how] of [['Time to resolve', /counting only resolutions that stayed resolved for 7 days/], ['Conversations per week', /divided by the weeks in the window/]] as const) {
      const row = screen.getByRole('region', { name });
      expect(within(row).getByRole('button', { name: `About ${name}` })).toHaveAccessibleDescription(how);
      // The tip's text is in the page for the accessible description, but nobody sees it until the tip opens.
      expect(within(row).getByText(how)).not.toBeVisible();
    }
    expect(screen.queryByText('How we count:')).toBeNull();
    // The two windows differ by the 7 days a resolution needs to settle: the tip says which window it counts.
    expect(within(screen.getByRole('region', { name: 'Time to resolve' })).getByRole('button', { name: 'About Time to resolve' }))
      .toHaveAccessibleDescription(/Colliedesk: conversations created Sep 16 – Oct 16\. Freshdesk: .* Jul 26 – Aug 25\./);
  });

  it('shows no helper line longer than 60 characters under a metric, with or without an import', () => {
    for (const given of [results({ baseline: null, baseline_state: 'none', hours_saved: { '30d': null, '7d': null } }), results()]) {
      const { unmount } = render(<ResultsPage results={given} loading={false} failed={false} onRetry={() => {}} window="30d" onWindow={() => {}} timezone="Europe/Berlin"
        isAdmin now={now} link={({ result, className, children }) => <a href={`/?result=${result}`} className={className}>{children}</a>} importLink={null} />);
      for (const row of Array.from(document.querySelectorAll('.results-row'))) {
        for (const line of Array.from(row.querySelectorAll('.results-muted'))) expect((line.textContent ?? '').length, line.textContent ?? '').toBeLessThanOrEqual(60);
      }
      unmount();
    }
  });

  it('describes Colliedesk alone until a Freshdesk import exists, and says so only once there is something to compare', () => {
    const { unmount } = render(<ResultsPage results={results({ baseline: null, baseline_state: 'none', hours_saved: { '30d': null, '7d': null } })} loading={false} failed={false}
      onRetry={() => {}} window="30d" onWindow={() => {}} timezone="Europe/Berlin" isAdmin now={now} link={({ children }) => <>{children}</>} importLink={null} />);
    expect(screen.getByText('How your team is doing in Colliedesk. Every number opens the conversations it counts.')).toBeVisible();
    expect(screen.queryByText(/How your team did in Freshdesk/)).toBeNull();
    unmount();
    renderPage();
    expect(screen.getByText('How your team did in Freshdesk, next to how it does in Colliedesk. Every number opens the conversations it counts.')).toBeVisible();
  });

  it('does not announce a comparison that is not coming when nothing was imported', () => {
    const fresh = results({ baseline: null, baseline_state: 'none', hours_saved: { '30d': null, '7d': null } });
    fresh.current!.windows['30d'] = window({ available_on: '2026-12-21T00:00:00Z', resolution: null, reopened: null });
    renderPage({ results: fresh });
    expect(screen.queryByText(/not compared yet/)).toBeNull();
    expect(screen.getAllByText('From Dec 21 · needs resolve history since Sep 16').length).toBe(2);
  });

  it('shows the count, not a bold weekly rate, while less than a week of conversations exists', () => {
    const young = results({ baseline: null, baseline_state: 'none', hours_saved: { '30d': null, '7d': null } });
    young.current!.windows['30d'] = window({ volume: { count: 3, from: '2026-10-20T12:00:00Z', days: 2.8, per_week: 7.5 } });
    renderPage({ results: young });
    const volume = screen.getByRole('region', { name: 'Conversations per week' });
    expect(within(volume).getByRole('link', { name: '3 conversations' })).toBeVisible();
    expect(within(volume).getByText('A weekly rate needs a week · 2 days so far')).toBeVisible();
    expect(within(volume).queryByText(/a week$/)).toBeNull();
  });

  it('right after the move, compares no weekly volume and says Colliedesk has no conversation yet', () => {
    const moved = results({ hours_saved: { '30d': null, '7d': null } });
    moved.current!.windows['30d'] = window({ volume: { count: 0, from: '2026-09-23T12:00:00Z', days: 0, per_week: null } });
    moved.baseline!.windows['30d'] = window({ ...baselineWindow, handle: { median_minutes: null, conversations: 2 } });
    renderPage({ results: moved });
    const volume = screen.getByRole('region', { name: 'Conversations per week' });
    expect(within(volume).getByText('No conversation a customer started yet.')).toBeVisible();
    expect(within(volume).queryByText(/fewer|more/)).toBeNull();
    // Already on Last 30 days: the hint does not ask to switch to it.
    const ai = screen.getByRole('region', { name: 'AI drafts' });
    expect(within(ai).getByText(/It appears when there is more history\./)).toBeVisible();
    expect(within(ai).queryByText(/Switch to Last 30 days/)).toBeNull();
  });

  it('on Last 7 days, a too small Freshdesk sample points to Last 30 days', () => {
    renderPage({ results: results({ hours_saved: { '30d': null, '7d': null } }), window: '7d' });
    expect(within(screen.getByRole('region', { name: 'AI drafts' })).getByText(/Switch to Last 30 days, or wait for more history\./)).toBeVisible();
  });

  it('asks to run an earlier import again for resolve times', () => {
    const old = results();
    old.baseline!.stats_read = 0;
    renderPage({ results: old });
    expect(screen.getByText(/needs one more import/)).toBeVisible();
    expect(screen.getByText('The earlier import skipped resolve times.')).toBeVisible();
  });

  it('shows the compute error to admins only', () => {
    const failing = results({ error: { message: 'statement timeout', at: '2026-10-23T11:00:00Z' } });
    renderPage({ results: failing });
    expect(screen.getByRole('alert')).toHaveTextContent('statement timeout');
  });

  it('says what to do when the page cannot load', () => {
    const onRetry = vi.fn();
    renderPage({ results: undefined, failed: true, onRetry });
    expect(screen.getByRole('alert')).toHaveTextContent('Reports did not load');
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(onRetry).toHaveBeenCalled();
  });
});
