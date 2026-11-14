import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, expect, test, vi } from 'vitest';
import { InvalidRequestError, PlanLimitError, UnauthorizedError, WorkspaceLockedError } from '../../api/client';
import type { Session } from '../../api/schemas';
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

// The form is in a dialog opened by Create token; the dialog's own Create token button sends it.
const openCreate = () => fireEvent.click(screen.getByRole('button', { name: 'Create token' }));
const createDialog = () => screen.getByRole('dialog', { name: 'Create a token' });
const nameField = () => within(createDialog()).getByRole('textbox', { name: 'Name' });
const submit = () => fireEvent.click(within(createDialog()).getByRole('button', { name: 'Create token' }));

// Tokens as the sessions list returns them: one of each access the list tells apart.
const tokenBase = { agent_id: '55555555-5555-4555-8555-555555555555', agent_email: 'tu@northwind.help', agent_name: 'Hoàng Tú', role: null, current: false,
  created_at: '2026-10-05T10:42:00Z', expires_at: '2099-12-05T10:42:00Z', revoked_at: null, app: null };
const grant = (kind: 'role' | 'custom' | 'legacy', extra: Partial<NonNullable<Session['token']>> = {}) =>
  ({ kind, role: null, role_held: true, permissions: ['conversations:read', 'tags:read'], legacy_tier: null, ...extra }) as NonNullable<Session['token']>;
const supportLead = { id: '88888888-8888-4888-8888-888888888888', name: 'Support lead', system: null };
const tokens: Session[] = [
  { ...tokenBase, id: '11111111-1111-4111-8111-111111111111', label: 'Claude Code on my laptop', token: grant('role', { role: rolesOf('agent')[0] }), permissions: ['conversations:read', 'tags:read'], last_seen_at: '2026-11-05T10:42:00Z' },
  { ...tokenBase, id: '22222222-2222-4222-8222-222222222222', label: 'Freshdesk agent — drafts only', token: grant('custom'), permissions: ['conversations:read', 'tags:read'], last_seen_at: '2026-11-05T09:15:00Z' },
  { ...tokenBase, id: '33333333-3333-4333-8333-333333333333', agent_email: 'long@northwind.help', agent_name: 'Long Bui', label: 'Triage bot',
    token: grant('role', { role: supportLead, role_held: false }), permissions: ['conversations:read', 'tags:read'], last_seen_at: '2026-11-03T16:20:00Z' },
  { ...tokenBase, id: '44444444-4444-4444-8444-444444444444', label: 'Nightly tagging script', token: grant('legacy', { legacy_tier: 'send' }), permissions: ['conversations:read', 'tags:read'], last_seen_at: '2026-10-28T02:05:00Z' },
];

function showList(list: Session[] = tokens, onRevoke = vi.fn()) {
  render(<AgentTokensSection roles={rolesOf('agent')} rolePermissions={new Map([[rolesOf('agent')[0].id, permissionsOf('agent')]])} held={permissionsOf('agent')}
    origin={origin} onCreate={vi.fn()} tokens={list} timezone="UTC" onRevoke={onRevoke} />);
  return onRevoke;
}
const row = (label: string) => screen.getByRole('row', { name: new RegExp(label) });

test('the token list shows each token with its person, its access, when it was last used and when it ends', () => {
  showList();
  const table = screen.getByRole('table', { name: 'Agent tokens' });
  expect(within(table).getAllByRole('columnheader').map((cell) => cell.textContent)).toEqual(['Token', 'Access', 'Last used', 'Expires', 'Actions']);
  expect(row('Claude Code on my laptop')).toHaveTextContent('tu@northwind.help');
  expect(row('Claude Code on my laptop')).toHaveTextContent('Follows role Agent');
  // Last used is the session's last_seen_at, in the person's time zone.
  expect(row('Claude Code on my laptop')).toHaveTextContent(/Nov 5,? (2026, )?10:42 AM/);
  expect(row('Claude Code on my laptop')).toHaveTextContent(/Dec 5, 2099/);
  expect(row('Freshdesk agent')).toHaveTextContent('Fixed · 2 permissions');
  expect(row('Triage bot')).toHaveTextContent('long@northwind.help');
  expect(row('Triage bot')).toHaveTextContent('Stopped: Long Bui no longer holds all of Support lead');
  expect(row('Triage bot')).toHaveTextContent('Giving them the role starts it again.');
  expect(row('Nightly tagging script')).toHaveTextContent('Created before roles · was send');
  // Connect your agent stays below the list.
  expect(table.compareDocumentPosition(screen.getByRole('heading', { level: 3, name: 'Connect your agent' })) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
});

test('What it may do opens the permissions a token holds, by name', () => {
  showList();
  const permissions = within(row('Freshdesk agent')).getByText('Tags: View');
  expect(permissions).not.toBeVisible();
  fireEvent.click(within(row('Freshdesk agent')).getByText('What it may do'));
  expect(permissions).toBeVisible();
  expect(within(row('Freshdesk agent')).getByText('Conversations: View')).toBeVisible();
});

test('revoking a token from its row menu asks first, names it, and says what stops and what stays', () => {
  const onRevoke = showList();
  fireEvent.click(within(row('Nightly tagging script')).getByRole('button', { name: 'Actions for Nightly tagging script' }));
  fireEvent.click(screen.getByRole('menuitem', { name: 'Revoke' }));
  const confirm = screen.getByRole('alertdialog', { name: 'Revoke Nightly tagging script?' });
  expect(confirm).toHaveTextContent('the agent that uses it is refused on its next call');
  expect(confirm).toHaveTextContent('Notes and replies it wrote keep showing “by Hoàng Tú via Nightly tagging script”.');
  expect(onRevoke).not.toHaveBeenCalled();
  fireEvent.click(within(confirm).getByRole('button', { name: 'Cancel' }));
  expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
  expect(onRevoke).not.toHaveBeenCalled();

  fireEvent.click(within(row('Nightly tagging script')).getByRole('button', { name: 'Actions for Nightly tagging script' }));
  fireEvent.click(screen.getByRole('menuitem', { name: 'Revoke' }));
  fireEvent.click(within(screen.getByRole('alertdialog')).getByRole('button', { name: 'Revoke' }));
  expect(onRevoke).toHaveBeenCalledWith('44444444-4444-4444-8444-444444444444');
  expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
});

test('a revoked token reads Revoked in place of its end date, with no actions left', () => {
  showList([...tokens.slice(0, 3), { ...tokens[3], revoked_at: '2026-11-12T03:00:00Z' }]);
  const revoked = row('Nightly tagging script');
  expect(revoked).toHaveTextContent('Revoked');
  expect(revoked).not.toHaveTextContent(/2099/);
  expect(within(revoked).queryByRole('button', { name: /Actions for/ })).not.toBeInTheDocument();
  // Last used still says when the agent last called.
  expect(revoked).toHaveTextContent(/Oct 28/);
  expect(within(row('Triage bot')).getByRole('button', { name: 'Actions for Triage bot' })).toBeInTheDocument();
});

test('with no token yet, the list says how to make the first one', () => {
  showList([]);
  expect(screen.getByText(/No agent tokens yet/)).toBeInTheDocument();
});

test('Create token opens the form in a dialog; Cancel closes it without creating anything', () => {
  const onCreate = show();
  expect(screen.queryByRole('textbox', { name: 'Name' })).not.toBeInTheDocument();
  openCreate();
  expect(nameField()).toHaveFocus();
  fireEvent.change(nameField(), { target: { value: 'Codex' } });
  fireEvent.click(within(createDialog()).getByRole('button', { name: 'Cancel' }));
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  expect(onCreate).not.toHaveBeenCalled();
});

test('with Send to customers off, the dialog says the agent leaves drafts for a person to send', () => {
  show(false);
  openCreate();
  fireEvent.click(within(createDialog()).getByRole('radio', { name: 'Pick permissions (fixed)' }));
  // Send to customers reaches outside the workspace: its name says so after the action.
  const send = /^Messages: Send to customers/;
  for (const name of [send, 'AI drafts and triage: Create']) {
    const box = within(createDialog()).getByRole('checkbox', { name });
    if (!(box as HTMLInputElement).checked) fireEvent.click(box);
  }
  expect(within(createDialog()).queryByText(/Send to customers is off\./)).not.toBeInTheDocument();
  fireEvent.click(within(createDialog()).getByRole('checkbox', { name: send }));
  const note = within(createDialog()).getByText(/Send to customers is off\./).closest('p') as HTMLElement;
  expect(note).toHaveTextContent('The agent leaves reply drafts above the reply box for a person to send.');
  expect(note).toHaveTextContent("With it on, the agent's replies reach customers at once; nothing waits for approval.");
  // Without AI drafts the agent leaves no draft either: the note does not promise one.
  fireEvent.click(within(createDialog()).getByRole('checkbox', { name: 'AI drafts and triage: Create' }));
  expect(within(createDialog()).queryByText(/Send to customers is off\./)).not.toBeInTheDocument();
});

test('a person with one role sees it named, not a list with one choice, and the token follows it', () => {
  const onCreate = vi.fn().mockResolvedValue(created('draft'));
  render(<AgentTokensSection roles={rolesOf('agent')} rolePermissions={new Map([[rolesOf('agent')[0].id, permissionsOf('agent')]])} held={permissionsOf('agent')} origin={origin} onCreate={onCreate} />);
  openCreate();
  expect(screen.queryByRole('combobox', { name: 'Role' })).not.toBeInTheDocument();
  expect(within(createDialog()).getByText('Agent', { selector: 'strong' })).toBeInTheDocument();
  fireEvent.change(nameField(), { target: { value: 'Claude' } });
  submit();
  expect(onCreate).toHaveBeenCalledWith('Claude', { role_id: rolesOf('agent')[0].id });
});

test('a token follows the role with the fewest permissions by default, is shown once in the dialog, copied, then hidden for good', async () => {
  const onCreate = show();
  openCreate();
  expect(screen.getByRole('radio', { name: 'Follow a role' })).toBeChecked();
  expect(screen.getByRole('combobox', { name: 'Role' })).toHaveValue(editor.id);
  expect(within(createDialog()).getByText(/2 permissions/)).toBeInTheDocument();
  fireEvent.change(nameField(), { target: { value: '  Claude Code  ' } });
  submit();
  expect(onCreate).toHaveBeenCalledWith('Claude Code', { role_id: editor.id });

  const field = await within(createDialog()).findByRole('textbox', { name: 'Your token: Claude Code' });
  expect(field).toHaveValue(token);
  expect(field).toHaveAttribute('readonly');
  // The whole token shows, also on a phone: it wraps instead of scrolling inside a one-line field.
  expect(field.tagName).toBe('TEXTAREA');
  expect(within(createDialog()).getByText(/cannot be shown again/)).toBeInTheDocument();
  // The steps to connect an agent are in the dialog, next to the token they need.
  expect(within(createDialog()).getByRole('heading', { level: 3, name: 'Connect your agent' })).toBeInTheDocument();
  fireEvent.click(within(createDialog()).getByRole('button', { name: 'Copy token' }));
  expect(writeText).toHaveBeenCalledWith(token);
  expect(await within(createDialog()).findByText('Copied')).toBeInTheDocument();

  fireEvent.click(within(createDialog()).getByRole('button', { name: 'I saved it, hide the token' }));
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  expect(screen.queryByDisplayValue(token)).not.toBeInTheDocument();
  expect(document.body.innerHTML).not.toContain(token);
  // Create token opens an empty form for the next one.
  openCreate();
  expect(nameField()).toHaveValue('');
  expect(nameField()).toHaveFocus();
});

test('a token without a name is refused at the form, and nothing is sent', () => {
  const onCreate = show();
  openCreate();
  fireEvent.change(nameField(), { target: { value: '   ' } });
  submit();
  expect(onCreate).not.toHaveBeenCalled();
  expect(nameField()).toHaveAttribute('aria-invalid', 'true');
  expect(nameField()).toHaveFocus();
  expect(screen.getByRole('alert')).toHaveTextContent('Name the token so you can tell it apart in the list.');
});

test('a request whose outcome is unknown does not claim that no token exists, keeps the form and can be pressed again', async () => {
  const onCreate = show(true, vi.fn().mockRejectedValueOnce(new Error('API 503')).mockResolvedValueOnce(created('draft')));
  openCreate();
  fireEvent.change(nameField(), { target: { value: 'Codex' } });
  submit();
  const alert = await screen.findByRole('alert');
  expect(alert).toHaveTextContent('Colliedesk did not confirm the token. If the token list shows it, revoke it, then press Create token again.');
  expect(alert).not.toHaveTextContent(/no token was created|connection/i);
  expect(nameField()).toHaveValue('Codex');
  submit();
  expect(await screen.findByDisplayValue(token)).toBeInTheDocument();
  expect(onCreate).toHaveBeenCalledTimes(2);
});

test.each([
  ['an ended session', new UnauthorizedError(), 'Your session ended, so no token was created. Sign in again to create one.'],
  ['a locked workspace', new WorkspaceLockedError('This workspace is locked.'), 'This workspace is locked.'],
  ['a plan limit', new PlanLimitError('This action is not included in your plan.'), 'This action is not included in your plan.'],
])('%s is said as such, never as a connection problem', async (_, error, message) => {
  show(true, vi.fn().mockRejectedValue(error));
  openCreate();
  fireEvent.change(nameField(), { target: { value: 'Codex' } });
  submit();
  expect(await screen.findByRole('alert')).toHaveTextContent(message);
});

test('a refusal shows the server reason', async () => {
  show(true, vi.fn().mockRejectedValue(new InvalidRequestError('You can only give permissions you hold yourself; you do not hold kb_media:purge.')));
  openCreate();
  fireEvent.change(nameField(), { target: { value: 'Ops' } });
  submit();
  expect(await screen.findByRole('alert')).toHaveTextContent('permissions you hold yourself');
});

test('the button cannot create two tokens from one press', async () => {
  let finish: (value: ReturnType<typeof created>) => void = () => {};
  const onCreate = show(true, vi.fn(() => new Promise<ReturnType<typeof created>>((resolve) => { finish = resolve; })));
  openCreate();
  fireEvent.change(nameField(), { target: { value: 'Codex' } });
  submit();
  const pending = within(createDialog()).getByRole('button', { name: 'Creating…' });
  expect(pending).toBeDisabled();
  fireEvent.click(pending);
  expect(onCreate).toHaveBeenCalledTimes(1);
  await act(async () => finish(created('draft')));
});

test('a role that reaches outside or changes the team is pointed out, with a way to leave some out', () => {
  show(true);
  openCreate();
  fireEvent.change(screen.getByRole('combobox', { name: 'Role' }), { target: { value: rolesOf('admin')[0].id } });
  expect(screen.getByText(/Pick permissions to leave some out/)).toBeInTheDocument();
});

test('each agent app has its steps with this address, a copy button, a guide and a way out', async () => {
  show();
  const hosts = screen.getByRole('radiogroup', { name: 'Agent app' });
  expect(within(hosts).getAllByRole('radio').map((radio) => radio.parentElement?.textContent)).toEqual(['Claude Code', 'Codex', 'Cursor', 'VS Code', 'Claude Desktop']);
  expect(screen.getByRole('radio', { name: 'Claude Code' })).toBeChecked();
  // A self-hosted server: Claude Code adds this server's address and signs in with OAuth, no token.
  let panel = screen.getByRole('group', { name: 'Claude Code' });
  expect(within(panel).getByRole('group', { name: 'Run in your terminal' })).toHaveTextContent(`claude mcp add --transport http colliedesk ${origin}/mcp`);
  expect(within(panel).getByRole('group', { name: 'Then sign in' })).toHaveTextContent('claude mcp login colliedesk');
  expect(panel).toHaveTextContent('needs no token');
  expect(panel).not.toHaveTextContent('Authorization');
  expect(within(panel).getByRole('link', { name: 'Full guide' })).toHaveAttribute('href', 'https://colliedesk.com/docs/connect-claude-code');
  expect(panel).toHaveTextContent('claude mcp remove colliedesk, then Disconnect it in Connected AI apps above.');
  expect(document.body.innerHTML).not.toContain('desk.example.com');
  expect(screen.getByRole('heading', { level: 3, name: 'Connect your agent' })).toBeInTheDocument();

  fireEvent.click(screen.getByRole('radio', { name: 'Codex' }));
  expect(screen.queryByRole('group', { name: 'Claude Code' })).not.toBeInTheDocument();
  panel = screen.getByRole('group', { name: 'Codex' });
  expect(panel).toHaveTextContent('codex mcp remove colliedesk, then revoke the token above.');
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

  openCreate();
  fireEvent.change(nameField(), { target: { value: 'Cursor' } });
  submit();
  // The dialog shows the host picked on the page, with the token in its button.
  const link = await within(createDialog()).findByRole('link', { name: 'Add to Cursor' });
  const url = new URL(link.getAttribute('href')!);
  expect(`${url.protocol}//${url.host}${url.pathname}`).toBe('cursor://anysphere.cursor-deeplink/mcp/install');
  expect(url.searchParams.get('name')).toBe('colliedesk');
  expect(JSON.parse(atob(url.searchParams.get('config')!))).toEqual({ url: `${origin}/mcp`, headers: { Authorization: `Bearer ${token}` } });
  // The token is in the deeplink and its own field, never in a command to paste into a shell.
  for (const code of document.querySelectorAll('pre')) expect(code.textContent).not.toContain(token);

  fireEvent.click(within(createDialog()).getByRole('button', { name: 'I saved it, hide the token' }));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Add to Cursor' })).toHaveAttribute('aria-disabled', 'true'));
  expect(screen.queryByRole('link', { name: 'Add to Cursor' })).not.toBeInTheDocument();
  expect(screen.getByText(/Token hidden\. Create another token above to use this button/)).toBeInTheDocument();
  expect(screen.queryByText(/Create a token above first/)).not.toBeInTheDocument();
});

test('picking permissions starts from the chosen role and sends exactly what is ticked', async () => {
  const onCreate = show(false);
  openCreate();
  fireEvent.change(nameField(), { target: { value: 'Writer' } });
  fireEvent.click(screen.getByRole('radio', { name: 'Pick permissions (fixed)' }));
  const update = screen.getByRole('checkbox', { name: 'Knowledge base articles: Edit' });
  expect(update).toBeChecked();
  fireEvent.click(update);
  submit();
  await waitFor(() => expect(onCreate).toHaveBeenCalledWith('Writer', { permissions: ['kb_articles:read'] }));
});

test('a permission the person does not hold is listed, locked, and cannot be ticked', () => {
  show(false);
  openCreate();
  fireEvent.click(screen.getByRole('radio', { name: 'Pick permissions (fixed)' }));
  const purge = screen.getByRole('checkbox', { name: 'Knowledge base media: Delete for good' });
  expect(purge).toHaveAttribute('aria-disabled', 'true');
  fireEvent.click(purge);
  expect(purge).not.toBeChecked();
});

test('a fixed token needs one permission', () => {
  const onCreate = show(false);
  openCreate();
  fireEvent.change(nameField(), { target: { value: 'Empty' } });
  fireEvent.click(screen.getByRole('radio', { name: 'Pick permissions (fixed)' }));
  fireEvent.click(screen.getByRole('button', { name: 'Clear all' }));
  submit();
  expect(onCreate).not.toHaveBeenCalled();
  expect(screen.getByRole('alert')).toHaveTextContent('Tick at least one permission');
});

// The role a token follows by default is the one with the fewest permissions among the person's roles now,
// also when those roles arrive or change after the form opened.
test('the default role follows the person\'s roles as they change', () => {
  const admin = rolesOf('admin')[0];
  const permissions = new Map<string, readonly string[]>([[admin.id, permissionsOf('admin')], [editor.id, ['kb_articles:read', 'kb_articles:update']]]);
  const { rerender } = render(<AgentTokensSection roles={[admin]} rolePermissions={permissions} held={permissionsOf('admin')} origin={origin} onCreate={vi.fn()} />);
  openCreate();
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

test('on Colliedesk Cloud, Claude Code installs the plugin and signs in with OAuth, with no token', () => {
  const roles = rolesOf('agent');
  render(<AgentTokensSection roles={roles} rolePermissions={new Map([[roles[0].id, permissionsOf('agent')]])} held={permissionsOf('agent')}
    origin="https://acme.colliedesk.com" mcpAddress="https://app.colliedesk.com/mcp" onCreate={vi.fn()} />);
  const panel = screen.getByRole('group', { name: 'Claude Code' });
  // Each command is a labelled stop for the keyboard, so a screen reader names what it is.
  expect(within(panel).getByRole('group', { name: 'Run in your terminal' })).toHaveTextContent(/^claude plugin install colliedesk --marketplace colliedesk\/colliedesk$/);
  expect(within(panel).getByRole('group', { name: 'Then sign in' })).toHaveTextContent('claude mcp login plugin:colliedesk:colliedesk');
  expect(panel).toHaveTextContent('claude plugin uninstall colliedesk@colliedesk, then Disconnect it in Connected AI apps above.');
  expect(panel).not.toHaveTextContent('acme.colliedesk.com');
});

test('someone who may see and revoke tokens but not make one gets the list without Create token or the setup steps', () => {
  render(<AgentTokensSection roles={[]} held={[]} tokens={[]} />);
  expect(screen.getByRole('heading', { name: 'Agent tokens' })).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Create token' })).toBeNull();
  expect(screen.queryByRole('heading', { name: 'Connect your agent' })).toBeNull();
});

test('a token that turns revoked is announced', () => {
  const props = { roles: rolesOf('agent'), rolePermissions: new Map([[rolesOf('agent')[0].id, permissionsOf('agent')]]), held: permissionsOf('agent'), origin, onCreate: vi.fn(), timezone: 'UTC', onRevoke: vi.fn() };
  const { rerender } = render(<AgentTokensSection {...props} tokens={tokens} />);
  expect(screen.getByRole('status')).toHaveTextContent('');
  rerender(<AgentTokensSection {...props} tokens={[...tokens.slice(0, 3), { ...tokens[3], revoked_at: '2026-11-12T03:00:00Z' }]} />);
  expect(screen.getByRole('status')).toHaveTextContent('Nightly tagging script revoked.');
});
