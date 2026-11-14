import { fireEvent, render, screen, within } from '@testing-library/react';
import { expect, test } from 'vitest';
import type { Session } from '../../api/schemas';
import { permissionsOf, rolesOf } from '../../test-roles';
import { AgentTokensSection } from './agent-tokens-section';
import { ConnectedAppsSection } from './connected-apps-section';

// "Quiet by default": each row of the agent screens keeps its label and one line of 60 characters or less. What the
// line used to say beyond that is the accessible description of its tip, and a procedure also links to its guide.

const origin = 'https://acme.example.com';
const guide = 'https://colliedesk.com/docs/connect-claude-code';
function show() {
  const roles = rolesOf('admin');
  render(<AgentTokensSection roles={roles} rolePermissions={new Map([[roles[0].id, permissionsOf('admin')]])} held={permissionsOf('admin')} origin={origin}
    onCreate={async () => { throw new Error('not used'); }} />);
}
// line finds the visible line by its exact words and checks it fits one quiet line.
function line(text: string) {
  expect(text.length).toBeLessThanOrEqual(60);
  return screen.getByText(text);
}
const tip = (label: string) => screen.getByRole('button', { name: `About ${label}` });
const learnMore = (label: string) => within(document.getElementById(tip(label).getAttribute('aria-controls')!)!).getByRole('link', { name: 'Learn more', hidden: true });

test('the token list says in one line that each agent has its own token, and the tip names the apps', () => {
  show();
  line('Each agent signs in with its own token.');
  expect(tip('Agent tokens')).toHaveAccessibleDescription('Connect Claude, Codex, Cursor or VS Code over MCP to work your inbox as you. Each one signs in with its own token, listed below.');
});

test('Claude Code on a self-hosted server: no token in one line, the browser step in a tip with the guide, the skills in another', () => {
  show();
  line('Claude Code needs no token; it signs in as you.');
  expect(tip('Claude Code sign-in')).toHaveAccessibleDescription('Claude Code needs no token: it signs in with your Colliedesk account and acts as you.');
  line('Allow access in the browser that opens.');
  expect(tip('Signing in')).toHaveAccessibleDescription('Your browser opens Colliedesk: choose the workspace and Allow. Or, in Claude Code, run /mcp, choose the server and Authenticate.');
  expect(learnMore('Signing in')).toHaveAttribute('href', guide);
  line('For agent skills, also install the plugin.');
  expect(tip('Agent skills')).toHaveAccessibleDescription('For the agent skills, also install the Colliedesk plugin and turn its Cloud server off in /mcp; the full guide shows how.');
  expect(learnMore('Agent skills')).toHaveAttribute('href', guide);
});

test('the Colliedesk plugin says what it installs in one line', () => {
  render(<AgentTokensSection roles={rolesOf('admin')} held={permissionsOf('admin')} origin={origin} mcpAddress="https://app.colliedesk.com/mcp" onCreate={async () => { throw new Error('not used'); }} />);
  line('Installs the Colliedesk tools and agent skills.');
  expect(tip('Plugin install')).toHaveAccessibleDescription('Installs the Colliedesk tools and the Colliedesk agent skills (see Agent skills in the docs).');
  expect(learnMore('Plugin install')).toHaveAttribute('href', guide);
});

test('Codex, VS Code and Claude Desktop keep one line about where the token lives', () => {
  show();
  fireEvent.click(screen.getByRole('radio', { name: 'Codex' }));
  line('Codex reads the token from this variable.');
  expect(tip('Codex token variable')).toHaveAccessibleDescription('Codex reads the token from this variable; it is never written into Codex’s config.');
  fireEvent.click(screen.getByRole('radio', { name: 'VS Code' }));
  line('VS Code asks for the token once and stores it securely.');
  fireEvent.click(screen.getByRole('radio', { name: 'Claude Desktop' }));
  line('Claude Desktop keeps it in your system credential store.');
});

test('Cursor says what the button needs in one line, and the tip says what it does', () => {
  show();
  fireEvent.click(screen.getByRole('radio', { name: 'Cursor' }));
  line('Create a token above to use this button.');
  expect(tip('Add to Cursor')).toHaveAccessibleDescription('Create a token above first; this button puts it into Cursor for you. You can also add it by hand below.');
  fireEvent.click(screen.getByText('Add it by hand'));
  line('Cursor reads it only when opened from a terminal.');
});

test('every host ends with its guide and a tip that holds the way out', () => {
  show();
  expect(tip('Disconnecting')).toHaveAccessibleDescription('To disconnect: run claude mcp remove colliedesk, then Disconnect it in Connected AI apps above.');
  fireEvent.click(screen.getByRole('radio', { name: 'Codex' }));
  expect(tip('Disconnecting')).toHaveAccessibleDescription('To disconnect: run codex mcp remove colliedesk, then revoke the token above.');
  fireEvent.click(screen.getByRole('radio', { name: 'Claude Desktop' }));
  expect(tip('Disconnecting')).toHaveAccessibleDescription('To disconnect: remove Colliedesk in Claude Desktop → Settings → Extensions, then revoke the token above.');
});

test('a new token says how its role works in one line, and how long it lasts in another', () => {
  show();
  fireEvent.click(screen.getByRole('button', { name: 'Create token' }));
  const dialog = screen.getByRole('dialog', { name: 'Create a token' });
  within(dialog).getByText('Any role whose permissions you all hold.');
  expect(within(dialog).getByRole('button', { name: 'About Follow a role' })).toHaveAccessibleDescription('The token changes when the role changes, stops while you lack any of them, and never gets more than you hold.');
  expect(within(dialog).getByText('Stops after 30 days unused, or one year after creation.').textContent!.length).toBeLessThanOrEqual(60);
  expect(within(dialog).getByRole('button', { name: 'About Token lifetime' })).toHaveAccessibleDescription('It stops working 30 days after its last use, and one year after you create it.');
});

const app = (change: Partial<Session> = {}): Session => ({
  id: '11111111-1111-4111-8111-111111111111', agent_id: '22222222-2222-4222-8222-222222222222', agent_email: 'ada@example.com', agent_name: 'Ada',
  label: 'Claude Code', token: { kind: 'oauth', role: null, role_held: true, permissions: [], legacy_tier: null }, role: null, permissions: [], current: false,
  created_at: '2026-11-06T09:00:00Z', last_seen_at: '2026-11-06T10:00:00Z', expires_at: '2099-12-06T10:00:00Z', revoked_at: null,
  app: { client_id: 'https://claude.ai/cc.json', user_agent: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15',
    connected_at: '2026-11-06T09:00:00Z', needs_reconnecting: false }, ...change,
});

test('Connected AI apps keeps one line, the address to add, and a tip with everything else', () => {
  render(<ConnectedAppsSection sessions={[app()]} timezone="UTC" mcpAddress="https://desk.example.com/mcp" />);
  line('AI apps you connected act as you in this workspace.');
  // The address is a value to copy, so it stays on screen.
  expect(screen.getByText('To connect one, add https://desk.example.com/mcp in your AI app.')).toBeVisible();
  expect(tip('Connected AI apps')).toHaveAccessibleDescription('AI apps you connected with OAuth act as you in this workspace, with everything you can do now. To connect one, add https://desk.example.com/mcp in Claude, ChatGPT, Codex, Cursor or Claude Code: it opens a page here to allow it. Each sign-in is its own row, so two computers show twice.');
  line('Removing it in the app does not disconnect it here.');
});
