import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { ContactRoute } from './contact-route';

const api = vi.hoisted(() => ({ me: vi.fn(), contact: vi.fn(), conversations: vi.fn(), undoMerge: vi.fn(), contacts: vi.fn() }));
vi.mock('./contact-api', async (importOriginal) => ({ ...(await importOriginal<typeof import('./contact-api')>()), contactApi: api }));
vi.mock('../../api/client', async (importOriginal) => ({ ...(await importOriginal<typeof import('../../api/client')>()), api }));
vi.mock('@tanstack/react-router', () => ({ useNavigate: () => vi.fn(), useParams: () => ({ contactId: '11111111-1111-4111-8111-111111111111' }) }));

const contact = (count: number, merges: unknown[] = []) => ({
  id: '11111111-1111-4111-8111-111111111111', name: 'Dana Work', email: 'dana@work.example', organization: null, merged_into_id: null,
  created_at: '2026-01-02T00:00:00Z', updated_at: '2026-01-02T00:00:00Z', conversation_count: count, merged_into: null, merges,
});
const row = (n: number) => ({
  id: `00000000-0000-4000-8000-${String(n).padStart(12, '0')}`, contact_id: contact(0).id, contact_name: 'Dana Work', subject: `Question ${n}`,
  status: 'open', priority: 'normal', assignee_id: null, tags: [], origin: null, created_at: '2026-01-02T00:00:00Z', updated_at: '2026-01-02T00:00:00Z',
});

// An undo that leaves fewer pages than the one on screen must not strand the person on an empty page.
test('the history page stays within the pages the customer has after an undo', async () => {
  const merge = { id: '22222222-2222-4222-8222-222222222222', contact_id: contact(0).id,
    merged_contact: { ...contact(0), id: '33333333-3333-4333-8333-333333333333', name: 'Dana Home', email: 'dana@home.example', merged_into_id: contact(0).id },
    conversation_ids: [], merged_by_agent_id: null, merged_at: '2026-02-01T00:00:00Z', undone_at: null };
  api.me.mockResolvedValue({ timezone: 'UTC' });
  api.contact.mockResolvedValue(contact(30, [merge]));
  api.conversations.mockImplementation(async (query: string) => {
    const page = Number(new URLSearchParams(query).get('page'));
    return { items: page === 1 ? [row(1)] : [row(26)], page, page_size: 25 };
  });
  api.undoMerge.mockImplementation(async () => { api.contact.mockResolvedValue(contact(10)); return { ...merge, undone_at: '2026-02-02T00:00:00Z' }; });
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><ContactRoute /></QueryClientProvider>);
  fireEvent.click(await screen.findByRole('button', { name: 'Next page' }));
  expect(await screen.findByText('Question 26')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Undo merge of dana@home.example' }));
  expect(await screen.findByText('Question 1')).toBeInTheDocument();
  await waitFor(() => expect(api.conversations).toHaveBeenLastCalledWith(expect.stringContaining('page=1')));
});
