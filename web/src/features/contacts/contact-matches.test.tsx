import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { ContactMatches } from './contact-matches';

const api = vi.hoisted(() => ({ contacts: vi.fn() }));
vi.mock('./contact-api', async (importOriginal) => ({ ...(await importOriginal<typeof import('./contact-api')>()), contactApi: { searchContacts: api.contacts } }));

const mai = {
  id: '22222222-2222-4222-8222-222222222222', name: 'Mai Linh', email: 'mai@customer.example', organization: null, merged_into_id: null,
  created_at: '2026-01-02T00:00:00Z', updated_at: '2026-01-02T00:00:00Z',
};

test('customers a search finds are listed, each opening their page', async () => {
  api.contacts.mockResolvedValue({ items: [mai], has_more: false });
  const onOpenContact = vi.fn();
  render(<QueryClientProvider client={new QueryClient()}><ContactMatches search=" mai " onOpenContact={onOpenContact} /></QueryClientProvider>);
  const found = await screen.findByRole('navigation', { name: 'Customers' });
  expect(api.contacts).toHaveBeenCalledWith('mai', 5, expect.anything());
  const link = within(found).getByRole('link', { name: /mai@customer.example/ });
  expect(link).toHaveAttribute('href', `/contacts/${mai.id}`);
  fireEvent.click(link);
  expect(onOpenContact).toHaveBeenCalledWith(mai.id);
});

test('no search, or nobody found, shows nothing', async () => {
  api.contacts.mockReset();
  const { container } = render(<QueryClientProvider client={new QueryClient()}><ContactMatches search="  " onOpenContact={vi.fn()} /></QueryClientProvider>);
  expect(container).toBeEmptyDOMElement();
  expect(api.contacts).not.toHaveBeenCalled();
});
