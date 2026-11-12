import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { RolesRoute } from './roles-route';
import { RolesSection } from './role-list';
import { adminRoleRef, builtInRoles, permissionsOf, roleOf } from '../../../test-roles';

const mocks = vi.hoisted(() => ({
  api: { me: vi.fn(), config: vi.fn(), roles: vi.fn(), role: vi.fn(), createRole: vi.fn(), restoreRole: vi.fn() },
  navigate: vi.fn(),
}));
vi.mock('@tanstack/react-router', () => ({ useNavigate: () => mocks.navigate }));
vi.mock('../../../api/client', async (importOriginal) => ({ ...(await importOriginal<typeof import('../../../api/client')>()), api: mocks.api }));

function setUp() {
  mocks.navigate.mockClear();
  mocks.api.me.mockResolvedValue({ agent: { id: '11111111-1111-4111-8111-111111111111', email: 'ada@example.com', display_name: 'Ada', roles: [adminRoleRef], timezone_override: null },
    permissions: permissionsOf('admin'), workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'acme', name: 'Acme', timezone: 'UTC' }, timezone: 'UTC', token: null, plan: null });
  mocks.api.config.mockResolvedValue({ billing: { enabled: false } });
  mocks.api.roles.mockImplementation((deleted?: boolean) => Promise.resolve({ items: deleted ? [] : builtInRoles }));
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

// The form is a page of its own and the list a section of Members, so the list that opens after a save is a new page: it still says
// what was saved and where to go next.
test('after a role is created, the list that opens says so and links to Team', async () => {
  const client = setUp();
  const tagger = roleOf({ id: '99999999-9999-4999-8999-999999999999', name: 'Tagger', system: null }, ['tags:read']);
  mocks.api.createRole.mockResolvedValue(tagger);
  const form = render(<QueryClientProvider client={client}><RolesRoute page={{ kind: 'new' }} /></QueryClientProvider>);
  fireEvent.change(await screen.findByRole('textbox', { name: 'Name' }), { target: { value: 'Tagger' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save role' }));
  await vi.waitFor(() => expect(mocks.navigate).toHaveBeenCalledWith({ to: '/settings/members', hash: 'roles-heading' }));
  form.unmount();

  render(<QueryClientProvider client={client}><RolesSection /></QueryClientProvider>);
  const status = await screen.findByRole('status');
  expect(status).toHaveTextContent('Tagger created. Give it to people in Team.');
  expect(within(status).getByRole('link', { name: 'Go to Team' })).toHaveAttribute('href', '/settings/members#team-heading');
});

test('a list opened without a save says nothing', async () => {
  const client = setUp();
  render(<QueryClientProvider client={client}><RolesSection /></QueryClientProvider>);
  expect(await screen.findByText('Agent')).toBeInTheDocument();
  expect(screen.queryByRole('status')).not.toBeInTheDocument();
});

const MAI = { id: '33333333-3333-4333-8333-333333333333', name: 'Mai', active: true };
const agentWithoutSend = () => roleOf(builtInRoles[1], (builtInRoles[1].default_permissions ?? []).filter((item) => item !== 'messages:send'), { members: [MAI] });

// Reset to default fills the matrix with the built-in list; saving says what changes before it applies.
test('Reset to default changes nothing until the change it makes is shown and saved', async () => {
  const client = setUp();
  const agent = agentWithoutSend();
  mocks.api.roles.mockResolvedValue({ items: [builtInRoles[0], agent] });
  mocks.api.role.mockResolvedValue(agent);
  (mocks.api as Record<string, ReturnType<typeof vi.fn>>).updateRole = vi.fn().mockResolvedValue({ ...agent, permissions: agent.default_permissions });
  (mocks.api as Record<string, ReturnType<typeof vi.fn>>).resetRole = vi.fn();
  render(<QueryClientProvider client={client}><RolesRoute page={{ kind: 'edit', id: agent.id }} /></QueryClientProvider>);
  fireEvent.click(await screen.findByRole('button', { name: 'Reset to default' }));
  expect(mocks.navigate).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Save role' }));
  // The change sits in the bar with the button that applies it, so it is seen before it is applied.
  const bar = screen.getByRole('button', { name: 'Save and apply' }).closest('.role-save-bar') as HTMLElement;
  expect(within(bar).getByText(/Adding 1: /)).toHaveTextContent('Send');
  expect(within(bar).getByText(/Mai gets this at once/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Save and apply' }));
  await vi.waitFor(() => expect((mocks.api as Record<string, ReturnType<typeof vi.fn>>).updateRole).toHaveBeenCalledWith(agent.id, expect.objectContaining({ permissions: agent.default_permissions })));
  expect((mocks.api as Record<string, ReturnType<typeof vi.fn>>).resetRole).not.toHaveBeenCalled();
});

test('a save says who it changed on the list it opens, and the list takes focus there', async () => {
  const client = setUp();
  const agent = agentWithoutSend();
  mocks.api.role.mockResolvedValue(agent);
  (mocks.api as Record<string, ReturnType<typeof vi.fn>>).updateRole = vi.fn().mockResolvedValue(agent);
  const form = render(<QueryClientProvider client={client}><RolesRoute page={{ kind: 'edit', id: agent.id }} /></QueryClientProvider>);
  fireEvent.change(await screen.findByRole('textbox', { name: 'Name' }), { target: { value: 'Agent' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save role' }));
  await vi.waitFor(() => expect(mocks.navigate).toHaveBeenCalled());
  form.unmount();
  render(<QueryClientProvider client={client}><RolesSection /></QueryClientProvider>);
  const status = await screen.findByRole('status');
  expect(status).toHaveTextContent('Agent saved. 1 person and 0 tokens updated.');
  await vi.waitFor(() => expect(status).toHaveFocus());
});

test('a role you may only read names who can change it, and names who holds it as links', async () => {
  const client = setUp();
  const lead = roleOf({ id: '88888888-8888-4888-8888-888888888888', name: 'Lead', system: null }, ['team:manage_roles', 'team:read'], { members: [{ id: '44444444-4444-4444-8444-444444444444', name: 'Linh', active: true }] });
  const viewed = roleOf({ id: '77777777-7777-4777-8777-777777777777', name: 'Viewer', system: null }, ['tags:read'], { editable: false, members: [MAI],
    tokens: [{ id: '66666666-6666-4666-8666-666666666666', label: 'KB bot', owner: 'Mai', owner_id: MAI.id }] });
  mocks.api.roles.mockResolvedValue({ items: [roleOf(builtInRoles[0], builtInRoles[0].permissions, { members: [{ id: '55555555-5555-4555-8555-555555555555', name: 'Ada', active: true }] }), builtInRoles[1], lead, viewed] });
  mocks.api.role.mockResolvedValue(viewed);
  render(<QueryClientProvider client={client}><RolesRoute page={{ kind: 'edit', id: viewed.id }} /></QueryClientProvider>);
  expect(await screen.findByText('Ask someone who can change roles (Ada, Linh) to change it.')).toBeInTheDocument();
  expect(screen.getByRole('link', { name: 'Mai' })).toHaveAttribute('href', '/settings/members#team-heading');
  expect(screen.getByRole('link', { name: 'KB bot (Mai)' })).toHaveAttribute('href', '/settings/ai#agent-tokens-heading');
});

test('a taken name puts focus back on the name, with the reason', async () => {
  const client = setUp();
  const { ConflictError } = await vi.importActual<typeof import('../../../api/client')>('../../../api/client');
  const taken = Object.assign(new ConflictError('A role named Agent exists already.'), { detail: { code: 'role_name_taken' } });
  mocks.api.createRole.mockRejectedValue(taken);
  render(<QueryClientProvider client={client}><RolesRoute page={{ kind: 'new' }} /></QueryClientProvider>);
  const name = await screen.findByRole('textbox', { name: 'Name' });
  fireEvent.change(name, { target: { value: 'Agent' } });
  const save = screen.getByRole('button', { name: 'Save role' });
  save.focus();
  fireEvent.click(save);
  await vi.waitFor(() => expect(name).toHaveAttribute('aria-invalid', 'true'));
  expect(name).toHaveFocus();
});

test('Undo after a delete brings the role back and says so, without offering Undo again', async () => {
  const client = setUp();
  const zero = roleOf({ id: '12121212-1212-4121-8121-121212121212', name: 'Zero', system: null }, ['tags:read'], { deletable: true });
  mocks.api.role.mockResolvedValue(zero);
  (mocks.api as Record<string, ReturnType<typeof vi.fn>>).deleteRole = vi.fn().mockResolvedValue(undefined);
  mocks.api.restoreRole.mockResolvedValue(zero);
  const form = render(<QueryClientProvider client={client}><RolesRoute page={{ kind: 'edit', id: zero.id }} /></QueryClientProvider>);
  fireEvent.click(await screen.findByRole('button', { name: 'Delete role' }));
  const asked = mocks.api.role.mock.calls.length;
  fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
  await vi.waitFor(() => expect(mocks.navigate).toHaveBeenCalled());
  // The deleted role is not asked for again, so its page never flashes "This role was deleted".
  await new Promise((resolve) => { setTimeout(resolve, 50); });
  expect(mocks.api.role.mock.calls.length).toBe(asked);
  expect(screen.queryByText('This role was deleted.')).not.toBeInTheDocument();
  form.unmount();
  render(<QueryClientProvider client={client}><RolesSection /></QueryClientProvider>);
  const status = await screen.findByRole('status');
  expect(status).toHaveTextContent('Zero deleted.');
  fireEvent.click(within(status).getByRole('button', { name: 'Undo' }));
  await vi.waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('Zero is back.'));
  expect(within(screen.getByRole('status')).queryByRole('button', { name: 'Undo' })).not.toBeInTheDocument();
  expect(mocks.api.restoreRole).toHaveBeenCalledWith(zero.id, expect.any(String));
});

test('an Undo that fails on the way can be tried again', async () => {
  const client = setUp();
  const zero = roleOf({ id: '13131313-1313-4131-8131-131313131313', name: 'Zero', system: null }, ['tags:read'], { deletable: true });
  mocks.api.role.mockResolvedValue(zero);
  (mocks.api as Record<string, ReturnType<typeof vi.fn>>).deleteRole = vi.fn().mockResolvedValue(undefined);
  mocks.api.restoreRole.mockReset();
  mocks.api.restoreRole.mockRejectedValueOnce(new Error('network')).mockResolvedValueOnce(zero);
  const form = render(<QueryClientProvider client={client}><RolesRoute page={{ kind: 'edit', id: zero.id }} /></QueryClientProvider>);
  fireEvent.click(await screen.findByRole('button', { name: 'Delete role' }));
  fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
  await vi.waitFor(() => expect(mocks.navigate).toHaveBeenCalled());
  form.unmount();
  render(<QueryClientProvider client={client}><RolesSection /></QueryClientProvider>);
  fireEvent.click(within(await screen.findByRole('status')).getByRole('button', { name: 'Undo' }));
  await vi.waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('Undo did not get through; try again.'));
  fireEvent.click(within(screen.getByRole('status')).getByRole('button', { name: 'Undo' }));
  await vi.waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('Zero is back.'));
});
