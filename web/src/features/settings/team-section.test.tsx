import { fireEvent, render, screen, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import type { Role, TeamMember } from '../../api/schemas';
import { permissionsOf, roleOf, rolesOf } from '../../test-roles';
import { settingsMessages } from './settings-messages';
import { TeamSection } from './team-section';

const member = (overrides: Partial<TeamMember> & Pick<TeamMember, 'id' | 'email'>): TeamMember => ({
  display_name: '', roles: rolesOf('agent'), manageable: true, timezone_override: null, deactivated_at: null, last_active_at: null, open_conversations: 0, ...overrides,
});
const admin = member({ id: '11111111-1111-4111-8111-111111111111', email: 'admin@example.com', display_name: 'Ada Admin', roles: rolesOf('admin'), last_active_at: '2026-10-15T09:30:00Z' });
const ann = member({ id: '22222222-2222-4222-8222-222222222222', email: 'ann@example.com', open_conversations: 2 });
const bob = member({ id: '33333333-3333-4333-8333-333333333333', email: 'bob@example.com', display_name: 'Bob Builder' });
const cy = member({ id: '44444444-4444-4444-8444-444444444444', email: 'cy@example.com', deactivated_at: '2026-10-10T00:00:00Z', last_active_at: '2026-10-09T08:00:00Z' });

const row = (text: string) => screen.getByText(text).closest('article') as HTMLElement;

test('lists every member with role, state and last activity', () => {
  render(<TeamSection members={[admin, ann, bob, cy]} currentAgentId={admin.id} canManage={false} timezone="UTC" />);
  const team = screen.getByRole('region', { name: 'Team' });
  expect(within(team).getAllByRole('article')).toHaveLength(4);
  expect(row('Ada Admin')).toHaveTextContent('admin@example.com');
  expect(row('Ada Admin')).toHaveTextContent('Admin');
  expect(row('Ada Admin')).toHaveTextContent('Active');
  expect(row('Ada Admin')).toHaveTextContent('Oct 15');
  expect(row('ann@example.com')).toHaveTextContent('Never signed in');
  expect(row('cy@example.com')).toHaveTextContent('Deactivated');
});

test('an agent sees the team read-only', () => {
  render(<TeamSection members={[admin, ann, cy]} currentAgentId={ann.id} canManage={false} timezone="UTC" />);
  expect(screen.queryByRole('combobox')).not.toBeInTheDocument();
  expect(screen.queryByRole('button')).not.toBeInTheDocument();
});

const adminRole = roleOf(rolesOf('admin')[0], permissionsOf('admin'));
const agentRole = roleOf(rolesOf('agent')[0], permissionsOf('agent'));
const editor = roleOf({ id: '99999999-9999-4999-8999-999999999999', name: 'Editor', system: null }, ['kb_articles:read', 'workspace:export'], { tokens: [{ id: 't1', label: 'Claude on laptop', owner: 'ann@example.com', owner_id: '22222222-2222-4222-8222-222222222222' }] });

// A token may follow a role its person does not hold, while their roles cover it: an admin's token that
// follows Agent keeps working when Admin becomes Agent, and stops when the new roles leave part of it out.
test('a token following a role its person covers stops only when the new roles no longer cover it', () => {
  const agentWithToken = { ...agentRole, tokens: [{ id: 't2', label: 'Ops bot', owner: 'admin@example.com', owner_id: admin.id }] };
  const other = member({ id: '55555555-5555-4555-8555-555555555555', email: 'eve@example.com', roles: rolesOf('admin') });
  render(<TeamSection members={[admin, other]} currentAgentId={other.id} canManage roles={[adminRole, agentWithToken, editor]} myPermissions={permissionsOf('admin')} timezone="UTC" onSetRoles={vi.fn()} />);
  fireEvent.click(screen.getByRole('button', { name: 'Change roles: Ada Admin' }));
  const group = screen.getByRole('group', { name: 'Roles of Ada Admin' });
  fireEvent.click(within(group).getByRole('checkbox', { name: /Agent/ }));
  fireEvent.click(within(group).getByRole('checkbox', { name: /Admin/ }));
  expect(group).not.toHaveTextContent('Ops bot');
  expect(within(group).getByRole('button', { name: 'Apply' })).toBeEnabled();
  fireEvent.click(within(group).getByRole('checkbox', { name: /Editor/ }));
  fireEvent.click(within(group).getByRole('checkbox', { name: /Agent/ }));
  expect(group).toHaveTextContent('Ops bot');
  expect(within(group).getByRole('button', { name: 'Remove and stop 1 token' })).toBeEnabled();
});

test('changing roles changes nothing until Apply is pressed, then sets the whole list', async () => {
  const onSetRoles = vi.fn().mockResolvedValue(undefined);
  render(<TeamSection members={[admin, ann]} currentAgentId={admin.id} canManage roles={[adminRole, agentRole, editor]} myPermissions={permissionsOf('admin')} timezone="UTC" onSetRoles={onSetRoles} />);
  fireEvent.click(screen.getByRole('button', { name: 'Change roles: ann@example.com' }));
  const group = screen.getByRole('group', { name: 'Roles of ann@example.com' });
  fireEvent.click(within(group).getByRole('checkbox', { name: /Editor/ }));
  expect(onSetRoles).not.toHaveBeenCalled();
  fireEvent.click(within(group).getByRole('button', { name: 'Apply' }));
  await vi.waitFor(() => expect(onSetRoles).toHaveBeenCalledWith(ann.id, [agentRole.id, editor.id]));
});

test('taking a role a token follows says which tokens stop, and nobody is left without a role', () => {
  // The token is matched by its owner's id, not the name, which a rename or a namesake changes.
  const holder = { ...ann, display_name: 'Ann', roles: [...rolesOf('agent'), { id: editor.id, name: 'Editor', system: null }] };
  render(<TeamSection members={[admin, holder]} currentAgentId={admin.id} canManage roles={[adminRole, agentRole, editor]} myPermissions={permissionsOf('admin')} timezone="UTC" onSetRoles={vi.fn()} />);
  fireEvent.click(screen.getByRole('button', { name: 'Change roles: Ann' }));
  const group = screen.getByRole('group', { name: 'Roles of Ann' });
  fireEvent.click(within(group).getByRole('checkbox', { name: /Editor/ }));
  expect(group).toHaveTextContent('Claude on laptop');
  expect(within(group).getByRole('button', { name: 'Remove and stop 1 token' })).toBeEnabled();
  fireEvent.click(within(group).getByRole('checkbox', { name: /Agent/ }));
  expect(within(group).getByRole('button', { name: 'Remove and stop 1 token' })).toBeDisabled();
  expect(group).toHaveTextContent('Everyone needs at least one role.');
});

test('a member who holds more than you has no Change roles button', () => {
  render(<TeamSection members={[{ ...admin, manageable: false }, ann]} currentAgentId={ann.id} canManage roles={[agentRole]} myPermissions={permissionsOf('agent')} timezone="UTC" />);
  expect(screen.queryByRole('button', { name: 'Change roles: Ada Admin' })).not.toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Change roles: ann@example.com' })).toBeInTheDocument();
});

test('deactivating asks first, shows the open work and moves it to Unassigned by default', () => {
  const onDeactivate = vi.fn();
  render(<TeamSection members={[admin, ann, bob, cy]} currentAgentId={admin.id} canManage timezone="UTC" onDeactivate={onDeactivate} />);
  fireEvent.click(screen.getByRole('button', { name: 'Deactivate ann@example.com' }));
  expect(onDeactivate).not.toHaveBeenCalled();

  const confirm = screen.getByRole('group', { name: 'Deactivate ann@example.com?' });
  expect(confirm).toHaveTextContent('2 open or pending conversations are assigned to them');
  expect(within(confirm).getByRole('radio', { name: 'Move them to Unassigned' })).toBeChecked();
  fireEvent.click(within(confirm).getByRole('button', { name: 'Yes, deactivate' }));
  expect(onDeactivate).toHaveBeenCalledWith(ann.id, null);
});

test('open work can go to another active agent, never to a deactivated one or to the same person', () => {
  const onDeactivate = vi.fn();
  render(<TeamSection members={[admin, ann, bob, cy]} currentAgentId={admin.id} canManage timezone="UTC" onDeactivate={onDeactivate} />);
  fireEvent.click(screen.getByRole('button', { name: 'Deactivate ann@example.com' }));
  const confirm = screen.getByRole('group', { name: 'Deactivate ann@example.com?' });
  fireEvent.click(within(confirm).getByRole('radio', { name: 'Assign them to another agent' }));
  const target = within(confirm).getByRole('combobox', { name: 'Assign to' });
  expect(within(target).getAllByRole('option').map((option) => option.textContent)).toEqual(['Ada Admin', 'Bob Builder']);
  fireEvent.change(target, { target: { value: bob.id } });
  fireEvent.click(within(confirm).getByRole('button', { name: 'Yes, deactivate' }));
  expect(onDeactivate).toHaveBeenCalledWith(ann.id, bob.id);
});

test('cancel closes the question without changing anything', () => {
  const onDeactivate = vi.fn();
  render(<TeamSection members={[admin, ann]} currentAgentId={admin.id} canManage timezone="UTC" onDeactivate={onDeactivate} />);
  fireEvent.click(screen.getByRole('button', { name: 'Deactivate ann@example.com' }));
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
  expect(screen.queryByRole('group')).not.toBeInTheDocument();
  expect(onDeactivate).not.toHaveBeenCalled();
});

test('someone with no open work gets a plain confirmation and nothing to move', () => {
  const onDeactivate = vi.fn();
  render(<TeamSection members={[admin, bob]} currentAgentId={admin.id} canManage timezone="UTC" onDeactivate={onDeactivate} />);
  fireEvent.click(screen.getByRole('button', { name: 'Deactivate Bob Builder' }));
  const confirm = screen.getByRole('group', { name: 'Deactivate Bob Builder?' });
  expect(confirm).toHaveTextContent('No open or pending conversations are assigned to them');
  expect(within(confirm).queryByRole('radio')).not.toBeInTheDocument();
  fireEvent.click(within(confirm).getByRole('button', { name: 'Yes, deactivate' }));
  expect(onDeactivate).toHaveBeenCalledWith(bob.id, null);
});

test('deactivating yourself warns that you will be signed out', () => {
  // A second admin exists: the last active admin is never offered Deactivate.
  render(<TeamSection members={[admin, { ...ann, roles: rolesOf('admin') }]} currentAgentId={admin.id} canManage timezone="UTC" />);
  fireEvent.click(screen.getByRole('button', { name: 'Deactivate Ada Admin' }));
  expect(screen.getByRole('group', { name: 'Deactivate Ada Admin?' })).toHaveTextContent('You will be signed out');
});

test('a deactivated agent can be reactivated and has no other action', () => {
  const onReactivate = vi.fn();
  render(<TeamSection members={[admin, cy]} currentAgentId={admin.id} canManage timezone="UTC" onReactivate={onReactivate} />);
  const line = row('cy@example.com');
  expect(within(line).queryByRole('button', { name: /^Deactivate/ })).not.toBeInTheDocument();
  fireEvent.click(within(line).getByRole('button', { name: 'Reactivate cy@example.com' }));
  expect(onReactivate).toHaveBeenCalledWith(cy.id);
});

test('shows why a change was refused', () => {
  render(<TeamSection members={[admin]} currentAgentId={admin.id} canManage timezone="UTC" error="A workspace needs at least one active admin." />);
  expect(screen.getByRole('alert')).toHaveTextContent('at least one active admin');
});

test('the question is visible text and keyboard focus moves into it', () => {
  render(<TeamSection members={[admin, ann]} currentAgentId={admin.id} canManage timezone="UTC" />);
  fireEvent.click(screen.getByRole('button', { name: 'Deactivate ann@example.com' }));
  expect(screen.getByText('Deactivate ann@example.com?')).toBeVisible();
  expect(screen.getByRole('group', { name: 'Deactivate ann@example.com?' })).toHaveFocus();
});

test('cancel gives focus back to the Deactivate button of the same row', () => {
  render(<TeamSection members={[admin, ann]} currentAgentId={admin.id} canManage timezone="UTC" />);
  fireEvent.click(screen.getByRole('button', { name: 'Deactivate ann@example.com' }));
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
  expect(screen.getByRole('button', { name: 'Deactivate ann@example.com' })).toHaveFocus();
});

test('confirming leaves focus on the row, not on the page body', () => {
  render(<TeamSection members={[admin, ann]} currentAgentId={admin.id} canManage timezone="UTC" onDeactivate={vi.fn()} />);
  fireEvent.click(screen.getByRole('button', { name: 'Deactivate ann@example.com' }));
  fireEvent.click(screen.getByRole('button', { name: 'Yes, deactivate' }));
  expect(row('ann@example.com')).toHaveFocus();
});

test('the only active admin cannot deactivate or demote themself; with a second admin they can', () => {
  const props = { currentAgentId: admin.id, canManage: true, roles: [adminRole, agentRole], myPermissions: permissionsOf('admin'), timezone: 'UTC' };
  const { rerender } = render(<TeamSection members={[admin, ann]} {...props} />);
  expect(screen.queryByRole('button', { name: 'Deactivate Ada Admin' })).not.toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Deactivate ann@example.com' })).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Change roles: Ada Admin' }));
  const adminBox = screen.getByRole('checkbox', { name: /Admin/ });
  expect(adminBox).toHaveAttribute('aria-disabled', 'true');
  expect(adminBox).toHaveAccessibleDescription(settingsMessages.teamLastAdmin);
  fireEvent.click(adminBox);
  expect(adminBox).toBeChecked();
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
  rerender(<TeamSection members={[admin, { ...ann, roles: rolesOf('admin') }]} {...props} />);
  expect(screen.getByRole('button', { name: 'Deactivate Ada Admin' })).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Change roles: Ada Admin' }));
  expect(screen.getByRole('checkbox', { name: /Admin/ })).not.toHaveAttribute('aria-disabled');
});

test('why the last admin keeps Admin sits in the tip of its lock, not in a visible line', () => {
  render(<TeamSection members={[admin, ann]} currentAgentId={admin.id} canManage roles={[adminRole, agentRole]} myPermissions={permissionsOf('admin')} timezone="UTC" />);
  fireEvent.click(screen.getByRole('button', { name: 'Change roles: Ada Admin' }));
  expect(screen.queryByText(settingsMessages.teamLastAdmin)).not.toBeVisible();
  const lock = screen.getByRole('button', { name: 'Why Admin is locked' });
  expect(lock).toHaveAccessibleDescription(settingsMessages.teamLastAdmin);
  fireEvent.click(lock);
  expect(screen.getByRole('tooltip')).toHaveTextContent(settingsMessages.teamLastAdmin);
});

test('an admin renames a member from their row, and saving an empty name clears it', () => {
  const onRename = vi.fn();
  render(<TeamSection members={[admin, ann, bob]} currentAgentId={admin.id} canManage timezone="UTC" onRename={onRename} />);
  fireEvent.click(within(row('Bob Builder')).getByRole('button', { name: 'Rename Bob Builder' }));
  const input = screen.getByRole('textbox', { name: 'Display name of Bob Builder' });
  expect(input).toHaveValue('Bob Builder');
  expect(input).toHaveFocus();
  fireEvent.change(input, { target: { value: '  Robert  ' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  expect(onRename).toHaveBeenLastCalledWith(bob.id, 'Robert');
  expect(screen.queryByRole('textbox', { name: 'Display name of Bob Builder' })).not.toBeInTheDocument();

  fireEvent.click(screen.getByRole('button', { name: 'Rename Bob Builder' }));
  fireEvent.change(screen.getByRole('textbox', { name: 'Display name of Bob Builder' }), { target: { value: '' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  expect(onRename).toHaveBeenLastCalledWith(bob.id, '');
});

test('a member without a name is renamed from an empty field that shows the email, and cancel changes nothing', () => {
  const onRename = vi.fn();
  render(<TeamSection members={[admin, ann]} currentAgentId={admin.id} canManage timezone="UTC" onRename={onRename} />);
  fireEvent.click(screen.getByRole('button', { name: 'Rename ann@example.com' }));
  const input = screen.getByRole('textbox', { name: 'Display name of ann@example.com' });
  expect(input).toHaveValue('');
  expect(input).toHaveAttribute('placeholder', 'ann@example.com');
  expect(input).toHaveAttribute('maxLength', '80');
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
  expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Rename ann@example.com' })).toHaveFocus();
  expect(onRename).not.toHaveBeenCalled();
});
