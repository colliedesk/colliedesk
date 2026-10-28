import { MutationCache, QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { SettingsRoute } from './settings-route';

const api = vi.hoisted(() => ({
  me: vi.fn(), sessions: vi.fn(), invites: vi.fn(), emailChannel: vi.fn(), agents: vi.fn(), updateAgent: vi.fn(),
  createInvite: vi.fn(), revokeInvite: vi.fn(), revokeSession: vi.fn(), updateWorkspaceTimezone: vi.fn(), updateWorkspaceName: vi.fn(), updateAgentTimezone: vi.fn(),
  saveEmailChannel: vi.fn(), saveEmailSenderName: vi.fn(), updateWorkspaceSlug: vi.fn(), rotateEmailSecret: vi.fn(), verifyEmailDomain: vi.fn(), disconnectEmailChannel: vi.fn(),
  exportWorkspace: vi.fn(), config: vi.fn(), webForm: vi.fn(), saveWebForm: vi.fn(),
  savedReplies: vi.fn(), createSavedReply: vi.fn(), updateSavedReply: vi.fn(), deleteSavedReply: vi.fn(),
  connectGmail: vi.fn(), disconnectGmail: vi.fn(), createAgentToken: vi.fn(),
}));
const errors = vi.hoisted(() => {
  class InvalidRequestError extends Error {}
  return { PlanLimitError: class extends Error {}, InvalidRequestError, ConflictError: class extends InvalidRequestError {}, UnauthorizedError: class extends Error {}, ChangedElsewhereError: class extends InvalidRequestError {} };
});
vi.mock('../../api/client', () => ({ api, ...errors }));
// The help center card has tests of its own (features/kb); here it stands for itself in the page.
vi.mock('../kb/help-center-card', () => ({ HelpCenterSettings: () => <p>Help center card</p> }));

const me = (role: 'admin' | 'agent') => ({
  agent: { id: '11111111-1111-4111-8111-111111111111', email: 'sam@example.com', display_name: '', role, timezone_override: null },
  workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'beta-desk', name: 'Beta Desk', timezone: 'UTC' },
  timezone: 'UTC', plan: null,
});

const teammate = (id: string, email: string, overrides = {}) => ({
  id, email, display_name: '', role: 'agent', timezone_override: null, deactivated_at: null, last_active_at: null, open_conversations: 0, ...overrides,
});
const SAM = '11111111-1111-4111-8111-111111111111';
const ANN = '33333333-3333-4333-8333-333333333333';

function show(role: 'admin' | 'agent', sessions: unknown[] = [], emailChannel?: unknown, client = new QueryClient({ defaultOptions: { queries: { retry: false } } })) {
  api.me.mockResolvedValue(me(role));
  api.config.mockResolvedValue({ billing: { enabled: false }, cloud_sign_in: { enabled: false, publishable_key: null }, product_analytics: { enabled: false, key: null, host: null, realm: null } });
  api.agents.mockResolvedValue({ items: [teammate(SAM, 'sam@example.com', { role }), teammate(ANN, 'ann@example.com', { open_conversations: 1 })] });
  api.updateAgent.mockResolvedValue(teammate(ANN, 'ann@example.com', { deactivated_at: '2026-10-15T10:00:00Z' }));
  api.sessions.mockResolvedValue({ items: sessions });
  api.savedReplies.mockResolvedValue({ items: [] });
  api.invites.mockResolvedValue({ items: [] });
  api.webForm.mockResolvedValue({ enabled: false, allowed_origins: [], submit_path: '/api/v1/public/forms/beta-desk' });
  api.emailChannel.mockResolvedValue(emailChannel ?? { channel: null, inbound_url: 'https://desk.example.com/x', smtp: { configured: false, host: '', port: 587, starttls: true, authenticated: false }, from_policy: 'verified', sending_from: null });
  render(<QueryClientProvider client={client}><SettingsRoute /></QueryClientProvider>);
}

test('an admin finds Export all data in Settings', async () => {
  show('admin');
  expect(await screen.findByRole('button', { name: 'Export all data' })).toBeInTheDocument();
});

test('an admin renames the workspace in General and the new name is read back', async () => {
  show('admin');
  api.updateWorkspaceName.mockResolvedValue({ id: '22222222-2222-4222-8222-222222222222', slug: 'beta-desk', name: 'Gamma Desk', timezone: 'UTC' });
  const field = await screen.findByRole('textbox', { name: 'Workspace name' });
  expect(field).toHaveValue('Beta Desk');
  expect(field).toHaveAccessibleDescription(/help center title.*sender name/i);
  const before = api.me.mock.calls.length;
  fireEvent.change(field, { target: { value: ' Gamma Desk ' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save workspace name' }));
  await waitFor(() => expect(api.updateWorkspaceName).toHaveBeenCalledWith('Gamma Desk'));
  await waitFor(() => expect(api.me.mock.calls.length).toBeGreaterThan(before));
});

test('an agent does not see Export all data', async () => {
  show('agent');
  await screen.findByRole('heading', { name: 'Sessions' });
  expect(screen.queryByRole('button', { name: 'Export all data' })).not.toBeInTheDocument();
  expect(screen.queryByRole('textbox', { name: 'Workspace name' })).not.toBeInTheDocument();
});

test('an admin deactivates a teammate and the lists reload', async () => {
  show('admin');
  fireEvent.click(await screen.findByRole('button', { name: 'Deactivate ann@example.com' }));
  fireEvent.click(within(screen.getByRole('group', { name: 'Deactivate ann@example.com?' })).getByRole('button', { name: 'Yes, deactivate' }));
  await waitFor(() => expect(api.updateAgent).toHaveBeenCalledWith(ANN, { active: false }));
  await waitFor(() => expect(api.agents.mock.calls.length).toBeGreaterThan(1));
});

test('an agent sees the team but no controls', async () => {
  show('agent');
  const team = await screen.findByRole('region', { name: 'Team' });
  expect(await within(team).findByText('ann@example.com')).toBeInTheDocument();
  expect(within(team).queryByRole('button')).not.toBeInTheDocument();
});

test('a chosen timezone saves once, shows Saving…, then Saved', async () => {
  show('admin');
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
  show('admin');
  api.updateWorkspaceTimezone.mockRejectedValue(new Error('boom'));
  const zone = await screen.findByRole('combobox', { name: 'Workspace timezone' });
  fireEvent.change(zone, { target: { value: 'Asia/Tokyo' } });
  fireEvent.blur(zone);
  const alert = await screen.findByRole('alert');
  expect(alert).toHaveTextContent('The settings action failed. Try again.');
  expect(alert.closest('.settings-card')).toHaveTextContent('Workspace timezone');
});

test('Settings shows the loading status until its data arrives', async () => {
  show('admin');
  api.me.mockReturnValue(new Promise(() => {}));
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><SettingsRoute /></QueryClientProvider>);
  expect(screen.getAllByRole('status')[0]).toHaveTextContent('Loading workspace…');
});

test('a plain agent manages the shared saved replies and the list reloads after each change', async () => {
  const stored = { id: '88888888-8888-4888-8888-888888888888', title: 'Closing', body: 'Thanks.', created_by_agent_id: null, created_at: '2026-10-15T00:00:00Z', updated_at: '2026-10-15T00:00:00Z' };
  show('agent');
  api.createSavedReply.mockResolvedValue(stored);
  api.deleteSavedReply.mockResolvedValue(undefined);
  await screen.findByRole('heading', { name: 'Saved replies' });
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

test('an agent creates an agent token above Sessions; the list reloads to show it and the token is kept in no cache', async () => {
  const session = { id: '77777777-7777-4777-8777-777777777777', agent_id: SAM, agent_email: 'sam@example.com', label: 'Claude Code', scope: 'draft',
    current: false, created_at: '2026-10-23T00:00:00Z', last_seen_at: '2026-10-23T00:00:00Z', expires_at: '2099-01-01T00:00:00Z', revoked_at: null };
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  show('agent', [], undefined, client);
  api.createAgentToken.mockResolvedValue({ token: 'cdk_agent_routetoken', session });
  const card = await screen.findByRole('region', { name: 'External AI agents' });
  expect(within(card).getByRole('radio', { name: 'Admin' })).toBeDisabled();
  expect(card.compareDocumentPosition(screen.getByRole('region', { name: 'Sessions' })) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  api.sessions.mockResolvedValue({ items: [session] });
  const before = api.sessions.mock.calls.length;
  fireEvent.change(within(card).getByRole('textbox', { name: 'Name' }), { target: { value: 'Claude Code' } });
  fireEvent.click(within(card).getByRole('button', { name: 'Create token' }));
  expect(await within(card).findByDisplayValue('cdk_agent_routetoken')).toBeInTheDocument();
  expect(api.createAgentToken).toHaveBeenCalledWith('Claude Code', { scope: 'draft' });
  await waitFor(() => expect(api.sessions.mock.calls.length).toBeGreaterThan(before));
  expect(await within(screen.getByRole('region', { name: 'Sessions' })).findByTitle('Claude Code')).toBeInTheDocument();
  const cached = JSON.stringify([...client.getQueryCache().getAll().map((query) => query.state.data), ...client.getMutationCache().getAll().map((mutation) => mutation.state.data)]);
  expect(cached).not.toContain('cdk_agent_routetoken');
});

// The app's mutation cache turns a 401 into the signed-out screen and a 402 into the locked one; a
// refused token request goes through it like every other action.
test('a refused token request reaches the app-wide handling of signed-out and locked workspaces', async () => {
  const onError = vi.fn();
  const refused = new errors.UnauthorizedError('API 401');
  show('admin', [], undefined, new QueryClient({ defaultOptions: { queries: { retry: false } }, mutationCache: new MutationCache({ onError }) }));
  api.createAgentToken.mockRejectedValue(refused);
  const card = await screen.findByRole('region', { name: 'External AI agents' });
  fireEvent.change(within(card).getByRole('textbox', { name: 'Name' }), { target: { value: 'Codex' } });
  fireEvent.click(within(card).getByRole('button', { name: 'Create token' }));
  await waitFor(() => expect(onError.mock.calls[0]?.[0]).toBe(refused));
  expect(await within(card).findByRole('alert')).toHaveTextContent('Your session ended, so no token was created.');
});

test('Settings has a Help center section in its nav, for every agent', async () => {
  show('agent');
  expect(await screen.findByText('Help center card')).toBeInTheDocument();
  expect(screen.getByRole('heading', { name: 'Help center' })).toBeInTheDocument();
  expect(screen.getByRole('navigation', { name: 'Settings sections' })).toHaveTextContent('Help center');
});

// A link to a section (#…-heading) opens Settings before its data arrives, so the router finds no section to go to.
function openAtExportAfterTheSessionsArrive() {
  let arrive = () => {};
  const scrolled = vi.fn();
  Element.prototype.scrollIntoView = scrolled;
  window.history.replaceState(null, '', '/settings#export-heading');
  show('admin');
  api.sessions.mockReturnValue(new Promise((resolve) => { arrive = () => resolve({ items: [] }); }));
  return { scrolled, arrive: () => arrive() };
}
const leaveSettings = () => {
  window.history.replaceState(null, '', '/');
  delete (Element.prototype as { scrollIntoView?: unknown }).scrollIntoView;
};

test('Settings opened at a section goes to it once the section is drawn', async () => {
  try {
    api.sessions.mockReset();
    const { scrolled, arrive } = openAtExportAfterTheSessionsArrive();
    await screen.findByRole('status');
    expect(scrolled).not.toHaveBeenCalled();
    arrive();
    await screen.findByRole('button', { name: 'Export all data' });
    await waitFor(() => expect(scrolled).toHaveBeenCalledOnce());
    expect(scrolled.mock.contexts[0]).toBe(document.getElementById('export-heading'));
  } finally { leaveSettings(); }
});

test('Settings opened at a section stays where the person scrolled while it loaded', async () => {
  try {
    api.sessions.mockReset();
    const { scrolled, arrive } = openAtExportAfterTheSessionsArrive();
    await screen.findByRole('status');
    fireEvent.wheel(window, { deltaY: 200 });
    arrive();
    await screen.findByRole('button', { name: 'Export all data' });
    expect(scrolled).not.toHaveBeenCalled();
  } finally { leaveSettings(); }
});

test('an old link to the email channel or web form section opens its channel page', async () => {
  const original = window.location;
  for (const [hash, target] of [['#email-channel-heading', '/settings/channels/type/email'], ['#web-form-heading', '/settings/channels/type/web_form']]) {
    const replace = vi.fn();
    Object.defineProperty(window, 'location', { configurable: true, value: { ...original, hash, replace } });
    try {
      const { unmount } = render(<QueryClientProvider client={new QueryClient()}><SettingsRoute /></QueryClientProvider>);
      await waitFor(() => expect(replace).toHaveBeenCalledWith(target));
      unmount();
    } finally {
      Object.defineProperty(window, 'location', { configurable: true, value: original });
    }
  }
});

test('an old anchor opened while Settings is on screen opens its channel page too', async () => {
  const original = window.location;
  const replace = vi.fn();
  const location = { ...original, hash: '', replace };
  Object.defineProperty(window, 'location', { configurable: true, value: location });
  try {
    render(<QueryClientProvider client={new QueryClient()}><SettingsRoute /></QueryClientProvider>);
    location.hash = '#web-form-heading';
    window.dispatchEvent(new HashChangeEvent('hashchange'));
    await waitFor(() => expect(replace).toHaveBeenCalledWith('/settings/channels/type/web_form'));
  } finally {
    Object.defineProperty(window, 'location', { configurable: true, value: original });
  }
});

test('adding a saved reply again after a failed request names the same submit, so it is never stored twice', async () => {
  const stored = { id: '88888888-8888-4888-8888-888888888888', title: 'Closing', body: 'Thanks.', created_by_agent_id: null, created_at: '2026-10-15T00:00:00Z', updated_at: '2026-10-15T00:00:00Z' };
  show('agent');
  api.createSavedReply.mockReset();
  api.createSavedReply.mockRejectedValueOnce(new TypeError('Failed to fetch')).mockResolvedValueOnce(stored);
  await screen.findByRole('heading', { name: 'Saved replies' });
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
