import { fireEvent, render, screen, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import type { ImportJob, ImportPreview, ImportReport } from '../../api/imports';
import { ImportPage } from './import-page';
import type { ImportPageProps } from './import-page';
import { currentJob } from './import-route';

const preview: ImportPreview = {
  domain: 'acme.freshdesk.com', account_id: 777, source: 'freshdesk:account:777',
  agents: [{ id: 101, name: 'Alice', email: 'alice@acme.example', active: true }], companies: 2, contacts: 3,
  tickets_outside_archive: 4800, highest_listed_ticket_id: 5000, archive: 'readable', canned_responses: 'readable', knowledge_base: 'none',
  rate_limit_per_minute: 100, api_calls: 9, checked_at: '2026-10-22T08:00:00Z',
};

function job(change: Partial<ImportJob> = {}): ImportJob {
  return {
    id: '11111111-1111-4111-8111-111111111111', kind: 'import', source: 'freshdesk', domain: 'acme.freshdesk.com', account_source: 'freshdesk:account:777',
    undo_of: null, state: 'previewed', phase: 'preview', options: {}, preview, progress: null, status: null, error_code: null, error_message: null,
    has_key: true, has_interim_report: false, has_report: false, cancel_requested: false, next_run_at: null, created_by: null,
    created_at: '2026-10-22T08:00:00Z', started_at: null, finished_at: null, updated_at: '2026-10-22T08:00:00Z', ...change,
  };
}

function page(props: Partial<ImportPageProps> = {}) {
  const handlers = { onPreview: vi.fn(), onStart: vi.fn(), onCancel: vi.fn(), onSetKey: vi.fn(), onUndo: vi.fn(), onNewImport: vi.fn() };
  render(<ImportPage isAdmin history={[]} timezone="UTC" {...handlers} {...props} />);
  return handlers;
}

test('an agent is told to ask an admin and sees no form', () => {
  page({ isAdmin: false });
  expect(screen.getByText(/Only a workspace admin can import/)).toBeInTheDocument();
  expect(screen.queryByLabelText('Freshdesk address')).not.toBeInTheDocument();
});

test('the account is read only from a Freshdesk address and a key, and the key is never shown', () => {
  const { onPreview } = page();
  expect(screen.getByLabelText('API key of a Freshdesk admin')).toHaveAttribute('type', 'password');
  expect(screen.getByText(/Free plan has no API. Start a 14-day trial/)).toBeInTheDocument();
  fireEvent.change(screen.getByLabelText('Freshdesk address'), { target: { value: 'acme.zendesk.com' } });
  fireEvent.change(screen.getByLabelText('API key of a Freshdesk admin'), { target: { value: 'secret' } });
  fireEvent.click(screen.getByRole('button', { name: 'Read account' }));
  expect(screen.getByRole('alert')).toHaveTextContent('<account>.freshdesk.com');
  expect(onPreview).not.toHaveBeenCalled();
    fireEvent.change(screen.getByLabelText('Freshdesk address'), { target: { value: 'https://Acme.freshdesk.com/' } });
  fireEvent.click(screen.getByRole('button', { name: 'Read account' }));
  expect(onPreview).toHaveBeenCalledWith('acme.freshdesk.com', 'secret');
});

test('the preview shows what Freshdesk holds before anything is written, and what the key cannot read', () => {
  const { onStart } = page({ job: job({ preview: { ...preview, archive: 'refused' } }) });
  const ledger = screen.getByRole('table');
  expect(within(ledger).getByRole('row', { name: /Tickets/ })).toHaveTextContent('at least 4,800');
  expect(within(ledger).getByRole('row', { name: /Contacts/ })).toHaveTextContent('3');
  expect(within(ledger).queryByRole('columnheader', { name: 'In Colliedesk' })).not.toBeInTheDocument();
  expect(screen.getByText(/cannot read archived tickets/)).toBeInTheDocument();
  expect(screen.getByText(/Tickets updated in the last 90 days come first/)).toBeInTheDocument();
  expect(screen.getByText(/at most 80 of the 100 Freshdesk API calls/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Start import' }));
  expect(onStart).toHaveBeenCalledWith(job().id);
});

test('a running import shows its step, its counts and when the recent tickets are in', () => {
  const { onCancel } = page({ job: job({ state: 'running', phase: 'full', has_interim_report: true,
    progress: { step: 'importing tickets', done: 25, total: 100, api_calls: 75, eta_seconds: 600, updated_at: '2026-10-22T08:05:00Z' } }) });
  const steps = screen.getByRole('list', { name: 'Import steps' });
  expect(within(steps).getByText('Whole account')).toHaveAttribute('aria-current', 'step');
  expect(screen.getByRole('status')).toHaveTextContent('The recent tickets are in');
  expect(screen.getByRole('progressbar', { name: 'importing tickets' })).toHaveAttribute('aria-valuenow', '25');
  expect(screen.getByText('importing tickets: 25 of 100')).toBeInTheDocument();
  expect(screen.getByText(/About 10 minutes left/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Stop import' }));
  expect(onCancel).toHaveBeenCalledWith(job().id);
});

test('a wait for Freshdesk says when the import tries again', () => {
  page({ job: job({ state: 'retry_wait', phase: 'full', error_code: 'freshdesk_unavailable', next_run_at: '2026-10-22T09:30:00Z' }) });
  expect(screen.getByText(/Freshdesk did not answer. The import tries again at/)).toBeInTheDocument();
});

test('an import that lost its key asks for it again and goes on', () => {
  const { onSetKey } = page({ job: job({ state: 'waiting_key', phase: 'full', has_key: false }) });
  fireEvent.change(screen.getByLabelText('API key of a Freshdesk admin'), { target: { value: 'again' } });
  fireEvent.click(screen.getByRole('button', { name: 'Continue import' }));
  expect(onSetKey).toHaveBeenCalledWith(job().id, 'again');
});

test('a finished import compares Freshdesk with Colliedesk, downloads its report and is undone only after a confirmation', () => {
  const report: ImportReport = { status: 'FAILED', tickets: { source: 10, imported: 9, failed: 1 }, messages: { source: 30, imported: 30, failed: 0 } };
  const { onUndo } = page({ job: job({ state: 'finished', phase: 'full', status: 'FAILED', has_report: true }), report });
  expect(screen.getByRole('heading', { name: 'Import incomplete' })).toBeInTheDocument();
  const tickets = screen.getByRole('row', { name: /Tickets/ });
  expect(within(tickets).getAllByRole('cell').map((cell) => cell.textContent)).toEqual(['10', '9', '1']);
  expect(screen.getByRole('link', { name: 'Download report' })).toHaveAttribute('href', `/api/v1/imports/${job().id}/report?format=markdown`);
  fireEvent.click(screen.getByRole('button', { name: 'Undo import' }));
  expect(onUndo).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Remove imported data' }));
  expect(onUndo).toHaveBeenCalledWith(job().id);
});

test('a plan without API calls says to start a Freshdesk trial, and the imported data can be undone', () => {
  const { onNewImport } = page({ job: job({ state: 'failed', phase: 'full', has_interim_report: true, error_code: 'freshdesk_plan_no_api', error_message: 'this Freshdesk plan has no API calls' }) });
  expect(screen.getByRole('alert')).toHaveTextContent('Start a 14-day trial of a higher Freshdesk plan');
  expect(screen.getByRole('button', { name: 'Undo import' })).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'New import' }));
  expect(onNewImport).toHaveBeenCalled();
});

test('the page is about the import still open, else the newest', () => {
  const finished = job({ id: '22222222-2222-4222-8222-222222222222', state: 'finished' });
  const waiting = job({ state: 'waiting_key' });
  expect(currentJob([finished, waiting])?.id).toBe(waiting.id);
  expect(currentJob([finished])?.id).toBe(finished.id);
  expect(currentJob([])).toBeUndefined();
});
