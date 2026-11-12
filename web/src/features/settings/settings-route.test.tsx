import { MutationCache, QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { agentRoleRef, builtInRoles, permissionsOf, rolesOf } from '../../test-roles';
import { AccountRoute, GeneralRoute, MembersRoute, SavedRepliesRoute } from './settings-route';
import { AIAgentsSections } from './ai-agents-sections';
import { DataRoute } from './data-route';

// Settings is one page per sidebar item (#528). Each page here is drawn with what the server answers, and
// holds the sections that were on the one long Settings page before.
const api = vi.hoisted(() => ({
  me: vi.fn(), sessions: vi.fn(), invites: vi.fn(), emailChannel: vi.fn(), agents: vi.fn(), updateAgent: vi.fn(),
  createInvite: vi.fn(), revokeInvite: vi.fn(), revokeSession: vi.fn(), updateWorkspaceTimezone: vi.fn(), updateWorkspaceName: vi.fn(), updateAgentTimezone: vi.fn(),
  saveEmailChannel: vi.fn(), saveEmailSenderName: vi.fn(), updateWorkspaceSlug: vi.fn(), rotateEmailSecret: vi.fn(), verifyEmailDomain: vi.fn(), disconnectEmailChannel: vi.fn(),
  exportWorkspace: vi.fn(), config: vi.fn(), webForm: vi.fn(), saveWebForm: vi.fn(),
  savedReplies: vi.fn(), createSavedReply: vi.fn(), updateSavedReply: vi.fn(), deleteSavedReply: vi.fn(),
  connectGmail: vi.fn(), disconnectGmail: vi.fn(), createAgentToken: vi.fn(), roles: vi.fn(), setAgentRoles: vi.fn(),
}));
const errors = vi.hoisted(() => {
  class InvalidRequestError extends Error {}
  return { PlanLimitError: class extends Error {}, InvalidRequestError, ConflictError: class extends InvalidRequestError {}, UnauthorizedError: class extends Error {}, ChangedElsewhereError: class extends InvalidRequestError {} };
});
vi.mock('../../api/client', () => ({ api, ...errors }));
const numberSeries = vi.hoisted(() => ({ list: vi.fn(), set: vi.fn() }));
vi.mock('../../api/number-series', async (actual) => ({ ...(await actual<typeof import('../../api/number-series')>()), numberSeriesApi: numberSeries }));
const imports = vi.hoisted(() => ({ list: vi.fn() }));
vi.mock('../../api/imports', async (actual) => ({ ...(await actual<typeof import('../../api/imports')>()), importsApi: imports }));

const me = (role: 'admin' | 'agent') => ({
  agent: { id: '11111111-1111-4111-8111-111111111111', email: 'sam@example.com', display_name: '', roles: rolesOf(role), timezone_override: null }, permissions: permissionsOf(role),
  workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'beta-desk', name: 'Beta Desk', timezone: 'UTC' },
  timezone: 'UTC', plan: null,
});

const teammate = (id: string, email: string, overrides = {}) => ({
  id, email, display_name: '', roles: rolesOf('agent'), manageable: true, timezone_override: null, deactivated_at: null, last_active_at: null, open_conversations: 0, ...overrides,
});
const SAM = '11111111-1111-4111-8111-111111111111';
const ANN = '33333333-3333-4333-8333-333333333333';

type Page = () => React.ReactNode;
const newClient = () => new QueryClient({ defaultOptions: { queries: { retry: false } } });
// show draws one settings page (Account unless another is named) with what the server answers.
function show(role: 'admin' | 'agent', sessions: unknown[] = [], Page: Page = AccountRoute, client = newClient()) {
  api.me.mockResolvedValue(me(role));
  api.config.mockResolvedValue({ billing: { enabled: false }, cloud_sign_in: { enabled: false, publishable_key: null }, product_analytics: { enabled: false, key: null, host: null, realm: null } });
  api.agents.mockResolvedValue({ items: [teammate(SAM, 'sam@example.com', { roles: rolesOf(role) }), teammate(ANN, 'ann@example.com', { open_conversations: 1 })] });
  api.updateAgent.mockResolvedValue(teammate(ANN, 'ann@example.com', { deactivated_at: '2026-10-15T10:00:00Z' }));
  api.sessions.mockResolvedValue({ items: sessions });
  api.roles.mockResolvedValue({ items: builtInRoles });
  api.savedReplies.mockResolvedValue({ items: [] });
  api.invites.mockResolvedValue({ items: [] });
  imports.list.mockResolvedValue({ items: [] });
  numberSeries.list.mockResolvedValue({ items: [{ series: 'default', format: '#{n}', next: 32, start: 1, start_year: null, updated_at: '2026-10-31T00:00:00Z' }] });
  render(<QueryClientProvider client={client}><Page /></QueryClientProvider>);
}
const draw = (Page: Page) => render(<QueryClientProvider client={newClient()}><Page /></QueryClientProvider>);

// Account: what belongs to the person signed in.
test('Account holds Profile, Preferences, the devices signed in and the connected AI apps, under its own title', async () => {
  show('agent');
  expect(await screen.findByRole('heading', { level: 1, name: 'Account' })).toBeInTheDocument();
  for (const name of ['Profile', 'Preferences', 'Sessions', 'Connected AI apps']) expect(await screen.findByRole('region', { name }), name).toBeInTheDocument();
  for (const name of ['General', 'Team', 'Invites', 'External AI agents', 'Saved replies', 'Export all data']) expect(screen.queryByRole('region', { name }), name).not.toBeInTheDocument();
  expect(within(screen.getByRole('navigation', { name: 'Settings sections' })).getByRole('link', { name: 'Account' })).toHaveAttribute('aria-current', 'page');
});

test('Account shows the loading status until its data arrives', () => {
  api.me.mockReturnValue(new Promise(() => {}));
  api.sessions.mockReturnValue(new Promise(() => {}));
  draw(AccountRoute);
  expect(screen.getAllByRole('status')[0]).toHaveTextContent('Loading workspace…');
});

test('signing out this device from Sessions reloads who is signed in, so the signed-out screen follows', async () => {
  show('agent', [{ id: '44444444-4444-4444-8444-444444444444', agent_id: SAM, agent_email: 'sam@example.com', label: 'Cloud sign-in',
    current: true, created_at: '2026-10-14T00:00:00Z', last_seen_at: '2026-10-14T00:00:00Z', expires_at: '2099-01-01T00:00:00Z', revoked_at: null }]);
  api.revokeSession.mockResolvedValue(undefined);
  const sessions = await screen.findByRole('region', { name: 'Sessions' });
  const signOut = await within(sessions).findByRole('button', { name: 'Sign out' });
  const before = api.me.mock.calls.length;
  fireEvent.click(signOut);
  await waitFor(() => expect(api.me.mock.calls.length).toBeGreaterThan(before));
});

// A person whose roles hold none of the read permissions of a section is not shown it, and the page asks
// the server for none of it: no refused call.
test('a narrow role sees only what it may read, and Settings asks for nothing else', async () => {
  show('agent');
  cleanup();
  api.me.mockResolvedValue({ ...me('agent'), permissions: ['conversations:read', 'messages:read', 'profile:update'] });
  for (const call of Object.values(api)) call.mockClear();
  draw(AccountRoute);
  await screen.findByRole('heading', { name: 'Sessions' });
  await new Promise((resolve) => { setTimeout(resolve, 50); });
  for (const name of ['agents', 'invites', 'savedReplies', 'roles'] as const) expect(api[name], name).not.toHaveBeenCalled();
  for (const name of ['Team', 'Invites', 'Saved replies', 'Help center']) expect(screen.queryByRole('heading', { name, level: 2 }), name).not.toBeInTheDocument();
  const nav = screen.getByRole('navigation', { name: 'Settings sections' });
  for (const name of ['Members', 'Saved replies', 'Channels', 'AI & agents', 'Data']) expect(within(nav).queryByRole('link', { name }), name).not.toBeInTheDocument();
  // Conversation numbers read conversations: General holds them.
  expect(within(nav).getByRole('link', { name: 'General' })).toBeInTheDocument();
});

// General: the workspace's name, timezone and conversation numbers.
test('an admin renames the workspace in General and the new name is read back', async () => {
  show('admin', [], GeneralRoute);
  api.updateWorkspaceName.mockResolvedValue({ id: '22222222-2222-4222-8222-222222222222', slug: 'beta-desk', name: 'Gamma Desk', timezone: 'UTC' });
  expect(await screen.findByRole('heading', { level: 1, name: 'General' })).toBeInTheDocument();
  const field = await screen.findByRole('textbox', { name: 'Workspace name' });
  expect(field).toHaveValue('Beta Desk');
  expect(field).toHaveAccessibleDescription(/help center title.*sender name/i);
  const before = api.me.mock.calls.length;
  fireEvent.change(field, { target: { value: ' Gamma Desk ' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save workspace name' }));
  await waitFor(() => expect(api.updateWorkspaceName).toHaveBeenCalledWith('Gamma Desk'));
  await waitFor(() => expect(api.me.mock.calls.length).toBeGreaterThan(before));
});

test('General holds the conversation numbers and points to the help center settings in the Help center area', async () => {
  show('admin', [], GeneralRoute);
  const numbers = await screen.findByRole('region', { name: 'Conversation numbers' });
  expect(await within(numbers).findByText('Next: #32')).toBeInTheDocument();
  expect(screen.getByRole('link', { name: 'Open Help center settings' })).toHaveAttribute('href', '/knowledge/settings');
});

test('an agent reads General without the workspace controls', async () => {
  show('agent', [], GeneralRoute);
  expect(await screen.findByRole('region', { name: 'Conversation numbers' })).toBeInTheDocument();
  expect(screen.queryByRole('textbox', { name: 'Workspace name' })).not.toBeInTheDocument();
  expect(screen.queryByRole('combobox', { name: 'Workspace timezone' })).not.toBeInTheDocument();
});

test('a chosen timezone saves once, shows Saving…, then Saved', async () => {
  show('admin', [], GeneralRoute);
  let finish: () => void = () => {};
  api.updateWorkspaceTimezone.mockReturnValue(new Promise<void>((resolve) => { finish = resolve; }));
  const select = await screen.findByRole('combobox', { name: 'Workspace timezone' });
  fireEvent.change(select, { target: { value: 'Asia/Tokyo' } });
  fireEvent.blur(select);
  expect(await screen.findByText('Saving…')).toBeInTheDocument();
  fireEvent.blur(select);
  expect(api.updateWorkspaceTimezone).toHaveBeenCalledTimes(1);
  expect(api.updateWorkspaceTimezone.mock.calls[0][0]).toBe('Asia/Tokyo');
  finish();
  expect(await screen.findByText('Saved')).toHaveAttribute('role', 'status');
  expect(select).toBeEnabled();
});

test('a failed timezone save shows the failure next to its control', async () => {
  show('admin', [], GeneralRoute);
  api.updateWorkspaceTimezone.mockRejectedValue(new Error('boom'));
  const zone = await screen.findByRole('combobox', { name: 'Workspace timezone' });
  fireEvent.change(zone, { target: { value: 'Asia/Tokyo' } });
  fireEvent.blur(zone);
  const alert = await screen.findByRole('alert');
  expect(alert).toHaveTextContent('The settings action failed. Try again.');
  expect(alert.closest('.settings-card')).toHaveTextContent('Workspace timezone');
});

// Members: the team, the invites that add to it and its roles.
test('Members holds the team, the invites and the roles', async () => {
  show('admin', [], MembersRoute);
  expect(await screen.findByRole('heading', { level: 1, name: 'Members' })).toBeInTheDocument();
  for (const name of ['Team', 'Invites', 'Roles']) expect(await screen.findByRole('region', { name }), name).toBeInTheDocument();
  // Invites sit right after the team they add to.
  expect(screen.getByRole('region', { name: 'Team' }).nextElementSibling).toBe(screen.getByRole('region', { name: 'Invites' }));
});

test('an admin deactivates a teammate and the lists reload', async () => {
  show('admin', [], MembersRoute);
  fireEvent.click(await screen.findByRole('button', { name: 'Deactivate ann@example.com' }));
  fireEvent.click(within(screen.getByRole('group', { name: 'Deactivate ann@example.com?' })).getByRole('button', { name: 'Yes, deactivate' }));
  await waitFor(() => expect(api.updateAgent).toHaveBeenCalledWith(ANN, { active: false }));
  await waitFor(() => expect(api.agents.mock.calls.length).toBeGreaterThan(1));
});

test('an agent sees the team but no controls', async () => {
  show('agent', [], MembersRoute);
  const team = await screen.findByRole('region', { name: 'Team' });
  expect(await within(team).findByText('ann@example.com')).toBeInTheDocument();
  expect(within(team).queryByRole('button')).not.toBeInTheDocument();
});

test('a burst of Create invite presses sends one invite under one key, and the button waits for it', async () => {
  show('admin', [], MembersRoute);
  let answer!: (value: unknown) => void;
  api.createInvite.mockReturnValue(new Promise((resolve) => { answer = resolve; }));
  fireEvent.change(await screen.findByRole('textbox', { name: 'Invite email' }), { target: { value: 'new@example.com' } });
  await screen.findByRole('button', { name: 'Roles: Agent' });
  const create = screen.getByRole('button', { name: 'Create invite' });
  fireEvent.click(create); fireEvent.click(create); fireEvent.click(create);
  await waitFor(() => expect(api.createInvite).toHaveBeenCalled());
  await new Promise((resolve) => { setTimeout(resolve, 20); });
  expect(api.createInvite).toHaveBeenCalledTimes(1);
  expect(api.createInvite).toHaveBeenCalledWith('new@example.com', [agentRoleRef.id], expect.stringMatching(/^[\x21-\x7e]{1,200}$/));
  await waitFor(() => expect(create).toBeDisabled());
  answer({ invite: { id: 'i1', email: 'new@example.com', roles: rolesOf('agent'), created_by_email: 'sam@example.com', created_at: '2026-10-28T07:00:00Z', expires_at: '2026-11-04T07:00:00Z', used_at: null, revoked_at: null }, url: 'https://desk.example.com/invite#t' });
  await waitFor(() => expect(create).toBeEnabled());
});

// Revoking an invite is its own permission (team:revoke), apart from creating one.
test('Members offers Revoke on a pending invite only to a person who may revoke invites', async () => {
  const pending = { id: '44444444-4444-4444-8444-444444444444', email: 'kim@example.com', roles: rolesOf('agent'), created_by_email: 'sam@example.com',
    created_at: '2026-10-28T07:00:00Z', expires_at: '2099-01-01T00:00:00Z', used_at: null, revoked_at: null };
  show('admin', [], MembersRoute);
  api.invites.mockResolvedValue({ items: [pending] });
  cleanup();
  draw(MembersRoute);
  const invites = await screen.findByRole('region', { name: 'Invites' });
  await within(invites).findByText('kim@example.com');
  expect(within(invites).getByRole('button', { name: 'Revoke' })).toBeInTheDocument();
  cleanup();
  api.me.mockResolvedValue({ ...me('admin'), permissions: permissionsOf('admin').filter((name) => name !== 'team:revoke') });
  draw(MembersRoute);
  const without = await screen.findByRole('region', { name: 'Invites' });
  await within(without).findByText('kim@example.com');
  expect(within(without).getByRole('button', { name: 'Create invite' })).toBeInTheDocument();
  expect(within(without).queryByRole('button', { name: 'Revoke' })).not.toBeInTheDocument();
});

// Saved replies: shared by the whole team.
test('a plain agent manages the shared saved replies and the list reloads after each change', async () => {
  const stored = { id: '88888888-8888-4888-8888-888888888888', title: 'Closing', body: 'Thanks.', created_by_agent_id: null, created_at: '2026-10-15T00:00:00Z', updated_at: '2026-10-15T00:00:00Z' };
  show('agent', [], SavedRepliesRoute);
  api.createSavedReply.mockResolvedValue(stored);
  api.deleteSavedReply.mockResolvedValue(undefined);
  expect(await screen.findByRole('heading', { level: 1, name: 'Saved replies' })).toBeInTheDocument();
  await screen.findByRole('heading', { level: 2, name: 'Saved replies' });
  fireEvent.click(screen.getByRole('button', { name: 'New saved reply' }));
  api.savedReplies.mockResolvedValue({ items: [stored] });
  fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Closing' } });
  fireEvent.change(screen.getByLabelText('Text'), { target: { value: 'Thanks.' } });
  fireEvent.click(screen.getByRole('button', { name: 'Add saved reply' }));
  await waitFor(() => expect(api.createSavedReply).toHaveBeenCalledWith('Closing', 'Thanks.', expect.stringMatching(/^[\x21-\x7e]{1,200}$/)));
  expect(await screen.findByText('Thanks.')).toBeInTheDocument();
  api.savedReplies.mockResolvedValue({ items: [] });
  fireEvent.click(screen.getByRole('button', { name: 'Delete Closing' }));
  fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
  await waitFor(() => expect(api.deleteSavedReply).toHaveBeenCalledWith(stored.id, stored.updated_at));
  expect(await screen.findByText('No saved replies yet.')).toBeInTheDocument();
});

test('adding a saved reply again after a failed request names the same submit, so it is never stored twice', async () => {
  const stored = { id: '88888888-8888-4888-8888-888888888888', title: 'Closing', body: 'Thanks.', created_by_agent_id: null, created_at: '2026-10-15T00:00:00Z', updated_at: '2026-10-15T00:00:00Z' };
  show('agent', [], SavedRepliesRoute);
  api.createSavedReply.mockReset();
  api.createSavedReply.mockRejectedValueOnce(new TypeError('Failed to fetch')).mockResolvedValueOnce(stored);
  await screen.findByRole('heading', { level: 2, name: 'Saved replies' });
  fireEvent.click(screen.getByRole('button', { name: 'New saved reply' }));
  fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Closing' } });
  fireEvent.change(screen.getByLabelText('Text'), { target: { value: 'Thanks.' } });
  fireEvent.click(screen.getByRole('button', { name: 'Add saved reply' }));
  expect(await screen.findByRole('alert')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Add saved reply' }));
  await waitFor(() => expect(api.createSavedReply).toHaveBeenCalledTimes(2));
  const [first, second] = api.createSavedReply.mock.calls as unknown as [string, string, string][];
  expect(first[2]).toEqual(expect.any(String));
  expect(second[2]).toBe(first[2]);
});

// AI & agents: the agent tokens and the sessions they opened.
test('an agent creates an agent token above its Sessions; the list reloads to show it and the token is kept in no cache', async () => {
  const session = { id: '77777777-7777-4777-8777-777777777777', agent_id: SAM, agent_email: 'sam@example.com', agent_name: 'Sam', label: 'Claude Code',
    token: { kind: 'role', role: agentRoleRef, role_held: true, permissions: permissionsOf('agent'), legacy_tier: null }, role: null, permissions: permissionsOf('agent'), current: false, created_at: '2026-10-23T00:00:00Z', last_seen_at: '2026-10-23T00:00:00Z', expires_at: '2099-01-01T00:00:00Z', revoked_at: null };
  const client = newClient();
  show('agent', [], AIAgentsSections, client);
  api.createAgentToken.mockResolvedValue({ token: 'cdk_agent_routetoken', session });
  const card = await screen.findByRole('region', { name: 'External AI agents' });
  expect(card.compareDocumentPosition(screen.getByRole('region', { name: 'Sessions' })) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  api.sessions.mockResolvedValue({ items: [session] });
  const before = api.sessions.mock.calls.length;
  await waitFor(() => expect(api.roles).toHaveBeenCalled());
  const rolesBefore = api.roles.mock.calls.length;
  fireEvent.click(within(card).getByRole('button', { name: 'Create token' }));
  const dialog = screen.getByRole('dialog', { name: 'Create a token' });
  expect(within(dialog).getByRole('radio', { name: 'Follow a role' })).toBeChecked();
  expect(within(dialog).getByText('Agent', { selector: 'strong' })).toBeInTheDocument();
  fireEvent.change(within(dialog).getByRole('textbox', { name: 'Name' }), { target: { value: 'Claude Code' } });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Create token' }));
  expect(await within(dialog).findByDisplayValue('cdk_agent_routetoken')).toBeInTheDocument();
  expect(api.createAgentToken).toHaveBeenCalledWith('Claude Code', { role_id: agentRoleRef.id }, expect.any(String));
  await waitFor(() => expect(api.sessions.mock.calls.length).toBeGreaterThan(before));
  // The new token follows a role: the roles list, which Team reads to warn before a role is taken, reloads.
  await waitFor(() => expect(api.roles.mock.calls.length).toBeGreaterThan(rolesBefore));
  expect(await within(screen.getByRole('region', { name: 'Sessions' })).findByTitle('Claude Code')).toBeInTheDocument();
  // The token list on External AI agents shows it too, without leaving the page.
  expect(await within(card).findByRole('row', { name: /Claude Code/ })).toBeInTheDocument();
  const cached = JSON.stringify([...client.getQueryCache().getAll().map((query) => query.state.data), ...client.getMutationCache().getAll().map((mutation) => mutation.state.data)]);
  expect(cached).not.toContain('cdk_agent_routetoken');
});

// The app's mutation cache turns a 401 into the signed-out screen and a 402 into the locked one; a
// refused token request goes through it like every other action.
test('a refused token request reaches the app-wide handling of signed-out and locked workspaces', async () => {
  const onError = vi.fn();
  const refused = new errors.UnauthorizedError('API 401');
  show('admin', [], AIAgentsSections, new QueryClient({ defaultOptions: { queries: { retry: false } }, mutationCache: new MutationCache({ onError }) }));
  api.createAgentToken.mockRejectedValue(refused);
  const card = await screen.findByRole('region', { name: 'External AI agents' });
  fireEvent.click(within(card).getByRole('button', { name: 'Create token' }));
  const dialog = screen.getByRole('dialog', { name: 'Create a token' });
  fireEvent.change(within(dialog).getByRole('textbox', { name: 'Name' }), { target: { value: 'Codex' } });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Create token' }));
  await waitFor(() => expect(onError.mock.calls[0]?.[0]).toBe(refused));
  expect(await within(dialog).findByRole('alert')).toHaveTextContent('Your session ended, so no token was created.');
});

test('revoking an agent token in AI & agents reloads its sessions, who is signed in and the roles that name it', async () => {
  const session = { id: '77777777-7777-4777-8777-777777777777', agent_id: SAM, agent_email: 'sam@example.com', agent_name: 'Sam', label: 'Claude Code',
    token: { kind: 'role', role: agentRoleRef, role_held: true, permissions: permissionsOf('agent'), legacy_tier: null }, role: null, permissions: permissionsOf('agent'), current: false, created_at: '2026-10-23T00:00:00Z', last_seen_at: '2026-10-23T00:00:00Z', expires_at: '2099-01-01T00:00:00Z', revoked_at: null };
  show('agent', [session], AIAgentsSections);
  api.revokeSession.mockResolvedValue(undefined);
  const sessions = await screen.findByRole('region', { name: 'Sessions' });
  const revoke = await within(sessions).findByRole('button', { name: 'Revoke' });
  await waitFor(() => expect(api.roles).toHaveBeenCalled());
  const before = { me: api.me.mock.calls.length, sessions: api.sessions.mock.calls.length, roles: api.roles.mock.calls.length };
  fireEvent.click(revoke);
  // Revoking a token asks first; only the dialog's Revoke sends it.
  expect(api.revokeSession).not.toHaveBeenCalled();
  fireEvent.click(within(screen.getByRole('alertdialog', { name: 'Revoke Claude Code?' })).getByRole('button', { name: 'Revoke' }));
  await waitFor(() => expect(api.revokeSession.mock.calls[0]?.[0]).toBe(session.id));
  await waitFor(() => expect(api.sessions.mock.calls.length).toBeGreaterThan(before.sessions));
  await waitFor(() => expect(api.me.mock.calls.length).toBeGreaterThan(before.me));
  // A revoked token no longer follows its role: the roles list, which names those tokens, reloads too.
  await waitFor(() => expect(api.roles.mock.calls.length).toBeGreaterThan(before.roles));
});

// The devices a person signed in on stay in Account; the agent tokens are listed in AI & agents.
test('agent tokens are listed in AI & agents, browser sessions in Account', async () => {
  const base = { agent_id: SAM, agent_email: 'sam@example.com', agent_name: 'Sam', role: null, current: false, created_at: '2026-10-23T00:00:00Z', last_seen_at: '2026-10-23T00:00:00Z', expires_at: '2099-01-01T00:00:00Z', revoked_at: null };
  const sessions = [
    { ...base, id: '77777777-7777-4777-8777-777777777777', label: 'Claude Code', permissions: permissionsOf('agent'), token: { kind: 'role', role: agentRoleRef, role_held: true, permissions: permissionsOf('agent'), legacy_tier: null } },
    { ...base, id: '66666666-6666-4666-8666-666666666666', label: 'Firefox', permissions: [], token: null },
  ];
  show('agent', sessions);
  const devices = await screen.findByRole('region', { name: 'Sessions' });
  expect(await within(devices).findByTitle('Firefox')).toBeInTheDocument();
  expect(within(devices).queryByTitle('Claude Code')).not.toBeInTheDocument();
  cleanup();
  draw(AIAgentsSections);
  const tokens = await screen.findByRole('region', { name: 'Sessions' });
  expect(await within(tokens).findByTitle('Claude Code')).toBeInTheDocument();
  expect(within(tokens).queryByTitle('Firefox')).not.toBeInTheDocument();
  // Revoking a token stops an AI agent; it signs no browser out, so the card says so instead of the devices' line.
  expect(tokens).toHaveTextContent('AI agents working with a token. Revoke one to stop it.');
  expect(tokens).not.toHaveTextContent('on this device that signs you out');
});

// Data: the Freshdesk import and the whole-workspace export.
test('an admin finds the import and Export all data on Data', async () => {
  show('admin', [], DataRoute);
  expect(await screen.findByRole('heading', { level: 1, name: 'Data' })).toBeInTheDocument();
  expect(await screen.findByRole('button', { name: 'Export all data' })).toBeInTheDocument();
  expect(await screen.findByRole('heading', { level: 2, name: 'Connect your Freshdesk account' })).toBeInTheDocument();
  // The import was a page of its own; in Data it has the page's title, the settings menu and one main landmark.
  expect(screen.getByText('Import from Freshdesk')).toBeInTheDocument();
  expect(within(screen.getByRole('navigation', { name: 'Settings sections' })).getByRole('link', { name: 'Data' })).toHaveAttribute('aria-current', 'page');
  expect(screen.getAllByRole('main')).toHaveLength(1);
  expect(screen.getByLabelText('Freshdesk address')).toBeInTheDocument();
});

test('an agent does not see Export all data', async () => {
  imports.list.mockClear();
  show('agent', [], DataRoute);
  await screen.findByRole('heading', { level: 1, name: 'Data' });
  await new Promise((resolve) => { setTimeout(resolve, 20); });
  expect(screen.queryByRole('button', { name: 'Export all data' })).not.toBeInTheDocument();
  expect(imports.list).not.toHaveBeenCalled();
});

// The import's email links here (/settings/import-freshdesk): a person who may neither import nor export is told who can.
test('Data tells a person who may neither import nor export that admins do it', async () => {
  show('agent', [], DataRoute);
  await screen.findByRole('heading', { level: 1, name: 'Data' });
  expect(await screen.findByText('Only a workspace admin can import. Ask an admin to import your Freshdesk account.')).toBeInTheDocument();
  expect(screen.getByText('Only workspace admins can export all data.')).toBeInTheDocument();
});

// A link to a section (#…-heading) opens the page before its data arrives, so the router finds no section to go to.
function openAtExportBeforeWhoIsSignedInArrives() {
  let arrive = () => {};
  const scrolled = vi.fn();
  Element.prototype.scrollIntoView = scrolled;
  window.history.replaceState(null, '', '/settings/data#export-heading');
  show('admin', [], () => null);
  api.me.mockReturnValue(new Promise((resolve) => { arrive = () => resolve(me('admin')); }));
  draw(DataRoute);
  return { scrolled, arrive: () => arrive() };
}
const leaveSettings = () => {
  window.history.replaceState(null, '', '/');
  delete (Element.prototype as { scrollIntoView?: unknown }).scrollIntoView;
};

test('a settings page opened at a section goes to it once the section is drawn', async () => {
  try {
    const { scrolled, arrive } = openAtExportBeforeWhoIsSignedInArrives();
    await screen.findByRole('status');
    expect(scrolled).not.toHaveBeenCalled();
    arrive();
    await screen.findByRole('button', { name: 'Export all data' });
    // The menu also brings its current page into view (on a narrow screen); that is not the section.
    await waitFor(() => expect(scrolled.mock.contexts).toContain(document.getElementById('export-heading')));
  } finally { leaveSettings(); }
});

test('a settings page opened at a section stays where the person scrolled while it loaded', async () => {
  try {
    const { scrolled, arrive } = openAtExportBeforeWhoIsSignedInArrives();
    await screen.findByRole('status');
    fireEvent.wheel(window, { deltaY: 200 });
    arrive();
    await screen.findByRole('button', { name: 'Export all data' });
    expect(scrolled.mock.contexts).not.toContain(document.getElementById('export-heading'));
  } finally { leaveSettings(); }
});
