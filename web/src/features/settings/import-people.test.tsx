import { fireEvent, render, screen, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import type { ImportPerson } from '../../api/imports';
import type { Invite, TeamMember } from '../../api/schemas';
import { ImportPeople } from './import-people';

const person = (change: Partial<ImportPerson>): ImportPerson => ({
  external_id: '101', name: 'Alice', email: 'alice@acme.example', active: true, linked_agent_id: null, linked_by_email: false, pending: false,
  updated_at: '2026-10-22T08:00:00Z', ...change,
});
const sam: TeamMember = { id: '33333333-3333-4333-8333-333333333333', email: 'sam@acme.example', display_name: 'Sam', role: 'admin', timezone_override: null,
  deactivated_at: null, last_active_at: null, open_conversations: 0 };
const invite: Invite = { id: '44444444-4444-4444-8444-444444444444', email: 'carol@acme.example', role_ceiling: 'agent', created_by_email: 'sam@acme.example',
  created_at: '2026-10-22T08:00:00Z', expires_at: '2099-01-01T00:00:00Z', used_at: null, revoked_at: null };

function show(people: ImportPerson[]) {
  const handlers = { onInvite: vi.fn(), onLink: vi.fn(), onUnlink: vi.fn() };
  render(<ImportPeople people={people} agents={[sam]} invites={[invite]} {...handlers} />);
  return handlers;
}

test('each Freshdesk agent says whether an agent here takes over their replies, and offers the next step', () => {
  const { onInvite, onLink, onUnlink } = show([
    person({ external_id: '101', name: 'Sam F', email: 'sam@acme.example', linked_agent_id: sam.id, linked_by_email: true }),
    person({ external_id: '102', name: 'Bob', email: 'bob@old.example', active: false }),
    person({ external_id: '103', name: 'Carol', email: 'carol@acme.example' }),
    person({ external_id: '104', name: 'Dan', email: 'dan@acme.example', linked_agent_id: sam.id, pending: true }),
  ]);
  const [samRow, bob, carol, dan] = screen.getAllByRole('listitem');
  expect(samRow).toHaveTextContent('Linked to Sam');
  fireEvent.click(within(samRow).getByRole('button', { name: 'Unlink' }));
  expect(onUnlink).toHaveBeenCalledWith('101');
  expect(bob).toHaveTextContent('Inactive in Freshdesk');
  fireEvent.click(within(bob).getByRole('button', { name: 'Invite' }));
  expect(onInvite).toHaveBeenCalledWith('bob@old.example');
  expect(within(bob).getByRole('button', { name: 'Link' })).toBeDisabled();
  fireEvent.change(within(bob).getByRole('combobox', { name: 'Link Bob to' }), { target: { value: sam.id } });
  fireEvent.click(within(bob).getByRole('button', { name: 'Link' }));
  expect(onLink).toHaveBeenCalledWith('102', sam.id);
  expect(carol).toHaveTextContent('Invited');
  expect(within(carol).queryByRole('button', { name: 'Invite' })).not.toBeInTheDocument();
  expect(dan).toHaveTextContent('Linking…');
});
