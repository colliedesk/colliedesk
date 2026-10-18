import { fireEvent, render, screen, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import type { TeamMember } from '../../api/schemas';
import { TeamSection } from './team-section';

const member = (overrides: Partial<TeamMember> & Pick<TeamMember, 'id' | 'email'>): TeamMember => ({
  display_name: '', role: 'agent', timezone_override: null, deactivated_at: null, last_active_at: null, open_conversations: 0, ...overrides,
});
const admin = member({ id: '11111111-1111-4111-8111-111111111111', email: 'admin@example.com', display_name: 'Ada Admin', role: 'admin', last_active_at: '2026-10-15T09:30:00Z' });
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

test('choosing a role changes nothing until Apply is pressed', () => {
  const onChangeRole = vi.fn();
  render(<TeamSection members={[admin, ann]} currentAgentId={admin.id} canManage timezone="UTC" onChangeRole={onChangeRole} />);
  expect(screen.queryByRole('button', { name: 'Apply role for ann@example.com' })).not.toBeInTheDocument();
  const select = screen.getByRole('combobox', { name: 'Role of ann@example.com' });
  fireEvent.change(select, { target: { value: 'admin' } });
  expect(onChangeRole).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Apply role for ann@example.com' }));
  expect(onChangeRole).toHaveBeenCalledWith(ann.id, 'admin');
});

test('putting the role back hides Apply, and a self demotion is applied the same explicit way', () => {
  const onChangeRole = vi.fn();
  render(<TeamSection members={[admin, ann]} currentAgentId={admin.id} canManage timezone="UTC" onChangeRole={onChangeRole} />);
  const own = screen.getByRole('combobox', { name: 'Role of Ada Admin' });
  fireEvent.change(own, { target: { value: 'agent' } });
  expect(onChangeRole).not.toHaveBeenCalled();
  fireEvent.change(own, { target: { value: 'admin' } });
  expect(screen.queryByRole('button', { name: 'Apply role for Ada Admin' })).not.toBeInTheDocument();
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
  render(<TeamSection members={[admin, { ...ann, role: 'admin' }]} currentAgentId={admin.id} canManage timezone="UTC" />);
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
  const { rerender } = render(<TeamSection members={[admin, ann]} currentAgentId={admin.id} canManage timezone="UTC" />);
  expect(screen.queryByRole('button', { name: 'Deactivate Ada Admin' })).not.toBeInTheDocument();
  expect(screen.getByRole('combobox', { name: 'Role of Ada Admin' })).toBeDisabled();
  expect(screen.getByRole('button', { name: 'Deactivate ann@example.com' })).toBeInTheDocument();
  rerender(<TeamSection members={[admin, { ...ann, role: 'admin' }]} currentAgentId={admin.id} canManage timezone="UTC" />);
  expect(screen.getByRole('button', { name: 'Deactivate Ada Admin' })).toBeInTheDocument();
  expect(screen.getByRole('combobox', { name: 'Role of Ada Admin' })).toBeEnabled();
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
