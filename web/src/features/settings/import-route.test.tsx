import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, expect, test, vi } from 'vitest';
import { ImportRoute } from './import-route';

const { api, InvalidRequestError } = vi.hoisted(() => ({
  api: { me: vi.fn(), config: vi.fn(), agents: vi.fn(), invites: vi.fn(), createInvite: vi.fn(), revokeInvite: vi.fn() }, InvalidRequestError: class extends Error {},
}));
const imports = vi.hoisted(() => ({ list: vi.fn(), get: vi.fn(), preview: vi.fn(), start: vi.fn(), cancel: vi.fn(), setKey: vi.fn(), report: vi.fn(), undo: vi.fn(),
  people: vi.fn(), link: vi.fn(), unlink: vi.fn() }));
vi.mock('../../api/client', () => ({ api, InvalidRequestError }));
vi.mock('../../api/imports', async (original) => ({ ...(await original<typeof import('../../api/imports')>()), importsApi: imports }));

const previewed = {
  id: '11111111-1111-4111-8111-111111111111', kind: 'import', source: 'freshdesk', domain: 'acme.freshdesk.com', account_source: 'freshdesk:account:777',
  undo_of: null, state: 'previewed', phase: 'preview', options: {}, progress: null, status: null, error_code: null, error_message: null,
  preview: { domain: 'acme.freshdesk.com', account_id: 777, source: 'freshdesk:account:777', agents: [], companies: 1, contacts: 1, tickets_outside_archive: 1,
    highest_listed_ticket_id: 1, archive: 'readable', canned_responses: 'readable', knowledge_base: 'none', rate_limit_per_minute: 100, api_calls: 1, checked_at: '2026-10-22T08:00:00Z' },
  has_key: true, has_interim_report: false, has_report: false, cancel_requested: false, next_run_at: null, created_by: null,
  created_at: '2026-10-22T08:00:00Z', started_at: null, finished_at: null, updated_at: '2026-10-22T08:00:00Z',
};

beforeEach(() => {
  vi.clearAllMocks();
  api.me.mockResolvedValue({ agent: { id: 'a', email: 'sam@example.com', display_name: '', role: 'admin', timezone_override: null }, workspace: { id: 'w', slug: 's', name: 'S', timezone: 'UTC' }, timezone: 'UTC' });
  api.config.mockResolvedValue({ billing: { enabled: false } });
  imports.people.mockResolvedValue({ items: [] });
  api.agents.mockResolvedValue({ items: [] });
  api.invites.mockResolvedValue({ items: [] });
});

test('a later action clears what an earlier one said', async () => {
  imports.list.mockResolvedValue({ items: [previewed] });
  imports.start.mockRejectedValue(new InvalidRequestError('An import or undo is already queued'));
  imports.cancel.mockResolvedValue({ ...previewed, state: 'discarded' });
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><ImportRoute /></QueryClientProvider>);
  fireEvent.click(await screen.findByRole('button', { name: 'Start import' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('already queued');
  fireEvent.click(screen.getByRole('button', { name: 'Discard' }));
  await waitFor(() => expect(imports.cancel).toHaveBeenCalled());
  await waitFor(() => expect(screen.queryByText(/already queued/)).not.toBeInTheDocument());
});

test('discarding the preview just asked for gives the page back to the import before it', async () => {
  const done = { ...previewed, id: '22222222-2222-4222-8222-222222222222', state: 'finished', phase: 'full', status: 'OK', has_report: true, imported_conversations: 3 };
  const fresh = { ...previewed, imported_conversations: null };
  imports.list.mockResolvedValueOnce({ items: [done] }).mockResolvedValue({ items: [fresh, done] });
  imports.report.mockResolvedValue({ status: 'OK' });
  imports.preview.mockResolvedValue(fresh);
  imports.cancel.mockImplementation(async () => {
    imports.list.mockResolvedValue({ items: [{ ...fresh, state: 'discarded' }, done] });
    return { ...fresh, state: 'discarded' };
  });
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><ImportRoute /></QueryClientProvider>);
  fireEvent.click(await screen.findByRole('button', { name: 'New import' }));
  fireEvent.change(screen.getByLabelText('Freshdesk address'), { target: { value: 'acme.freshdesk.com' } });
  fireEvent.change(screen.getByLabelText('API key of a Freshdesk admin'), { target: { value: 'k' } });
  fireEvent.click(screen.getByRole('button', { name: 'Read account' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Discard' }));
  expect(await screen.findByRole('button', { name: 'Undo import' })).toBeInTheDocument();
});

test('the old help center address typed in the form goes with the preview', async () => {
  imports.list.mockResolvedValue({ items: [] });
  imports.preview.mockResolvedValue(previewed);
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><ImportRoute /></QueryClientProvider>);
  fireEvent.change(await screen.findByLabelText('Freshdesk address'), { target: { value: 'acme.freshdesk.com' } });
  fireEvent.change(screen.getByLabelText('API key of a Freshdesk admin'), { target: { value: 'k' } });
  fireEvent.change(screen.getByLabelText('Old help center address (optional)'), { target: { value: 'support.acme.com' } });
  fireEvent.click(screen.getByRole('button', { name: 'Read account' }));
  await waitFor(() => expect(imports.preview).toHaveBeenCalledWith('acme.freshdesk.com', 'k', expect.any(String), 'support.acme.com'));
});

test('a later people action clears what an earlier one said', async () => {
  const finished = { ...previewed, state: 'finished', phase: 'full', status: 'OK', has_report: true, imported_conversations: 1 };
  imports.list.mockResolvedValue({ items: [finished] });
  imports.report.mockResolvedValue({ status: 'OK' });
  imports.people.mockResolvedValue({ items: [
    { external_id: '102', name: 'Bob', email: 'bob@old.example', active: false, linked_agent_id: null, linked_by_email: false, pending: false, updated_at: '2026-10-22T08:00:00Z' }] });
  imports.link.mockResolvedValue({});
  const sam = { id: '33333333-3333-4333-8333-333333333333', email: 'sam@acme.example', display_name: 'Sam', role: 'admin', timezone_override: null,
    deactivated_at: null, last_active_at: null, open_conversations: 0 };
  api.agents.mockResolvedValue({ items: [sam] });
  api.invites.mockResolvedValue({ items: [] });
  api.createInvite.mockRejectedValue(new InvalidRequestError('No seat left for an invite'));
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><ImportRoute /></QueryClientProvider>);
  fireEvent.click(await screen.findByRole('button', { name: 'Invite Bob' }));
  expect(await screen.findByText('No seat left for an invite')).toBeInTheDocument();
  fireEvent.change(await screen.findByRole('combobox', { name: 'Link Bob to' }), { target: { value: sam.id } });
  fireEvent.click(screen.getByRole('button', { name: 'Link Bob to the chosen agent' }));
  await waitFor(() => expect(imports.link).toHaveBeenCalled());
  await waitFor(() => expect(screen.queryByText('No seat left for an invite')).not.toBeInTheDocument());
});
