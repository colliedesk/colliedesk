import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import type { ReactNode } from 'react';
import { expect, test, vi } from 'vitest';
import type { ContactDetail, Conversation } from '../../api/schemas';
import { ContactPage } from './contact-page';

const api = vi.hoisted(() => ({ contacts: vi.fn() }));
vi.mock('./contact-api', async (importOriginal) => ({ ...(await importOriginal<typeof import('./contact-api')>()), contactApi: { searchContacts: api.contacts } }));

const wrap = (node: ReactNode) => render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{node}</QueryClientProvider>);

const contact = (overrides: Partial<ContactDetail> = {}): ContactDetail => ({
  id: '11111111-1111-4111-8111-111111111111', name: 'Mai Linh', email: 'mai@customer.example', organization: 'Acme Ltd', merged_into_id: null,
  created_at: '2026-01-02T00:00:00Z', updated_at: '2026-01-02T00:00:00Z', conversation_count: 2, merged_into: null, merges: [], ...overrides,
});
const duplicate = {
  id: '22222222-2222-4222-8222-222222222222', name: 'Mai at home', email: 'mai@home.example', organization: null, merged_into_id: null,
  created_at: '2026-01-03T00:00:00Z', updated_at: '2026-01-03T00:00:00Z',
};
const row = (id: string, subject: string, imported = false): Conversation => ({ latest_reply_number: 0,
  id, contact_id: contact().id, contact_name: 'Mai Linh', subject, status: 'closed', priority: 'normal', assignee_id: null, tags: [], channel: imported ? 'imported' : 'email',
  origin: imported ? { system: 'freshdesk', external_id: '7', type: null, source: null, due_at: null, first_response_due_at: null, cc: [], forwarded_to: [] } : null,
  created_at: '2026-01-02T00:00:00Z', updated_at: '2026-01-02T00:00:00Z',
});

test('the contact page shows who the customer is and every conversation, imported ones marked, each opening its conversation', () => {
  const onOpenConversation = vi.fn();
  wrap(<ContactPage contact={contact()} conversations={[row('c1', 'Refund'), row('c2', 'Old ticket', true)]} page={1} pageSize={25} onPage={vi.fn()} onOpenConversation={onOpenConversation} />);
  expect(screen.getByRole('heading', { level: 1, name: 'Mai Linh' })).toBeInTheDocument();
  expect(screen.getByText('mai@customer.example')).toBeInTheDocument();
  expect(screen.getByText('Acme Ltd')).toBeInTheDocument();
  expect(screen.getByText(/2 conversations, any status, imported ones included/)).toBeInTheDocument();
  const old = screen.getByRole('link', { name: /Old ticket/ });
  expect(old).toHaveAttribute('href', '/conversations/c2');
  expect(within(old).getByText('Imported')).toBeInTheDocument();
  fireEvent.click(old);
  expect(onOpenConversation).toHaveBeenCalledWith('c2');
});

test('the history comes in pages counted from the total', () => {
  const onPage = vi.fn();
  wrap(<ContactPage contact={contact({ conversation_count: 60 })} conversations={[row('c1', 'One')]} page={2} pageSize={25} onPage={onPage} />);
  expect(screen.getByText('Page 2 of 3')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Next page' }));
  expect(onPage).toHaveBeenCalledWith(3);
  // The new page is read from its first row: the heading above the list has the focus.
  expect(screen.getByRole('heading', { name: 'Conversations' })).toHaveFocus();
});

test('a rename that fails keeps the form open with the reason, and one that works closes it', async () => {
  const onRename = vi.fn().mockRejectedValueOnce(new Error('API 500')).mockResolvedValueOnce(undefined);
  wrap(<ContactPage contact={contact()} page={1} pageSize={25} onPage={vi.fn()} onRename={onRename} />);
  fireEvent.click(screen.getByRole('button', { name: 'Rename' }));
  fireEvent.change(screen.getByLabelText('Name'), { target: { value: '  Mai Linh Tran ' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save name' }));
  expect(await screen.findByRole('alert')).toHaveTextContent(/could not be saved/);
  expect(onRename).toHaveBeenCalledWith('Mai Linh Tran');
  fireEvent.click(screen.getByRole('button', { name: 'Save name' }));
  await waitFor(() => expect(screen.queryByLabelText('Name')).not.toBeInTheDocument());
  await waitFor(() => expect(screen.getByRole('button', { name: 'Rename' })).toHaveFocus());
  expect(screen.getByText('Name saved.')).toBeInTheDocument();
});

test('merging asks for the duplicate, says where replies will go, and only then merges', async () => {
  api.contacts.mockResolvedValue({ items: [contact(), duplicate], has_more: false });
  const onMerge = vi.fn().mockResolvedValue(undefined);
  wrap(<ContactPage contact={contact()} page={1} pageSize={25} onPage={vi.fn()} onMerge={onMerge} />);
  fireEvent.click(screen.getByRole('button', { name: 'Merge a duplicate…' }));
  const dialog = screen.getByRole('dialog', { name: 'Merge a duplicate into Mai Linh' });
  fireEvent.change(within(dialog).getByLabelText('Search contacts by name or email'), { target: { value: 'mai' } });
  // The contact itself is never offered as its own duplicate.
  const pick = await within(dialog).findByRole('option', { name: /Mai at home/ });
  expect(within(dialog).queryByRole('option', { name: /mai@customer.example/ })).not.toBeInTheDocument();
  fireEvent.click(pick);
  expect(dialog).toHaveTextContent('replies on those conversations go to mai@customer.example');
  expect(onMerge).not.toHaveBeenCalled();
  fireEvent.click(within(dialog).getByRole('button', { name: 'Merge' }));
  await waitFor(() => expect(onMerge).toHaveBeenCalledWith(duplicate));
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  await waitFor(() => expect(screen.getByRole('button', { name: 'Merge a duplicate…' })).toHaveFocus());
  expect(screen.getByText('Merged mai@home.example into this customer.')).toBeInTheDocument();
});

test('a merge is listed with what it moved and can be undone; a merged contact points to the one it joined', async () => {
  const onUndoMerge = vi.fn().mockResolvedValue(undefined);
  const merge = { id: '33333333-3333-4333-8333-333333333333', contact_id: contact().id, merged_contact: { ...duplicate, merged_into_id: contact().id },
    conversation_ids: ['c9'], merged_by_agent_id: null, merged_at: '2026-02-01T00:00:00Z', undone_at: null };
  const { unmount } = wrap(<ContactPage contact={contact({ merges: [merge] })} page={1} pageSize={25} onPage={vi.fn()} onUndoMerge={onUndoMerge} onMerge={vi.fn()} />);
  expect(screen.getByText(/mai@home.example: 1 conversation moved/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Undo merge of mai@home.example' }));
  await waitFor(() => expect(onUndoMerge).toHaveBeenCalledWith(merge));
  // The undone row is gone with its button: the merge button takes the focus, and the change is announced.
  await waitFor(() => expect(screen.getByRole('button', { name: 'Merge a duplicate…' })).toHaveFocus());
  expect(screen.getByText('Merge of mai@home.example undone.')).toBeInTheDocument();
  unmount();

  const onOpenContact = vi.fn();
  wrap(<ContactPage contact={contact({ id: duplicate.id, name: duplicate.name, email: duplicate.email, merged_into_id: contact().id, merged_into: contact(), conversation_count: 0 })}
    page={1} pageSize={25} onPage={vi.fn()} onRename={vi.fn()} onMerge={vi.fn()} onOpenContact={onOpenContact} />);
  expect(screen.getByText(/merged into Mai Linh \(mai@customer.example\)/)).toBeInTheDocument();
  // Its conversations are with the contact it joined: no history of its own that would read "none".
  expect(screen.queryByRole('heading', { name: 'Conversations' })).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Rename' })).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Merge a duplicate…' })).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole('link', { name: 'Open that customer' }));
  expect(onOpenContact).toHaveBeenCalledWith(contact().id);
});

test('a click on the merge window\'s backdrop keeps the focus in it, so Escape closes it back to its button', async () => {
  wrap(<ContactPage contact={contact()} page={1} pageSize={25} onPage={vi.fn()} onMerge={vi.fn()} />);
  const opener = screen.getByRole('button', { name: 'Merge a duplicate…' });
  opener.focus();
  fireEvent.click(opener);
  const dialog = screen.getByRole('dialog', { name: 'Merge a duplicate into Mai Linh' });
  expect(fireEvent.mouseDown(dialog.parentElement!)).toBe(false);
  fireEvent.keyDown(dialog, { key: 'Escape' });
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  expect(opener).toHaveFocus();
});
