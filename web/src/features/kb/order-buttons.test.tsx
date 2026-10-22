import { useState } from 'react';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { OrderAnnouncement, OrderButtons, OrderGoneError, moveInOrder, sendMove } from './order-buttons';
import { json, serve } from './kb.test.fixture';

afterEach(() => { vi.unstubAllGlobals(); });

const rows = [{ id: 'a', name: 'Alpha' }, { id: 'b', name: 'Bravo' }, { id: 'c', name: 'Charlie' }];

// List is a list ordered with the buttons, as the knowledge base page shows one.
function List({ onMove, more = false }: { onMove: (ids: string[], step: number) => Promise<unknown>; more?: boolean }) {
  const [ids, setIds] = useState(rows.map((row) => row.id));
  const [message, setMessage] = useState('');
  return (
    <>
      <ul>{ids.map((id) => (
        <li key={id}>{rows.find((row) => row.id === id)!.name}
          <OrderButtons ids={ids} id={id} name={rows.find((row) => row.id === id)!.name} more={more} onAnnounce={setMessage}
            onMove={async (next, step) => { await onMove(next, step); setIds(next); }} /></li>
      ))}</ul>
      <OrderAnnouncement message={message} />
    </>
  );
}

test('moveInOrder swaps an item with its neighbour and leaves the ends where they are', () => {
  expect(moveInOrder(['a', 'b', 'c'], 'b', -1)).toEqual(['b', 'a', 'c']);
  expect(moveInOrder(['a', 'b', 'c'], 'b', 1)).toEqual(['a', 'c', 'b']);
  expect(moveInOrder(['a', 'b', 'c'], 'a', -1)).toEqual(['a', 'b', 'c']);
  expect(moveInOrder(['a', 'b', 'c'], 'c', 1)).toEqual(['a', 'b', 'c']);
  expect(moveInOrder(['a', 'b'], 'x', 1)).toEqual(['a', 'b']);
});

test('the first row has no Move up and the last no Move down, unless more rows follow', () => {
  const view = render(<List onMove={() => Promise.resolve()} />);
  expect(screen.queryByRole('button', { name: 'Move Alpha up' })).not.toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Move Alpha down' })).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Move Bravo up' })).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Move Charlie down' })).not.toBeInTheDocument();
  view.unmount();
  render(<List onMove={() => Promise.resolve()} more />);
  expect(screen.getByRole('button', { name: 'Move Charlie down' })).toBeInTheDocument();
});

test('a click sends the new order, says where the row went and keeps the focus on the row', async () => {
  const onMove = vi.fn((_next: string[], _step: number) => Promise.resolve());
  render(<List onMove={onMove} />);
  const down = screen.getByRole('button', { name: 'Move Alpha down' });
  down.focus();
  fireEvent.click(down);
  await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('Moved Alpha to place 2 of 3.'));
  expect(onMove).toHaveBeenCalledWith(['b', 'a', 'c'], 1);
  expect(screen.getByRole('button', { name: 'Move Alpha down' })).toHaveFocus();
  // At the end the button it used is gone: the focus goes to the other one.
  fireEvent.click(screen.getByRole('button', { name: 'Move Alpha down' }));
  await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('Moved Alpha to place 3 of 3.'));
  expect(screen.queryByRole('button', { name: 'Move Alpha down' })).not.toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Move Alpha up' })).toHaveFocus();
});

test('a failed move says so and changes nothing', async () => {
  render(<List onMove={() => Promise.reject(new Error('API 500'))} />);
  fireEvent.click(screen.getByRole('button', { name: 'Move Bravo up' }));
  await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('Bravo could not be moved. Try again.'));
  expect(screen.getAllByRole('listitem').map((item) => item.firstChild?.textContent)).toEqual(['Alpha', 'Bravo', 'Charlie']);
});

test('a second click while a move is sent waits instead of sending again', async () => {
  let finish = () => {};
  const onMove = vi.fn(() => new Promise<void>((resolve) => { finish = resolve; }));
  render(<List onMove={onMove} />);
  const up = screen.getByRole('button', { name: 'Move Charlie up' });
  fireEvent.click(up);
  expect(up).toHaveAttribute('aria-disabled', 'true');
  fireEvent.click(up);
  expect(onMove).toHaveBeenCalledTimes(1);
  await act(async () => { finish(); });
});

// Ids as the server sends them: the answer is checked against the API's schema.
const [a, b, c, k, n, parent] = ['a', 'b', 'c', 'd', 'e', 'f'].map((letter) => `${letter.repeat(8)}-0000-4000-8000-000000000000`);

test('a stale order is moved again in the lists the server has and sent once more', async () => {
  const sent: unknown[] = [];
  const calls = serve({
    'PUT /api/v1/kb/order': (call) => {
      sent.push(call.body);
      return sent.length === 1
        ? json(409, { error: { code: 'order_stale', message: 'Something was added.', collection_ids: [k], article_ids: [a, n, b, c] } })
        : json(200, call.body);
    },
  });
  expect(await sendMove(parent, { collection_ids: [], article_ids: [a, b, c] }, 'article_ids', b, -1)).toEqual([a, b, n, c]);
  expect(calls).toHaveLength(2);
  expect(sent[0]).toEqual({ parent_id: parent, collection_ids: [], article_ids: [b, a, c] });
  expect(sent[1]).toEqual({ parent_id: parent, collection_ids: [k], article_ids: [a, b, n, c] });
});

test('a refusal other than a stale order is not sent again', async () => {
  const calls = serve({ 'PUT /api/v1/kb/order': () => json(404, { error: { code: 'not_found', message: 'The parent collection was not found.' } }) });
  await expect(sendMove(parent, { collection_ids: [], article_ids: [a, b] }, 'article_ids', a, 1)).rejects.toThrow();
  expect(calls).toHaveLength(1);
});

test('a row someone else moved away or deleted is not sent again, and the move says so', async () => {
  const calls = serve({
    'PUT /api/v1/kb/order': () => json(409, { error: { code: 'order_stale', message: 'Something moved.', collection_ids: [], article_ids: [a, c] } }),
  });
  await expect(sendMove(parent, { collection_ids: [], article_ids: [a, b, c] }, 'article_ids', b, -1)).rejects.toBeInstanceOf(OrderGoneError);
  expect(calls).toHaveLength(1);
  // A list read without the row never goes out at all.
  await expect(sendMove(parent, { collection_ids: [], article_ids: [a, c] }, 'article_ids', b, 1)).rejects.toBeInstanceOf(OrderGoneError);
  expect(calls).toHaveLength(1);
});

test('a move whose row is gone reads out why instead of a place', async () => {
  render(<List onMove={() => Promise.reject(new OrderGoneError())} />);
  fireEvent.click(screen.getByRole('button', { name: 'Move Bravo up' }));
  await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('Bravo is no longer in this list: someone else moved or deleted it.'));
  expect(screen.getByRole('status')).not.toHaveTextContent('place 0');
});
