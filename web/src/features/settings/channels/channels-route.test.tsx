import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, expect, test, vi } from 'vitest';
import { ChannelsRoute } from './channels-route';
import type { ChannelsSearch } from './channels-route';

const mocks = vi.hoisted(() => {
  class InvalidRequestError extends Error {}
  class PlanLimitError extends Error {}
  class UnauthorizedError extends Error {}
  const api = {
    me: vi.fn(), config: vi.fn(), emailChannel: vi.fn(), saveEmailChannel: vi.fn(), saveEmailSenderName: vi.fn(), updateWorkspaceSlug: vi.fn(),
    rotateEmailSecret: vi.fn(), verifyEmailDomain: vi.fn(), disconnectEmailChannel: vi.fn(), connectGmail: vi.fn(), disconnectGmail: vi.fn(),
  };
  const channelsApi = { list: vi.fn(), get: vi.fn(), create: vi.fn(), update: vi.fn(), remove: vi.fn(), events: vi.fn(), replayHeld: vi.fn(), discardHeld: vi.fn() };
  return { api, channelsApi, InvalidRequestError, PlanLimitError, UnauthorizedError };
});
vi.mock('../../../api/client', () => ({ api: mocks.api, InvalidRequestError: mocks.InvalidRequestError, PlanLimitError: mocks.PlanLimitError, UnauthorizedError: mocks.UnauthorizedError }));
vi.mock('../../../api/channels', () => ({ channelsApi: mocks.channelsApi }));
const { api, channelsApi } = mocks;

const EMAIL = '44444444-4444-4444-8444-444444444444';
const FORM = '55555555-5555-4555-8555-555555555555';
const me = (role: 'admin' | 'agent') => ({
  agent: { id: '11111111-1111-4111-8111-111111111111', email: 'sam@example.com', display_name: '', role, timezone_override: null },
  workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'beta-desk', name: 'Beta Desk', timezone: 'UTC' }, timezone: 'UTC', plan: null,
});
const channel = (type: 'email' | 'web_form', change: Record<string, unknown> = {}) => ({
  id: type === 'email' ? EMAIL : FORM, type, name: type === 'email' ? 'Email' : 'Web form', enabled: true,
  status: { state: 'working', reason_code: null, message: null }, summary: type === 'email' ? 'support@example.com' : 'https://example.com',
  capabilities: type === 'email' ? ['threads', 'cc'] : ['form'], held_count: 0, unsent_count: 0, last_inbound_at: null,
  created_at: '2026-10-14T08:00:00Z', updated_at: '2026-10-14T09:00:00Z', ...change,
});
// As the server answers: a type is available while the workspace has fewer than its most.
const types = (emailCount = 1, formCount = 1) => [
  { type: 'email', available: emailCount < 1, max_per_workspace: 1, count: emailCount }, { type: 'web_form', available: formCount < 1, max_per_workspace: 1, count: formCount },
];
const withChannel = {
  channel: {
    support_address: 'support@example.com', sender_name: 'Acme Support', secret_set: true, secret_rotated_at: '2026-10-14T09:00:00Z', domain: 'example.com', domain_verified_at: null,
    dns_record_name: '_colliedesk.example.com', dns_record_value: 'colliedesk-verification=abc', dns_records: [], dns_records_expired_at: null,
    created_at: '2026-10-14T08:00:00Z', updated_at: '2026-10-14T09:00:00Z',
  },
  inbound_url: 'https://desk.example.com/x', smtp: { configured: true, host: 'smtp.example.net', port: 587, starttls: true, authenticated: true },
  from_policy: 'any', sending_from: 'support@example.com', hosted_address: null, domain_verification: 'txt', sending_pause: null,
  gmail: { available: true, missing_settings: [], redirect_uri: 'https://desk.example.com/api/v1/channels/email/gmail/callback', connection: null },
};
const webFormConfig = { enabled: true, allowed_origins: ['https://example.com'], submit_path: '/api/v1/public/forms/beta-desk' };

beforeEach(() => {
  vi.clearAllMocks();
  api.me.mockResolvedValue(me('admin'));
  api.config.mockResolvedValue({ billing: { enabled: false }, cloud_sign_in: { enabled: false, publishable_key: null }, product_analytics: { enabled: false, key: null, host: null, realm: null } });
  api.emailChannel.mockResolvedValue(withChannel);
  channelsApi.events.mockResolvedValue({ items: [], next_cursor: null });
});

function show(path: string, search: ChannelsSearch = {}, navigate = vi.fn()) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><ChannelsRoute path={path} search={search} navigate={navigate} /></QueryClientProvider>);
  return navigate;
}

test('an empty workspace offers each type to connect, email first, and Live chat as not available yet', async () => {
  channelsApi.list.mockResolvedValue({ items: [], types: types(0, 0) });
  show('');
  const empty = await screen.findByRole('region', { name: 'No channel yet' });
  const links = within(empty).getAllByRole('link');
  expect(links[0]).toHaveTextContent('Connect your support email');
  expect(links[0]).toHaveAttribute('href', '/settings/channels/new/email');
  expect(within(empty).getByRole('link', { name: /Add a web form/ })).toHaveAttribute('href', '/settings/channels/new/web_form');
  expect(within(empty).getByText('Live chat').closest('[aria-disabled="true"]')).toHaveTextContent('Not available yet');
  expect(screen.queryByText(/Zalo/)).toBeNull();
});

test('the list says when it is loading and when it failed, with a way to try again', async () => {
  channelsApi.list.mockRejectedValueOnce(new Error('API 500')).mockResolvedValue({ items: [channel('email')], types: types(1, 0) });
  show('');
  expect(document.querySelector('[aria-busy="true"]')).not.toBeNull();
  fireEvent.click(await screen.findByRole('button', { name: 'Try again' }));
  expect(await screen.findByText('support@example.com')).toBeInTheDocument();
});

test('a channel that needs attention says why with a link to fix it; one that is off reads Off', async () => {
  channelsApi.list.mockResolvedValue({ items: [
    channel('email', { status: { state: 'needs_attention', reason_code: 'cannot_send', message: null } }),
    channel('web_form', { enabled: false, status: { state: 'off', reason_code: null, message: null } }),
  ], types: types() });
  show('');
  const notice = await screen.findByText('Needs attention:');
  expect(notice.closest('.notice')).toHaveTextContent('Email: Replies cannot be sent yet.');
  expect(within(notice.closest('.notice') as HTMLElement).getByRole('link', { name: 'Fix it' })).toHaveAttribute('href', `/settings/channels/${EMAIL}`);
  expect(screen.getByRole('switch', { name: 'Web form' })).not.toBeChecked();
  expect(screen.getAllByText('Off').length).toBeGreaterThan(0);
});

test('an agent reads the channels without switches or Add channel', async () => {
  api.me.mockResolvedValue(me('agent'));
  channelsApi.list.mockResolvedValue({ items: [channel('email')], types: types(1, 0) });
  show('');
  expect(await screen.findByText('support@example.com')).toBeInTheDocument();
  expect(screen.queryByRole('switch')).toBeNull();
  expect(screen.queryByRole('button', { name: 'Add channel' })).toBeNull();
  expect(screen.getByRole('link', { name: 'Configure Email' })).toHaveAttribute('href', `/settings/channels/${EMAIL}`);
});

test('turning a channel off asks first, then sends enabled false; a failure puts the switch back and says so', async () => {
  channelsApi.list.mockResolvedValue({ items: [channel('web_form')], types: types(0, 1) });
  channelsApi.update.mockRejectedValue(new Error('API 500'));
  show('');
  const toggle = await screen.findByRole('switch', { name: 'Web form' });
  fireEvent.click(toggle);
  const question = screen.getByRole('alertdialog', { name: 'Turn Web form off?' });
  expect(question).toHaveTextContent('conversations stay');
  expect(channelsApi.update).not.toHaveBeenCalled();
  fireEvent.click(within(question).getByRole('button', { name: 'Turn off' }));
  await waitFor(() => expect(channelsApi.update).toHaveBeenCalledWith(FORM, { enabled: false }));
  expect(await screen.findByRole('alert')).toHaveTextContent('The channel could not be changed.');
  expect(screen.getByRole('switch', { name: 'Web form' })).toBeChecked();
});

test('turning a channel on needs no question', async () => {
  channelsApi.list.mockResolvedValue({ items: [channel('web_form', { enabled: false, status: { state: 'off', reason_code: null, message: null } })], types: types(0, 1) });
  channelsApi.update.mockResolvedValue({ ...channel('web_form'), config: webFormConfig });
  show('');
  fireEvent.click(await screen.findByRole('switch', { name: 'Web form' }));
  await waitFor(() => expect(channelsApi.update).toHaveBeenCalledWith(FORM, { enabled: true }));
  expect(screen.queryByRole('alertdialog')).toBeNull();
});

test('Add channel lists the types, one per workspace, and is off once every type is added', async () => {
  channelsApi.list.mockResolvedValue({ items: [channel('email')], types: types(1, 0) });
  const navigate = show('');
  fireEvent.click(await screen.findByRole('button', { name: 'Add channel' }));
  const dialog = screen.getByRole('dialog', { name: 'Add channel' });
  expect(within(dialog).getByText('Email').closest('[aria-disabled="true"]')).toHaveTextContent('One per workspace for now');
  expect(within(dialog).getByText('Live chat').closest('[aria-disabled="true"]')).toHaveTextContent('Not available yet');
  fireEvent.click(within(dialog).getByRole('button', { name: /Add a web form/ }));
  expect(navigate).toHaveBeenCalledWith('/settings/channels/new/web_form');
});

test('with every type added, Add channel is disabled and says why', async () => {
  channelsApi.list.mockResolvedValue({ items: [channel('email'), channel('web_form')], types: types() });
  show('');
  const add = await screen.findByRole('button', { name: 'Add channel' });
  expect(add).toBeDisabled();
  expect(add).toHaveAccessibleDescription(/one of each per workspace/);
});

test('replies not sent while a channel was off are counted, with the inbox filter that lists them', async () => {
  channelsApi.list.mockResolvedValue({ items: [channel('email', { unsent_count: 2 })], types: types(1, 0) });
  show('');
  const notice = await screen.findByText(/2 replies were not sent while Email was off/);
  expect(within(notice.closest('.notice') as HTMLElement).getByRole('link', { name: 'See them' })).toHaveAttribute('href', '/?delivery_failed=true&channel=email');
});

test('the type shortcut opens the channel of that type and keeps what the address carried', async () => {
  channelsApi.list.mockResolvedValue({ items: [channel('email')], types: types(1, 0) });
  const navigate = show('type/email', { tab: 'replies', gmail: 'connected' });
  await waitFor(() => expect(navigate).toHaveBeenCalledWith(`/settings/channels/${EMAIL}?tab=replies&gmail=connected`, true));
});

test('the type shortcut opens the connect form when the workspace has no channel of that type', async () => {
  channelsApi.list.mockResolvedValue({ items: [], types: types(0, 0) });
  const navigate = show('type/web_form');
  await waitFor(() => expect(navigate).toHaveBeenCalledWith('/settings/channels/new/web_form', true));
});

test('connecting email creates the channel with its support address and opens it', async () => {
  api.emailChannel.mockResolvedValue({ ...withChannel, channel: null });
  channelsApi.create.mockResolvedValue({ ...channel('email'), config: withChannel.channel });
  const navigate = show('new/email');
  fireEvent.change(await screen.findByRole('textbox', { name: 'Support address' }), { target: { value: ' help@example.com ' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save support address' }));
  await waitFor(() => expect(channelsApi.create).toHaveBeenCalledWith({ type: 'email', config: { support_address: 'help@example.com' } }));
  await waitFor(() => expect(navigate).toHaveBeenCalledWith(`/settings/channels/${EMAIL}`, true));
});

test('adding a web form creates it on, with its allowed origins, and opens it', async () => {
  channelsApi.create.mockResolvedValue({ ...channel('web_form'), config: webFormConfig });
  const navigate = show('new/web_form');
  expect(await screen.findByRole('heading', { name: 'Web form' })).toBeInTheDocument();
  const origins = await screen.findByRole('textbox', { name: /Allowed origins/i });
  expect(screen.queryByRole('checkbox')).toBeNull();
  fireEvent.change(origins, { target: { value: 'https://example.com' } });
  fireEvent.click(screen.getByRole('button', { name: /save web form/i }));
  await waitFor(() => expect(channelsApi.create).toHaveBeenCalledWith({ type: 'web_form', enabled: true, config: { allowed_origins: ['https://example.com'] } }));
  await waitFor(() => expect(navigate).toHaveBeenCalledWith(`/settings/channels/${FORM}`, true));
});

test('a refused web form connect shows the server message', async () => {
  channelsApi.create.mockRejectedValue(new mocks.InvalidRequestError('Invalid origin "*".'));
  show('new/web_form');
  fireEvent.click(await screen.findByRole('button', { name: /save web form/i }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Invalid origin "*".');
});

test('a refused connect shows the server message next to the form', async () => {
  api.emailChannel.mockResolvedValue({ ...withChannel, channel: null });
  channelsApi.create.mockRejectedValue(new mocks.InvalidRequestError('Enter a valid support address.'));
  show('new/email');
  fireEvent.change(await screen.findByRole('textbox', { name: 'Support address' }), { target: { value: 'help@example.com' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save support address' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Enter a valid support address.');
});

test('a channel page shows the type tabs then History, and opens a tab from the address', async () => {
  channelsApi.get.mockResolvedValue({ ...channel('email'), config: withChannel.channel });
  const navigate = show(EMAIL, { tab: 'replies' });
  expect(await screen.findByRole('heading', { name: 'Email' })).toBeInTheDocument();
  const tabs = screen.getAllByRole('tab').map((tab) => tab.textContent);
  expect(tabs).toEqual(['Connection', 'Replies', 'History']);
  expect(screen.getByRole('tab', { name: 'Replies' })).toHaveAttribute('aria-selected', 'true');
  expect(await screen.findByRole('textbox', { name: 'Sender name' })).toBeInTheDocument();
  fireEvent.click(screen.getByRole('tab', { name: 'Connection' }));
  expect(navigate).toHaveBeenCalledWith(`/settings/channels/${EMAIL}?tab=connection`, true);
  // Email's removal is named Disconnect email, from the same place and question as every channel.
  expect(screen.queryByRole('button', { name: 'Remove channel' })).toBeNull();
  expect(screen.getByRole('button', { name: 'Disconnect email' })).toBeInTheDocument();
});

test('the Replies tab puts each block under its heading, how replies leave first', async () => {
  channelsApi.get.mockResolvedValue({ ...channel('email'), config: withChannel.channel });
  show(EMAIL, { tab: 'replies' });
  await screen.findByRole('textbox', { name: 'Sender name' });
  const headings = (await screen.findAllByRole('heading', { level: 3 })).map((heading) => heading.textContent);
  expect(headings.slice(0, 3)).toEqual(['How replies leave', 'The name customers see', 'Your domain']);
  expect(within(screen.getByRole('region', { name: 'How replies leave' })).getByText('Replies are sent from support@example.com.')).toBeInTheDocument();
  expect(within(screen.getByRole('region', { name: 'Your domain' })).getByRole('textbox', { name: 'TXT record name' })).toBeInTheDocument();
});

test('a channel that does not exist says so, with the way back', async () => {
  channelsApi.get.mockRejectedValue(new Error('API 404'));
  show(EMAIL);
  expect(await screen.findByRole('alert')).toHaveTextContent('This channel was removed or does not exist.');
  expect(screen.getByRole('link', { name: 'All channels' })).toHaveAttribute('href', '/settings/channels');
});

// Moved from settings-route.test.tsx with the email channel; the assertions are unchanged.
test('Connect Gmail sends the browser to Google, and the outcome it comes back with is shown once', async () => {
  const assign = vi.fn();
  const original = window.location;
  window.history.replaceState(null, '', `/settings/channels/${EMAIL}?tab=replies&gmail_error=denied`);
  Object.defineProperty(window, 'location', { configurable: true, value: { ...original, assign, search: '?tab=replies&gmail_error=denied', pathname: `/settings/channels/${EMAIL}`, hash: '' } });
  try {
    api.connectGmail.mockResolvedValue({ url: 'https://accounts.google.com/o/oauth2/v2/auth?state=x' });
    channelsApi.get.mockResolvedValue({ ...channel('email'), config: withChannel.channel });
    show(EMAIL, { tab: 'replies', gmail_error: 'denied' });
    expect(await screen.findByText('Gmail was not connected: access was cancelled at Google.')).toBeInTheDocument();
    fireEvent.click(await screen.findByRole('button', { name: 'Connect Gmail' }));
    await waitFor(() => expect(assign).toHaveBeenCalledWith('https://accounts.google.com/o/oauth2/v2/auth?state=x'));
  } finally {
    Object.defineProperty(window, 'location', { configurable: true, value: original });
  }
});

test('saving the sender name sends only the trimmed name, never the address this page loaded', async () => {
  api.saveEmailSenderName.mockResolvedValue(withChannel);
  channelsApi.get.mockResolvedValue({ ...channel('email'), config: withChannel.channel });
  show(EMAIL, { tab: 'replies' });
  const field = await screen.findByRole('textbox', { name: 'Sender name' });
  fireEvent.change(field, { target: { value: ' Colliedesk Support ' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save sender name' }));
  await waitFor(() => expect(api.saveEmailSenderName).toHaveBeenCalledWith('Colliedesk Support'));
  expect(api.saveEmailChannel).not.toHaveBeenCalled();
});

test('the web form save shows Saved after it succeeds', async () => {
  channelsApi.get.mockResolvedValue({ ...channel('web_form'), config: webFormConfig });
  channelsApi.update.mockResolvedValue({ ...channel('web_form'), config: webFormConfig });
  show(FORM);
  fireEvent.click(await screen.findByRole('button', { name: /save web form/i }));
  await waitFor(() => expect(screen.getByText('Saved')).toHaveAttribute('role', 'status'));
});

test('a refused web form save shows the server message, not the generic failure', async () => {
  channelsApi.get.mockResolvedValue({ ...channel('web_form'), config: webFormConfig });
  channelsApi.update.mockRejectedValue(new mocks.InvalidRequestError('Invalid origin "*": use an exact origin such as https://example.com.'));
  show(FORM);
  fireEvent.click(await screen.findByRole('button', { name: /save web form/i }));
  await waitFor(() => expect(screen.getByText(/Invalid origin "\*": use an exact origin/)).toBeInTheDocument());
});

test('the Embed tab saves only the origins: the switch alone turns the form on or off', async () => {
  channelsApi.get.mockResolvedValue({ ...channel('web_form', { enabled: false }), config: { ...webFormConfig, enabled: false } });
  channelsApi.update.mockResolvedValue({ ...channel('web_form', { enabled: false }), config: { ...webFormConfig, enabled: false } });
  show(FORM);
  expect(await screen.findByRole('textbox', { name: /Allowed origins/i })).toBeInTheDocument();
  expect(screen.queryByRole('checkbox', { name: /Accept/i })).toBeNull();
  fireEvent.change(screen.getByRole('textbox', { name: /Allowed origins/i }), { target: { value: 'https://example.com\nhttps://shop.example.com' } });
  fireEvent.click(screen.getByRole('button', { name: /save web form/i }));
  await waitFor(() => expect(channelsApi.update).toHaveBeenCalledWith(FORM, { config: { allowed_origins: ['https://example.com', 'https://shop.example.com'] } }));
});

test('removing a web form asks first, then removes it and goes back to the list', async () => {
  channelsApi.get.mockResolvedValue({ ...channel('web_form'), config: webFormConfig });
  channelsApi.remove.mockResolvedValue(undefined);
  const navigate = show(FORM);
  fireEvent.click(await screen.findByRole('button', { name: 'Remove channel' }));
  const question = screen.getByRole('alertdialog', { name: 'Remove Web form?' });
  expect(question).toHaveTextContent('Its conversations stay');
  fireEvent.click(within(question).getByRole('button', { name: 'Yes, remove' }));
  await waitFor(() => expect(channelsApi.remove).toHaveBeenCalledWith(FORM));
  await waitFor(() => expect(navigate).toHaveBeenCalledWith('/settings/channels', true));
});

test('a channel that holds messages is not removed until they are opened or discarded', async () => {
  channelsApi.get.mockResolvedValue({ ...channel('web_form', { enabled: false, held_count: 2 }), config: webFormConfig });
  show(FORM);
  fireEvent.click(await screen.findByRole('button', { name: 'Remove channel' }));
  const question = screen.getByRole('alertdialog', { name: 'Remove Web form?' });
  expect(within(question).getByRole('alert')).toHaveTextContent('It holds 2 messages that arrived while it was off: open or discard them first.');
  expect(within(question).getByRole('button', { name: 'Yes, remove' })).toBeDisabled();
  expect(channelsApi.remove).not.toHaveBeenCalled();
});

test('disconnecting email asks in a dialog that names the address, and is refused up front while mail is held', async () => {
  channelsApi.get.mockResolvedValue({ ...channel('email', { held_count: 1 }), config: withChannel.channel });
  show(EMAIL);
  fireEvent.click(await screen.findByRole('button', { name: 'Disconnect email' }));
  const question = screen.getByRole('alertdialog', { name: 'Disconnect email?' });
  expect(question).toHaveTextContent('support@example.com');
  expect(within(question).getByRole('alert')).toHaveTextContent('It holds 1 message that arrived while it was off: open or discard it first.');
  expect(within(question).getByRole('button', { name: 'Yes, disconnect' })).toBeDisabled();
});

test('disconnecting email that holds nothing removes the channel and goes back to the list', async () => {
  channelsApi.get.mockResolvedValue({ ...channel('email'), config: withChannel.channel });
  channelsApi.remove.mockResolvedValue(undefined);
  const navigate = show(EMAIL);
  fireEvent.click(await screen.findByRole('button', { name: 'Disconnect email' }));
  fireEvent.click(within(screen.getByRole('alertdialog', { name: 'Disconnect email?' })).getByRole('button', { name: 'Yes, disconnect' }));
  await waitFor(() => expect(channelsApi.remove).toHaveBeenCalledWith(EMAIL));
  await waitFor(() => expect(navigate).toHaveBeenCalledWith('/settings/channels', true));
});

test('removing a channel reloads what the inbox reads of it: its own settings, the conversations and the history', async () => {
  channelsApi.get.mockResolvedValue({ ...channel('email'), config: withChannel.channel });
  channelsApi.remove.mockResolvedValue(undefined);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  client.setQueryData(['email-channel'], withChannel);
  client.setQueryData(['conversations', {}], { items: [] });
  client.setQueryData(['channel-events', ''], { pages: [], pageParams: [] });
  const navigate = vi.fn();
  render(<QueryClientProvider client={client}><ChannelsRoute path={EMAIL} search={{}} navigate={navigate} /></QueryClientProvider>);
  fireEvent.click(await screen.findByRole('button', { name: 'Disconnect email' }));
  fireEvent.click(within(screen.getByRole('alertdialog')).getByRole('button', { name: 'Yes, disconnect' }));
  await waitFor(() => expect(navigate).toHaveBeenCalledWith('/settings/channels', true));
  // Marked stale, or already read again when the page still shows it.
  for (const key of [['email-channel'], ['conversations', {}], ['channel-events', '']]) {
    const state = client.getQueryState(key);
    expect(Boolean(state?.isInvalidated) || (state?.dataUpdateCount ?? 0) > 1, JSON.stringify(key)).toBe(true);
  }
});

test('creating the inbound secret reloads the channel, so its state and notice follow', async () => {
  channelsApi.get.mockResolvedValue({ ...channel('email', { status: { state: 'needs_attention', reason_code: 'no_inbound_secret', message: null } }), config: withChannel.channel });
  api.emailChannel.mockResolvedValue({ ...withChannel, channel: { ...withChannel.channel, secret_set: false, secret_rotated_at: null } });
  api.rotateEmailSecret.mockResolvedValue({ inbound_secret: 'k.secret', inbound_url: 'https://desk.example.com/x' });
  show(EMAIL);
  fireEvent.click(await screen.findByRole('button', { name: 'Create inbound secret' }));
  await waitFor(() => expect(api.rotateEmailSecret).toHaveBeenCalled());
  await waitFor(() => expect(channelsApi.get).toHaveBeenCalledTimes(2));
});

test('rotating the inbound secret asks first: the old one stops at once', async () => {
  channelsApi.get.mockResolvedValue({ ...channel('email'), config: withChannel.channel });
  api.rotateEmailSecret.mockResolvedValue({ inbound_secret: 'k.secret', inbound_url: 'https://desk.example.com/x' });
  show(EMAIL);
  fireEvent.click(await screen.findByRole('button', { name: 'Rotate inbound secret' }));
  const question = screen.getByRole('alertdialog', { name: 'Rotate the inbound secret?' });
  expect(question).toHaveTextContent('stops working at once');
  expect(api.rotateEmailSecret).not.toHaveBeenCalled();
  fireEvent.click(within(question).getByRole('button', { name: 'Rotate secret' }));
  await waitFor(() => expect(api.rotateEmailSecret).toHaveBeenCalled());
});

test('opening held messages says how many opened and links to them; discarding says how many went', async () => {
  channelsApi.get.mockResolvedValue({ ...channel('email', { held_count: 3 }), config: withChannel.channel });
  channelsApi.replayHeld.mockResolvedValue({ results: [1, 2, 3].map(() => ({ status: 'created', conversation_id: null, message_id: null })) });
  channelsApi.discardHeld.mockResolvedValue({ discarded: 3 });
  show(EMAIL);
  fireEvent.click(await screen.findByRole('button', { name: 'Open them as conversations' }));
  const opened = await screen.findByText(/3 messages opened as conversations/);
  expect(within(opened.closest('.notice') as HTMLElement).getByRole('link', { name: 'View them in the inbox' })).toHaveAttribute('href', '/?channel=email');
  fireEvent.click(screen.getByRole('button', { name: 'Discard 3 messages…' }));
  fireEvent.click(within(screen.getByRole('alertdialog')).getByRole('button', { name: 'Discard' }));
  expect(await screen.findByText('3 messages discarded.')).toBeInTheDocument();
  expect(screen.queryByText(/3 messages opened/)).toBeNull();
});

test('after turning a channel off, focus is back on its switch', async () => {
  channelsApi.list.mockResolvedValue({ items: [channel('web_form')], types: types(0, 1) });
  channelsApi.update.mockResolvedValue({ ...channel('web_form', { enabled: false }), config: webFormConfig });
  show('');
  const toggle = await screen.findByRole('switch', { name: 'Web form' });
  fireEvent.click(toggle);
  fireEvent.click(within(screen.getByRole('alertdialog')).getByRole('button', { name: 'Turn off' }));
  await waitFor(() => expect(channelsApi.update).toHaveBeenCalled());
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('switch', { name: 'Web form' })));
});

test('messages held while off can be opened as conversations, or discarded after a question', async () => {
  channelsApi.get.mockResolvedValue({ ...channel('email', { held_count: 3 }), config: withChannel.channel });
  channelsApi.replayHeld.mockResolvedValue({ results: [] });
  channelsApi.discardHeld.mockResolvedValue({ discarded: 3 });
  show(EMAIL);
  expect(await screen.findByText('3 messages arrived while Email was off.')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Open them as conversations' }));
  await waitFor(() => expect(channelsApi.replayHeld).toHaveBeenCalledWith(EMAIL));
  fireEvent.click(screen.getByRole('button', { name: 'Discard 3 messages…' }));
  const question = screen.getByRole('alertdialog', { name: 'Discard 3 messages?' });
  expect(question).toHaveTextContent('cannot be undone');
  fireEvent.click(within(question).getByRole('button', { name: 'Discard' }));
  await waitFor(() => expect(channelsApi.discardHeld).toHaveBeenCalledWith(EMAIL));
});

test('History names what changed, never a value, and how many held messages went', async () => {
  channelsApi.list.mockResolvedValue({ items: [channel('email')], types: types(1, 0) });
  const event = (kind: string, change: Record<string, unknown>) => ({ id: crypto.randomUUID(), at: '2026-10-15T10:00:00Z', kind, channel_id: EMAIL, channel_name: 'Email',
    channel_type: 'email', actor: { agent_id: null, name: 'Sam', token_label: null }, fields: [], count: null, ...change });
  channelsApi.events.mockResolvedValue({ items: [
    event('config_changed', { fields: ['sender_name', 'support_address'] }), event('credential_changed', { fields: ['inbound_secret'] }),
    event('held_replayed', { count: 3 }), event('held_discarded', { count: 1 }),
  ], next_cursor: null });
  show('', { tab: 'history' });
  expect(await screen.findByText(/changed the sender name and support address of Email/)).toBeInTheDocument();
  expect(screen.getByText(/replaced the inbound secret of Email/)).toBeInTheDocument();
  expect(screen.getByText(/opened 3 messages Email held while off/)).toBeInTheDocument();
  expect(screen.getByText(/discarded 1 message Email held while off/)).toBeInTheDocument();
});

test('History lists who changed which channel, newest first, and pages on', async () => {
  channelsApi.list.mockResolvedValue({ items: [channel('email')], types: types(1, 0) });
  channelsApi.events.mockResolvedValueOnce({ items: [{
    id: '66666666-6666-4666-8666-666666666666', at: '2026-10-15T10:00:00Z', kind: 'disabled', channel_id: EMAIL, channel_name: 'Email', channel_type: 'email',
    actor: { agent_id: null, name: 'Sam', token_label: null },
  }], next_cursor: 'c1' }).mockResolvedValueOnce({ items: [{
    id: '77777777-7777-4777-8777-777777777777', at: '2026-10-14T10:00:00Z', kind: 'added', channel_id: EMAIL, channel_name: 'Email', channel_type: 'email',
    actor: { agent_id: null, name: null, token_label: 'Codex' },
  }], next_cursor: null });
  show('', { tab: 'history' });
  expect(await screen.findByText('turned Email off', { exact: false })).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Show more' }));
  expect(await screen.findByText(/added Email/)).toBeInTheDocument();
  expect(channelsApi.events).toHaveBeenLastCalledWith(undefined, 'c1');
});
