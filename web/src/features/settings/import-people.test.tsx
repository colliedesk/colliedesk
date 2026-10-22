import { fireEvent, render, screen, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import type { ImportPerson } from '../../api/imports';
import type { Invite, TeamMember } from '../../api/schemas';
import { ImportPeople } from './import-people';
import type { ImportPeopleProps } from './import-people';

const person = (change: Partial<ImportPerson>): ImportPerson => ({
  external_id: '101', name: 'Alice', email: 'alice@acme.example', active: true, linked_agent_id: null, linked_by_email: false, pending: false,
  updated_at: '2026-10-22T08:00:00Z', ...change,
});
const sam: TeamMember = { id: '33333333-3333-4333-8333-333333333333', email: 'sam@acme.example', display_name: 'Sam', role: 'admin', timezone_override: null,
  deactivated_at: null, last_active_at: null, open_conversations: 0 };
const invite: Invite = { id: '44444444-4444-4444-8444-444444444444', email: 'carol@acme.example', role_ceiling: 'agent', created_by_email: 'sam@acme.example',
  created_at: '2026-10-22T08:00:00Z', expires_at: '2099-01-01T00:00:00Z', used_at: null, revoked_at: null };

function show(people: ImportPerson[], props: Partial<ImportPeopleProps> = {}) {
  const handlers = { onInvite: vi.fn(), onLink: vi.fn(), onUnlink: vi.fn(), onRevokeInvite: vi.fn(), onRetry: vi.fn() };
  render(<ImportPeople people={people} agents={[sam]} invites={[invite]} imported timezone="UTC" {...handlers} {...props} />);
  return handlers;
}

test('each Freshdesk agent says whether an agent here takes over their replies, and offers the next step', () => {
  const { onInvite, onLink, onUnlink, onRevokeInvite } = show([
    person({ external_id: '101', name: 'Sam F', email: 'sam@acme.example', linked_agent_id: sam.id, linked_by_email: true }),
    person({ external_id: '102', name: 'Bob', email: 'bob@old.example', active: false }),
    person({ external_id: '103', name: 'Carol', email: 'carol@acme.example' }),
    person({ external_id: '104', name: 'Dan', email: 'dan@acme.example', linked_agent_id: sam.id, pending: true }),
  ]);
  const [samRow, bob, carol, dan] = screen.getAllByRole('listitem');
  expect(samRow).toHaveTextContent('Linked to Sam');
  fireEvent.click(within(samRow).getByRole('button', { name: 'Unlink Sam F' }));
  expect(onUnlink).toHaveBeenCalledWith('101');
  expect(bob).toHaveTextContent('Inactive in Freshdesk');
  fireEvent.click(within(bob).getByRole('button', { name: 'Invite Bob' }));
  expect(onInvite).toHaveBeenCalledWith('bob@old.example');
  expect(within(bob).getByRole('button', { name: 'Link Bob to the chosen agent' })).toBeDisabled();
  fireEvent.change(within(bob).getByRole('combobox', { name: 'Link Bob to' }), { target: { value: sam.id } });
  fireEvent.click(within(bob).getByRole('button', { name: 'Link Bob to the chosen agent' }));
  expect(onLink).toHaveBeenCalledWith('102', sam.id);
  expect(carol).toHaveTextContent('Invited');
  expect(within(carol).queryByRole('button', { name: /^Invite/ })).not.toBeInTheDocument();
  fireEvent.click(within(carol).getByRole('button', { name: 'Revoke the invite of Carol' }));
  expect(onRevokeInvite).toHaveBeenCalledWith(invite.id);
  expect(dan).toHaveTextContent('Linked to Sam');
  expect(dan).toHaveTextContent('Moving their imported replies');
  expect(within(dan).getByRole('status')).toBeInTheDocument();
});

test('the link of an invite just made shows once in the row of that person, to copy and send', () => {
  show([person({ external_id: '102', name: 'Bob', email: 'bob@old.example' })],
    { createdInvite: { email: 'bob@old.example', url: 'https://desk.example.com/join?token=abc', expiresAt: '2026-10-22T08:15:00Z' } });
  expect(screen.getByLabelText('One-time invite link')).toHaveValue('https://desk.example.com/join?token=abc');
  expect(screen.getByRole('button', { name: 'Copy link' })).toBeInTheDocument();
  expect(screen.getByText(/works once, until/)).toBeInTheDocument();
});

test('before the import a link has nothing to move, so it shows as linked', () => {
  show([person({ linked_agent_id: sam.id, linked_by_email: true, pending: true })], { imported: false });
  expect(screen.getByRole('status')).toHaveTextContent('Linked to Sam');
  expect(screen.queryByText(/Moving/)).not.toBeInTheDocument();
});

test('agents that could not be loaded say so and can be asked again', () => {
  const { onRetry } = show([], { failed: true });
  expect(screen.getByRole('alert')).toHaveTextContent('could not be loaded');
  fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
  expect(onRetry).toHaveBeenCalled();
});
