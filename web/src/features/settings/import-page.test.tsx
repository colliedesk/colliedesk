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
    undo_of: null, state: 'previewed', phase: 'preview', options: {}, preview, progress: null, status: null, error_code: null, error_message: null, imported_conversations: null,
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
  expect(screen.getByText(/leaves a fifth of that to your other Freshdesk apps/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Start import' }));
  expect(onStart).toHaveBeenCalledWith(job().id);
});

test('a running import shows its step, its counts and when the recent tickets are in', () => {
  const { onCancel } = page({ report: { status: 'INTERIM', interim: true, tickets: { source: 5, imported: 5, failed: 0 } }, job: job({ state: 'running', phase: 'full', has_interim_report: true,
    progress: { step: 'importing tickets', done: 25, total: 100, api_calls: 75, eta_seconds: 600, updated_at: '2026-10-22T08:05:00Z' } }) });
  const steps = screen.getByRole('list', { name: 'Import steps' });
  expect(within(steps).getByText('Whole account')).toHaveAttribute('aria-current', 'step');
  expect(screen.getByRole('status')).toHaveTextContent('The recent tickets are in');
  expect(screen.getByRole('progressbar', { name: 'Copying tickets' })).toHaveAttribute('aria-valuenow', '25');
  expect(screen.getByText('Copying tickets: 25 of 100')).toBeInTheDocument();
  expect(screen.getByRole('link', { name: 'Open the inbox' })).toHaveAttribute('href', '/');
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
  const { onNewImport } = page({ job: job({ state: 'failed', phase: 'full', has_interim_report: true, imported_conversations: 120, error_code: 'freshdesk_plan_no_api', error_message: 'this Freshdesk plan has no API calls' }) });
  expect(screen.getByText(/120 conversations from this account are in your inbox/)).toBeInTheDocument();
  expect(screen.getByRole('alert')).toHaveTextContent('Start a 14-day trial of a higher Freshdesk plan');
  expect(screen.getByRole('heading', { name: 'Your Freshdesk plan has no API access' })).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Undo import' })).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'New import' }));
  expect(onNewImport).toHaveBeenCalled();
});

test('a preview that ended without starting does not hide the import before it, unless it was just asked for', () => {
  const done = job({ id: '22222222-2222-4222-8222-222222222222', state: 'finished', phase: 'full', status: 'OK' });
  const refused = job({ state: 'failed', phase: 'preview' });
  expect(currentJob([refused, done])?.id).toBe(done.id);
  expect(currentJob([refused, done], refused.id)?.id).toBe(refused.id);
  expect(currentJob([refused])?.id).toBe(refused.id);
  // The preview asked for became the import, and its undo is newer: the undo leads.
  const undo = job({ id: '33333333-3333-4333-8333-333333333333', kind: 'undo', phase: 'undo', state: 'finished', undo_of: done.id });
  expect(currentJob([undo, done], done.id)?.id).toBe(undo.id);
});

test('the page is about the import still open, else the newest', () => {
  const finished = job({ id: '22222222-2222-4222-8222-222222222222', state: 'finished' });
  const waiting = job({ state: 'waiting_key' });
  expect(currentJob([finished, waiting])?.id).toBe(waiting.id);
  expect(currentJob([finished])?.id).toBe(finished.id);
  expect(currentJob([])).toBeUndefined();
});

test('a finished step never shows a full bar, and a step of unknown size shows no bar', () => {
  page({ job: job({ state: 'running', phase: 'full', progress: { step: 'checking ticket ids', done: 1, total: 1, api_calls: 3, updated_at: '2026-10-22T08:05:00Z' } }) });
  expect(screen.queryByRole('progressbar')).not.toBeInTheDocument();
  expect(screen.getByText('Checking ticket numbers: 1 of 1')).toBeInTheDocument();
});

test('a key Freshdesk refused while reading the account is a preview to try again, not a stopped import', () => {
  const { onNewImport } = page({ job: job({ state: 'failed', phase: 'preview', error_code: 'freshdesk_key_rejected', account_source: null }),
    history: [job({ id: '22222222-2222-4222-8222-222222222222', state: 'failed', phase: 'preview' })] });
  expect(screen.getByRole('heading', { name: 'Could not read your Freshdesk account' })).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
  expect(onNewImport).toHaveBeenCalled();
  expect(screen.queryByRole('heading', { name: 'Earlier imports' })).not.toBeInTheDocument();
});

test('trying again keeps the address that was typed', () => {
  page({ job: job({ state: 'failed', phase: 'preview', domain: 'beta.freshdesk.com' }), connecting: true });
  expect(screen.getByLabelText('Freshdesk address')).toHaveValue('beta.freshdesk.com');
});

test('a finished import says where the old help center addresses and links lead now', () => {
  const links = {
    status: 'UNVERIFIED', detail: ['1 Freshdesk link(s) were left as written; they stop working when the Freshdesk account closes.'],
    help_center: 'https://acme.help.example.net',
    old_addresses: { found: 36, redirected: 36, languages: ['en', 'es'], unmapped: [] },
    rewritten: { articles: 2, saved_replies: 1, links: 5 },
    left: [{ entity: 'kb_article', freshdesk_id: '101', url: 'https://acme.freshdesk.com/support/solutions/articles/999-gone', reason: 'not_imported' }],
  };
  page({ job: job({ state: 'finished', phase: 'full', status: 'OK', has_report: true }), report: { status: 'OK', links } });
  const section = screen.getByRole('region', { name: 'Old help center links' });
  expect(section).toHaveTextContent('36 of 36 old addresses lead to their new page on https://acme.help.example.net.');
  expect(section).toHaveTextContent('5 Freshdesk links in 2 articles and 1 saved reply now point at the help center.');
  expect(section).toHaveTextContent('1 Freshdesk link(s) were left as written');
  expect(within(section).getByText('Needs a look')).toBeInTheDocument();
});

test('an import without a knowledge base shows no links section', () => {
  page({ job: job({ state: 'finished', phase: 'full', status: 'OK', has_report: true }), report: { status: 'OK' } });
  expect(screen.queryByRole('region', { name: 'Old help center links' })).not.toBeInTheDocument();
});

test('the undo question is brought into view with the safe answer focused', () => {
  page({ job: job({ state: 'finished', phase: 'full', status: 'OK', has_report: true }) });
  fireEvent.click(screen.getByRole('button', { name: 'Undo import' }));
  expect(screen.getByRole('button', { name: 'Keep' })).toHaveFocus();
});

test('after an undo the page offers a new import, and a failed undo can be tried again', () => {
  const undone = job({ id: '33333333-3333-4333-8333-333333333333', kind: 'undo', undo_of: job().id, state: 'finished', phase: 'undo', has_report: true });
  const { onNewImport } = page({ job: undone, undoReport: { conversations: 300, contacts: 120 }, history: [undone, job({ state: 'finished', status: 'OK', phase: 'full' })] });
  expect(screen.getByRole('status')).toHaveTextContent('300 conversations and 120 contacts');
  fireEvent.click(screen.getByRole('button', { name: 'New import' }));
  expect(onNewImport).toHaveBeenCalled();
  expect(within(screen.getByRole('region', { name: 'Earlier imports' })).getByRole('list')).toHaveTextContent('Removed');
});

test('a failed undo can be tried again', () => {
  const { onUndo } = page({ job: job({ id: '33333333-3333-4333-8333-333333333333', kind: 'undo', undo_of: job().id, state: 'failed', phase: 'undo' }) });
  fireEvent.click(screen.getByRole('button', { name: 'Try undo again' }));
  expect(onUndo).toHaveBeenCalledWith(job().id);
});

test('an import refused for storage says what to do and leads to the plan on the hosted cloud', () => {
  page({ showBilling: true, job: job({ state: 'failed', phase: 'full', error_code: 'storage_cap', has_interim_report: true }) });
  expect(screen.getByRole('alert')).toHaveTextContent('Move to a plan with more storage');
  expect(within(screen.getByRole('main')).getAllByRole('link', { name: 'Plan and billing' }).at(-1)).toHaveAttribute('href', '/settings/billing');
});

test('a recent pass that found nothing says so instead of sending the team to an empty inbox', () => {
  page({ report: { status: 'INTERIM', interim: true, tickets: { source: 0, imported: 0, failed: 0 } },
    job: job({ state: 'running', phase: 'full', has_interim_report: true, progress: { step: 'listing tickets', done: 0, total: 0, api_calls: 9, updated_at: '2026-10-22T08:05:00Z' } }) });
  expect(screen.getByRole('status')).toHaveTextContent('No ticket changed in the last 90 days');
  expect(screen.queryByRole('link', { name: 'Open the inbox' })).not.toBeInTheDocument();
});

test('an import stopped before any ticket still offers its undo: contacts and saved replies come first', () => {
  page({ job: job({ state: 'cancelled', phase: 'full', imported_conversations: 0 }) });
  expect(screen.getByRole('button', { name: 'Undo import' })).toBeInTheDocument();
  expect(screen.queryByText(/conversations from this account are in your inbox/)).not.toBeInTheDocument();
});

test('a wrong address is pointed at on its field', () => {
  page();
  fireEvent.change(screen.getByLabelText('Freshdesk address'), { target: { value: 'acme.zendesk.com' } });
  fireEvent.click(screen.getByRole('button', { name: 'Read account' }));
  const field = screen.getByLabelText('Freshdesk address');
  expect(field).toHaveAttribute('aria-invalid', 'true');
  expect(field).toHaveFocus();
  expect(field).toHaveAccessibleDescription(/<account>.freshdesk.com/);
  fireEvent.change(field, { target: { value: 'acme.freshdesk.com' } });
  fireEvent.click(screen.getByRole('button', { name: 'Read account' }));
  expect(screen.getByLabelText('API key of a Freshdesk admin')).toHaveAttribute('aria-invalid', 'true');
});
