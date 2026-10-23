import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, expect, test, vi } from 'vitest';
import { InvalidRequestError, PlanLimitError, UnauthorizedError, WorkspaceLockedError } from '../../api/client';
import { AgentTokensSection } from './agent-tokens-section';

const origin = 'https://acme.example.com';
const token = 'cdk_agent_7Qm2vK9xT4pLw8RzN1cY5bH3sJ6dF0aE';
const created = (scope: string) => ({ token, session: { id: '66666666-6666-4666-8666-666666666666', agent_id: '55555555-5555-4555-8555-555555555555',
  agent_email: 'sam@example.com', label: 'Claude Code', scope, current: false, created_at: '2026-10-23T00:00:00Z', last_seen_at: '2026-10-23T00:00:00Z',
  expires_at: '2026-11-22T00:00:00Z', revoked_at: null } } as const);

const writeText = vi.fn();
beforeEach(() => {
  writeText.mockReset().mockResolvedValue(undefined);
  Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
});

function show(canCreateAdmin = true, onCreate = vi.fn().mockResolvedValue(created('draft'))) {
  render(<AgentTokensSection canCreateAdmin={canCreateAdmin} origin={origin} onCreate={onCreate} />);
  return onCreate;
}

const nameField = () => screen.getByRole('textbox', { name: 'Name' });

test('a token is created with its name and tier, shown once, copied, then hidden for good', async () => {
  const onCreate = show();
  expect(screen.getByRole('radio', { name: 'Draft' })).toBeChecked();
  expect(screen.getByText(/Recommended/)).toBeInTheDocument();
  fireEvent.change(nameField(), { target: { value: '  Claude Code  ' } });
  fireEvent.click(screen.getByRole('radio', { name: 'Send' }));
  expect(screen.getByText('Also sends replies to customers on its own.')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Create token' }));
  expect(onCreate).toHaveBeenCalledWith('Claude Code', 'send');

  const field = await screen.findByRole('textbox', { name: 'Your token: Claude Code' });
  expect(field).toHaveValue(token);
  expect(field).toHaveAttribute('readonly');
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
  show(true, vi.fn().mockRejectedValue(new InvalidRequestError('Only a workspace admin can create an admin token. Choose read, draft or send.')));
  fireEvent.change(nameField(), { target: { value: 'Ops' } });
  fireEvent.click(screen.getByRole('button', { name: 'Create token' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Only a workspace admin can create an admin token.');
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

test('an agent sees Admin but cannot pick it, and is told why', () => {
  show(false);
  expect(screen.getByRole('radio', { name: 'Admin' })).toBeDisabled();
  expect(screen.getByText('Only an admin can create an Admin token.')).toBeInTheDocument();
  expect(screen.getByRole('radio', { name: 'Draft' })).toBeChecked();
});

test('an admin may pick Admin', () => {
  show(true);
  expect(screen.getByRole('radio', { name: 'Admin' })).toBeEnabled();
  expect(screen.queryByText('Only an admin can create an Admin token.')).not.toBeInTheDocument();
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
  fireEvent.click(within(panel).getByRole('button', { name: 'Copy address' }));
  expect(writeText).toHaveBeenLastCalledWith(origin);
  expect(screen.getByText(/Who am I in Colliedesk\?/)).toBeInTheDocument();
});

test('Add to Cursor works only while the token is shown, and carries it to Cursor alone', async () => {
  show();
  fireEvent.click(screen.getByRole('radio', { name: 'Cursor' }));
  const disabled = screen.getByText('Add to Cursor');
  expect(disabled).not.toHaveAttribute('href');
  expect(disabled).toHaveAttribute('aria-disabled', 'true');
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
  await waitFor(() => expect(screen.getByText('Add to Cursor')).not.toHaveAttribute('href'));
});
