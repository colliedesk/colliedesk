import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, expect, test, vi } from 'vitest';
import { InvalidRequestError, PlanLimitError, UnauthorizedError, WorkspaceLockedError } from '../../api/client';
import { AgentTokensSection, followableRoles } from './agent-tokens-section';
import { permissionsOf, roleOf, rolesOf } from '../../test-roles';

const origin = 'https://acme.example.com';
const token = 'cdk_agent_7Qm2vK9xT4pLw8RzN1cY5bH3sJ6dF0aE';
const created = (_kind: string) => ({ token, session: { id: '66666666-6666-4666-8666-666666666666', agent_id: '55555555-5555-4555-8555-555555555555',
  agent_email: 'sam@example.com', agent_name: 'Sam', label: 'Claude Code', token: null, role: null, permissions: [], current: false, created_at: '2026-10-23T00:00:00Z', last_seen_at: '2026-10-23T00:00:00Z',
  expires_at: '2026-11-22T00:00:00Z', revoked_at: null } } as const);

const writeText = vi.fn();
beforeEach(() => {
  writeText.mockReset().mockResolvedValue(undefined);
  Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
});

const editor = { id: '99999999-9999-4999-8999-999999999999', name: 'Editor', system: null };
// admin: the person holds Admin and Editor; otherwise Agent and Editor.
function show(admin = true, onCreate = vi.fn().mockResolvedValue(created('draft'))) {
  const roles = [...rolesOf(admin ? 'admin' : 'agent'), editor];
  const rolePermissions = new Map<string, readonly string[]>([[roles[0].id, permissionsOf(admin ? 'admin' : 'agent')], [editor.id, ['kb_articles:read', 'kb_articles:update']]]);
  render(<AgentTokensSection roles={roles} rolePermissions={rolePermissions} held={permissionsOf(admin ? 'admin' : 'agent')} origin={origin} onCreate={onCreate} />);
  return onCreate;
}

const nameField = () => screen.getByRole('textbox', { name: 'Name' });

test('a person with one role sees it named, not a list with one choice, and the token follows it', () => {
  const onCreate = vi.fn().mockResolvedValue(created('draft'));
  render(<AgentTokensSection roles={rolesOf('agent')} rolePermissions={new Map([[rolesOf('agent')[0].id, permissionsOf('agent')]])} held={permissionsOf('agent')} origin={origin} onCreate={onCreate} />);
  expect(screen.queryByRole('combobox', { name: 'Role' })).not.toBeInTheDocument();
  expect(screen.getByText('Agent', { selector: 'strong' })).toBeInTheDocument();
  fireEvent.change(nameField(), { target: { value: 'Claude' } });
  fireEvent.click(screen.getByRole('button', { name: 'Create token' }));
  expect(onCreate).toHaveBeenCalledWith('Claude', { role_id: rolesOf('agent')[0].id });
});

test('a token follows the role with the fewest permissions by default, is shown once, copied, then hidden for good', async () => {
  const onCreate = show();
  expect(screen.getByRole('radio', { name: 'Follow a role' })).toBeChecked();
  expect(screen.getByRole('combobox', { name: 'Role' })).toHaveValue(editor.id);
  expect(screen.getByText(/2 permissions/)).toBeInTheDocument();
  fireEvent.change(nameField(), { target: { value: '  Claude Code  ' } });
  fireEvent.click(screen.getByRole('button', { name: 'Create token' }));
  expect(onCreate).toHaveBeenCalledWith('Claude Code', { role_id: editor.id });

  const field = await screen.findByRole('textbox', { name: 'Your token: Claude Code' });
  expect(field).toHaveValue(token);
  expect(field).toHaveAttribute('readonly');
  // The whole token shows, also on a phone: it wraps instead of scrolling inside a one-line field.
  expect(field.tagName).toBe('TEXTAREA');
  expect(screen.getByRole('heading', { level: 3, name: 'Create a token' })).toBeInTheDocument();
  expect(screen.getByText(/cannot be shown again/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Copy token' }));
  expect(writeText).toHaveBeenCalledWith(token);
  expect(await screen.findByText('Copied')).toBeInTheDocument();

  fireEvent.click(screen.getByRole('button', { name: 'I saved it, hide the token' }));
  expect(screen.queryByDisplayValue(token)).not.toBeInTheDocument();
  expect(document.body.innerHTML).not.toContain(token);
  expect(screen.getByText(/Token hidden/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Create another token' }));
  expect(nameField()).toHaveValue('');
  expect(nameField()).toHaveFocus();
});

test('a token without a name is refused at the form, and nothing is sent', () => {
  const onCreate = show();
  fireEvent.change(nameField(), { target: { value: '   ' } });
  fireEvent.click(screen.getByRole('button', { name: 'Create token' }));
  expect(onCreate).not.toHaveBeenCalled();
  expect(nameField()).toHaveAttribute('aria-invalid', 'true');
  expect(nameField()).toHaveFocus();
  expect(screen.getByRole('alert')).toHaveTextContent('Name the token so you can tell it apart in Sessions.');
});

test('a request whose outcome is unknown does not claim that no token exists, keeps the form and can be pressed again', async () => {
  const onCreate = show(true, vi.fn().mockRejectedValueOnce(new Error('API 503')).mockResolvedValueOnce(created('draft')));
  fireEvent.change(nameField(), { target: { value: 'Codex' } });
  fireEvent.click(screen.getByRole('button', { name: 'Create token' }));
  const alert = await screen.findByRole('alert');
  expect(alert).toHaveTextContent('Colliedesk did not confirm the token. If Sessions below lists it, revoke it, then press Create token again.');
  expect(alert).not.toHaveTextContent(/no token was created|connection/i);
  expect(nameField()).toHaveValue('Codex');
  fireEvent.click(screen.getByRole('button', { name: 'Create token' }));
  expect(await screen.findByDisplayValue(token)).toBeInTheDocument();
  expect(onCreate).toHaveBeenCalledTimes(2);
});

test.each([
  ['an ended session', new UnauthorizedError(), 'Your session ended, so no token was created. Sign in again to create one.'],
  ['a locked workspace', new WorkspaceLockedError('This workspace is locked.'), 'This workspace is locked.'],
  ['a plan limit', new PlanLimitError('This action is not included in your plan.'), 'This action is not included in your plan.'],
])('%s is said as such, never as a connection problem', async (_, error, message) => {
  show(true, vi.fn().mockRejectedValue(error));
  fireEvent.change(nameField(), { target: { value: 'Codex' } });
  fireEvent.click(screen.getByRole('button', { name: 'Create token' }));
  expect(await screen.findByRole('alert')).toHaveTextContent(message);
});

test('a refusal shows the server reason', async () => {
  show(true, vi.fn().mockRejectedValue(new InvalidRequestError('You can only give permissions you hold yourself; you do not hold kb_media:purge.')));
  fireEvent.change(nameField(), { target: { value: 'Ops' } });
  fireEvent.click(screen.getByRole('button', { name: 'Create token' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('permissions you hold yourself');
});

test('the button cannot create two tokens from one press', async () => {
  let finish: (value: ReturnType<typeof created>) => void = () => {};
  const onCreate = show(true, vi.fn(() => new Promise<ReturnType<typeof created>>((resolve) => { finish = resolve; })));
  fireEvent.change(nameField(), { target: { value: 'Codex' } });
  fireEvent.click(screen.getByRole('button', { name: 'Create token' }));
  const pending = screen.getByRole('button', { name: 'Creating…' });
  expect(pending).toBeDisabled();
  fireEvent.click(pending);
  expect(onCreate).toHaveBeenCalledTimes(1);
  await act(async () => finish(created('draft')));
});

test('a role that reaches outside or changes the team is pointed out, with a way to leave some out', () => {
  show(true);
  fireEvent.change(screen.getByRole('combobox', { name: 'Role' }), { target: { value: rolesOf('admin')[0].id } });
  expect(screen.getByText(/Pick permissions to leave some out/)).toBeInTheDocument();
});

test('each agent app has its steps with this address, a copy button, a guide and a way out', async () => {
  show();
  const hosts = screen.getByRole('radiogroup', { name: 'Agent app' });
  expect(within(hosts).getAllByRole('radio').map((radio) => radio.parentElement?.textContent)).toEqual(['Claude Code', 'Codex', 'Cursor', 'VS Code', 'Claude Desktop']);
  expect(screen.getByRole('radio', { name: 'Claude Code' })).toBeChecked();
  let panel = screen.getByRole('group', { name: 'Claude Code' });
  expect(within(panel).getByText(/claude plugin install colliedesk/)).toHaveTextContent(`--config url=${origin}/mcp`);
  expect(within(panel).getByRole('link', { name: 'Full guide' })).toHaveAttribute('href', 'https://colliedesk.com/docs/connect-claude-code');
  expect(panel).toHaveTextContent('claude plugin uninstall colliedesk@colliedesk');
  expect(document.body.innerHTML).not.toContain('desk.example.com');
  expect(screen.getByRole('heading', { level: 3, name: 'Connect your agent' })).toBeInTheDocument();
  // Each command is a labelled stop for the keyboard, so a screen reader names what it is.
  expect(within(panel).getByRole('group', { name: 'Run in your terminal' })).toHaveTextContent('claude plugin install colliedesk');

  fireEvent.click(screen.getByRole('radio', { name: 'Codex' }));
  expect(screen.queryByRole('group', { name: 'Claude Code' })).not.toBeInTheDocument();
  panel = screen.getByRole('group', { name: 'Codex' });
  fireEvent.click(within(panel).getByRole('button', { name: 'Copy: Then run' }));
  expect(writeText).toHaveBeenCalledWith(`codex mcp add colliedesk --url ${origin}/mcp --bearer-token-env-var COLLIEDESK_TOKEN`);
  expect(await within(panel).findByText('Copied')).toBeInTheDocument();

  fireEvent.click(screen.getByRole('radio', { name: 'VS Code' }));
  expect(screen.getByRole('group', { name: 'VS Code' })).toHaveTextContent('MCP: Open User Configuration');

  fireEvent.click(screen.getByRole('radio', { name: 'Claude Desktop' }));
  panel = screen.getByRole('group', { name: 'Claude Desktop' });
  expect(within(panel).getByRole('link', { name: 'Download colliedesk.mcpb' })).toHaveAttribute('href', 'https://github.com/colliedesk/colliedesk/releases/latest/download/colliedesk.mcpb');
  expect(within(panel).getByRole('textbox', { name: 'Colliedesk address' })).toHaveValue(origin);
  expect(panel).toHaveTextContent('asks for two things: your Colliedesk address (copy it below) and your token.');
  fireEvent.click(within(panel).getByRole('button', { name: 'Copy address' }));
  expect(writeText).toHaveBeenLastCalledWith(origin);
  expect(screen.getByText(/Who am I in Colliedesk\?/)).toBeInTheDocument();
});

test('Add to Cursor works only while the token is shown, and carries it to Cursor alone', async () => {
  show();
  fireEvent.click(screen.getByRole('radio', { name: 'Cursor' }));
  // Still in the Tab order, so the sentence after it is found; pressing it does nothing.
  const disabled = screen.getByRole('button', { name: 'Add to Cursor' });
  expect(disabled).toHaveAttribute('aria-disabled', 'true');
  expect(disabled).toBeEnabled();
  expect(screen.getByText(/Create a token above first/)).toBeInTheDocument();

  fireEvent.change(nameField(), { target: { value: 'Cursor' } });
  fireEvent.click(screen.getByRole('button', { name: 'Create token' }));
  const link = await screen.findByRole('link', { name: 'Add to Cursor' });
  const url = new URL(link.getAttribute('href')!);
  expect(`${url.protocol}//${url.host}${url.pathname}`).toBe('cursor://anysphere.cursor-deeplink/mcp/install');
  expect(url.searchParams.get('name')).toBe('colliedesk');
  expect(JSON.parse(atob(url.searchParams.get('config')!))).toEqual({ url: `${origin}/mcp`, headers: { Authorization: `Bearer ${token}` } });
  // The token is in the deeplink and its own field, never in a command to paste into a shell.
  for (const code of document.querySelectorAll('pre')) expect(code.textContent).not.toContain(token);

  fireEvent.click(screen.getByRole('button', { name: 'I saved it, hide the token' }));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Add to Cursor' })).toHaveAttribute('aria-disabled', 'true'));
  expect(screen.queryByRole('link', { name: 'Add to Cursor' })).not.toBeInTheDocument();
  expect(screen.getByText(/Token hidden\. Create another token above to use this button/)).toBeInTheDocument();
  expect(screen.queryByText(/Create a token above first/)).not.toBeInTheDocument();
});

test('picking permissions starts from the chosen role and sends exactly what is ticked', async () => {
  const onCreate = show(false);
  fireEvent.change(nameField(), { target: { value: 'Writer' } });
  fireEvent.click(screen.getByRole('radio', { name: 'Pick permissions (fixed)' }));
  const update = screen.getByRole('checkbox', { name: 'Knowledge base articles: Edit' });
  expect(update).toBeChecked();
  fireEvent.click(update);
  fireEvent.click(screen.getByRole('button', { name: 'Create token' }));
  await waitFor(() => expect(onCreate).toHaveBeenCalledWith('Writer', { permissions: ['kb_articles:read'] }));
});

test('a permission the person does not hold is listed, locked, and cannot be ticked', () => {
  show(false);
  fireEvent.click(screen.getByRole('radio', { name: 'Pick permissions (fixed)' }));
  const purge = screen.getByRole('checkbox', { name: 'Knowledge base media: Delete for good' });
  expect(purge).toHaveAttribute('aria-disabled', 'true');
  fireEvent.click(purge);
  expect(purge).not.toBeChecked();
});

test('a fixed token needs one permission', () => {
  const onCreate = show(false);
  fireEvent.change(nameField(), { target: { value: 'Empty' } });
  fireEvent.click(screen.getByRole('radio', { name: 'Pick permissions (fixed)' }));
  fireEvent.click(screen.getByRole('button', { name: 'Clear all' }));
  fireEvent.click(screen.getByRole('button', { name: 'Create token' }));
  expect(onCreate).not.toHaveBeenCalled();
  expect(screen.getByRole('alert')).toHaveTextContent('Tick at least one permission');
});

// The role a token follows by default is the one with the fewest permissions among the person's roles now,
// also when those roles arrive or change after the form opened.
test('the default role follows the person\'s roles as they change', () => {
  const admin = rolesOf('admin')[0];
  const permissions = new Map<string, readonly string[]>([[admin.id, permissionsOf('admin')], [editor.id, ['kb_articles:read', 'kb_articles:update']]]);
  const { rerender } = render(<AgentTokensSection roles={[admin]} rolePermissions={permissions} held={permissionsOf('admin')} origin={origin} onCreate={vi.fn()} />);
  expect(screen.getByText('Admin', { selector: 'strong' })).toBeInTheDocument();
  rerender(<AgentTokensSection roles={[admin, editor]} rolePermissions={permissions} held={permissionsOf('admin')} origin={origin} onCreate={vi.fn()} />);
  expect(screen.getByRole('combobox', { name: 'Role' })).toHaveValue(editor.id);
});

test('a token may follow any role whose permissions the person all holds: every role for an admin', () => {
  const admin = roleOf(rolesOf('admin')[0], permissionsOf('admin'));
  const agent = roleOf(rolesOf('agent')[0], permissionsOf('agent'));
  const exporter = roleOf({ id: '88888888-8888-4888-8888-888888888888', name: 'Exporter', system: null }, ['conversations:read', 'workspace:export']);
  const gone = { ...roleOf({ id: '77777777-7777-4777-8777-777777777777', name: 'Old', system: null }, ['tags:read']), deleted_at: '2026-10-01T00:00:00Z' };
  const all = [admin, agent, exporter, gone];
  expect(followableRoles(rolesOf('admin'), all, permissionsOf('admin')).map((role) => role.name)).toEqual(['Admin', 'Agent', 'Exporter']);
  expect(followableRoles(rolesOf('agent'), all, permissionsOf('agent')).map((role) => role.name)).toEqual(['Agent']);
  // Without the workspace's roles (no team:read), only the person's own.
  expect(followableRoles(rolesOf('agent'), undefined, permissionsOf('agent'))).toEqual(rolesOf('agent'));
});
