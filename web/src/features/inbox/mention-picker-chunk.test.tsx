import { fireEvent, render, screen, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import type { TeamMember } from '../../api/schemas';
import { askableTeammates } from './mention-match';
import { ReplyComposer } from './reply-composer';

const member = (id: string, display_name: string, email: string): TeamMember => ({
  id, email, display_name, roles: [{ id: '00000000-0000-4000-8000-000000000001', name: 'Agent', system: 'agent' }], manageable: false,
  timezone_override: null, deactivated_at: null, last_active_at: null, open_conversations: 0,
});
const me = member('10000000-0000-4000-8000-000000000001', 'Hoàng Tú', 'tu@northwind.example');
const long = member('10000000-0000-4000-8000-000000000002', 'Long Bui', 'long@northwind.example');

// The @ list loads with the reply pickers' chunk, not with the page. This file starts without that chunk: the first @
// typed right after choosing Private note still opens the list, with no second key needed.
test('the @ list opens on the first @ typed after switching to Private note, before its chunk was there', async () => {
  const onSubmit = vi.fn();
  render(<ReplyComposer onSubmit={onSubmit} teammates={askableTeammates([me, long], me.id)} />);
  fireEvent.click(screen.getByRole('button', { name: /Private note/ }));
  const box = screen.getByRole('textbox', { name: 'Message body' });
  fireEvent.change(box, { target: { value: '@Lo', selectionStart: 3, selectionEnd: 3 } });
  const list = await screen.findByRole('listbox', { name: 'Told in Mentions and by email' });
  expect(within(list).getByRole('option', { selected: true })).toHaveTextContent('Long Bui');
  fireEvent.keyDown(box, { key: 'Enter' });
  expect(box).toHaveValue('@Long Bui ');
  fireEvent.click(screen.getByRole('button', { name: /Add private note/ }));
  expect(onSubmit.mock.lastCall?.[7]).toEqual([long.id]);
});
