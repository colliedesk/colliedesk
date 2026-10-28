import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeAll, expect, test, vi } from 'vitest';
import type { SavedReply } from '../../api/schemas';
import { presenceViewPart } from './presence-part';

// The moment before the lists' chunk is there: the composer is a fresh module in each test, so its chunk
// starts unloaded, and the chunk itself is held back or refused.
afterEach(() => {
  vi.doUnmock('./reply-pickers');
  vi.resetModules();
});

const refund: SavedReply = {
  id: '22222222-2222-4222-8222-222222222222', title: 'Refund steps', body: 'We refunded you.',
  created_by_agent_id: null, created_at: '2026-10-15T00:00:00Z', updated_at: '2026-10-15T00:00:00Z', last_change: null,
};


// The presence chunk loads when somebody else is on a conversation, a reply is refused or a notice shows;
// start with it there, so no test ends while it is still on its way (vitest refuses a module asked for
// after the test environment is gone).
beforeAll(async () => {
  presenceViewPart.preload();
  await import('./presence-view');
});

test('a list opened before its chunk arrived says it is loading where the list will be, then the list takes its place', async () => {
  let release: () => void = () => {};
  const held = new Promise<void>((resolve) => { release = resolve; });
  vi.doMock('./reply-pickers', async () => {
    await held;
    return vi.importActual('./reply-pickers');
  });
  const { ReplyComposer } = await import('./reply-composer');
  render(<ReplyComposer savedReplies={[refund]} />);
  fireEvent.click(screen.getByRole('button', { name: 'Insert saved reply' }));
  expect(screen.getByRole('status')).toHaveTextContent('Loading…');
  expect(screen.getByRole('status').closest('.saved-reply-picker')).not.toBeNull();
  release();
  const search = await screen.findByRole('combobox', { name: 'Search saved replies' });
  await waitFor(() => expect(search).toHaveFocus());
  expect(screen.getByRole('option', { name: 'Refund steps' })).toBeInTheDocument();
  expect(screen.queryByText('Loading…')).not.toBeInTheDocument();
});

test('a chunk that cannot be loaded says so and offers a reload; the draft stays in the box', async () => {
  vi.spyOn(console, 'warn').mockImplementation(() => {});
  vi.doMock('./reply-pickers', () => Promise.reject(new Error('chunk gone after a deploy')));
  const { ReplyComposer } = await import('./reply-composer');
  render(<ReplyComposer savedReplies={[refund]} />);
  const body = screen.getByLabelText('Message body') as HTMLTextAreaElement;
  fireEvent.change(body, { target: { value: 'My draft' } });
  fireEvent.click(screen.getByRole('button', { name: 'Articles' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('This part of the page could not be loaded.');
  expect(screen.getByRole('button', { name: 'Reload the page' })).toBeInTheDocument();
  expect(body.value).toBe('My draft');
});
