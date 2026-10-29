import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { beforeAll, beforeEach, expect, test, vi } from 'vitest';
import { ChannelsRoute } from './channels-route';
import type { ChannelsSearch } from './channels-route';

// Settings → Email: the status table of the workspace's support addresses and the four-step setup.
const mocks = vi.hoisted(() => {
  class InvalidRequestError extends Error {}
  class ConflictError extends InvalidRequestError { detail?: Record<string, unknown>; }
  const api = { me: vi.fn(), config: vi.fn(), updateWorkspaceSlug: vi.fn(), messages: vi.fn() };
  const channelsApi = { list: vi.fn(), get: vi.fn(), create: vi.fn(), update: vi.fn(), remove: vi.fn(), events: vi.fn(), email: vi.fn(), emailSecret: vi.fn(),
    emailVerifyDomain: vi.fn(), emailConnectGmail: vi.fn(), emailDisconnectGmail: vi.fn(), emailConnectMicrosoft: vi.fn(), emailDisconnectMicrosoft: vi.fn(),
    emailTestMessage: vi.fn(), emailForwardingConfirmation: vi.fn(), replayHeld: vi.fn(), discardHeld: vi.fn() };
  return { api, channelsApi, InvalidRequestError, ConflictError };
});
vi.mock('../../../api/client', () => ({ api: mocks.api, InvalidRequestError: mocks.InvalidRequestError, ConflictError: mocks.ConflictError }));
vi.mock('../../../api/channels', () => ({ channelsApi: mocks.channelsApi }));
const { api, channelsApi } = mocks;

const MAIN = '44444444-4444-4444-8444-444444444444';
const BILLING = '66666666-6666-4666-8666-666666666666';
const CONVERSATION = '77777777-7777-4777-8777-777777777777';
const me = (role: 'admin' | 'agent' = 'admin') => ({
  agent: { id: '11111111-1111-4111-8111-111111111111', email: 'sam@example.com', display_name: '', role, timezone_override: null },
  workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'acme', name: 'Acme', timezone: 'UTC' }, timezone: 'UTC', plan: null,
});
const waiting = { state: 'waiting', reason_code: 'no_message_yet', message: null };
const working = { state: 'working', reason_code: null, message: null };
const channel = (id: string, address: string, change: Record<string, unknown> = {}) => ({
  id, type: 'email', name: address, enabled: true, is_default: id === MAIN, status: working, summary: address,
  capabilities: ['threads'], held_count: 0, unsent_count: 0, last_inbound_at: '2026-10-29T00:33:00Z',
  created_at: '2026-10-14T08:00:00Z', updated_at: '2026-10-14T09:00:00Z', ...change,
});
const emailConfig = (address: string, change: Record<string, unknown> = {}) => ({
  support_address: address, sender_name: 'Acme Support', secret_set: false, secret_rotated_at: null, domain: address.split('@')[1], domain_verified_at: null,
  dns_record_name: `_colliedesk.${address.split('@')[1]}`, dns_record_value: 'colliedesk-verification=abc', dns_records: [], dns_records_expired_at: null,
  created_at: '2026-10-14T08:00:00Z', updated_at: '2026-10-14T09:00:00Z', ...change,
});
const connection = (provider: 'google' | 'microsoft', address: string) => ({ provider, address, status: 'connected', error_code: null, error: null, failed_at: null, connected_at: '2026-10-20T00:00:00Z' });
const settings = (address: string | null, change: Record<string, unknown> = {}) => ({
  channel: address ? emailConfig(address) : null, inbound_url: 'https://app.example.com/api/v1/channels/email/inbound',
  smtp: { configured: true, host: 'smtp.example.net', port: 587, starttls: true, authenticated: true },
  from_policy: 'verified', sending_from: 'reply@mail.example.com', hosted_address: 'support@acme.colliedesk.test', domain_verification: 'txt', sending_pause: null,
  gmail: { available: true, missing_settings: [], redirect_uri: 'https://app.example.com/api/v1/channels/email/gmail/callback', connection: null },
  microsoft: { available: true, missing_settings: [], redirect_uri: 'https://app.example.com/api/v1/channels/email/microsoft/callback',
    admin_consent_redirect_uri: 'https://app.example.com/api/v1/channels/email/microsoft/admin-consent', admin_consent_url: null,
    large_attachment_bytes: 3145728, server_credentials_refused: false, connection: null },
  ...change,
});
const types = (count: number, defaultID: string | null = count ? MAIN : null) => [
  { type: 'email', available: true, count, default_channel_id: defaultID }, { type: 'web_form', available: true, count: 0, default_channel_id: null },
];

// The email panels are their own chunk; loaded once here, so the first test does not wait on the import.
beforeAll(async () => { await import('../../../channels/email/panels'); });

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  sessionStorage.clear();
  api.me.mockResolvedValue(me());
  api.config.mockResolvedValue({ billing: { enabled: false }, cloud_sign_in: { enabled: false, publishable_key: null }, product_analytics: { enabled: false, key: null, host: null, realm: 'self-hosted' } });
  api.messages.mockResolvedValue({ items: [] });
  channelsApi.events.mockResolvedValue({ items: [], next_cursor: null });
  channelsApi.emailForwardingConfirmation.mockRejectedValue(new Error('API 404'));
});

// waitingChannel is a channel whose support address is saved and that has received nothing yet.
function waitingChannel(change: Record<string, unknown> = {}) {
  const detail = { ...channel(MAIN, 'support@acme.com', { status: waiting, last_inbound_at: null }), config: emailConfig('support@acme.com'), ...change };
  channelsApi.list.mockResolvedValue({ items: [detail], types: types(1) });
  channelsApi.get.mockResolvedValue(detail);
  channelsApi.email.mockResolvedValue(settings('support@acme.com'));
  return detail;
}

function show(path: string, search: ChannelsSearch = {}, navigate = vi.fn()) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><ChannelsRoute path={path} search={search} navigate={navigate} /></QueryClientProvider>);
  return { navigate, client };
}

const step = (name: RegExp) => screen.getByRole('group', { name });

test('the Email home of a workspace without an email address is the setup, at step 1 of 4, later steps waiting their turn', async () => {
  channelsApi.list.mockResolvedValue({ items: [], types: types(0) });
  show('type/email');
  expect(await screen.findByRole('heading', { level: 1, name: 'Email' })).toBeInTheDocument();
  expect(await screen.findByRole('heading', { name: 'Connect your support email' })).toBeInTheDocument();
  const first = step(/Your support address/);
  expect(first).toHaveTextContent('Step 1 of 4');
  expect(within(first).getByRole('textbox', { name: 'Support address' })).toBeInTheDocument();
  for (const later of [/Forward its mail to Colliedesk/, /Send replies from/, /Try it with a test message/]) expect(step(later)).toHaveAttribute('aria-disabled', 'true');
});

test('saving the support address of a new workspace creates the channel and continues its setup at step 2', async () => {
  channelsApi.list.mockResolvedValue({ items: [], types: types(0) });
  channelsApi.create.mockResolvedValue({ ...channel(MAIN, 'help@acme.com'), config: emailConfig('help@acme.com') });
  const { navigate } = show('type/email');
  fireEvent.change(await screen.findByRole('textbox', { name: 'Support address' }), { target: { value: ' help@acme.com ' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save support address' }));
  await waitFor(() => expect(channelsApi.create).toHaveBeenCalledWith({ type: 'email', config: { support_address: 'help@acme.com' } }, expect.any(String)));
  await waitFor(() => expect(navigate).toHaveBeenCalledWith(`/settings/channels/${MAIN}/setup`, true));
});

test('a new cloud workspace whose only address is its forwarding address starts at step 1 on that channel, and saving changes it', async () => {
  const hosted = { ...channel(MAIN, 'support@acme.colliedesk.test', { status: waiting, last_inbound_at: null }), config: emailConfig('support@acme.colliedesk.test') };
  channelsApi.list.mockResolvedValue({ items: [hosted], types: types(1) });
  channelsApi.get.mockResolvedValue(hosted);
  channelsApi.email.mockResolvedValue(settings('support@acme.colliedesk.test'));
  channelsApi.update.mockResolvedValue({ ...hosted, config: emailConfig('help@acme.com') });
  show('type/email');
  const field = await screen.findByRole('textbox', { name: 'Support address' });
  expect(field).toHaveValue('');
  fireEvent.change(field, { target: { value: 'help@acme.com' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save support address' }));
  await waitFor(() => expect(channelsApi.update).toHaveBeenCalledWith(MAIN, { config: { support_address: 'help@acme.com' } }));
  expect(channelsApi.create).not.toHaveBeenCalled();
});

test('the Email home lists every address with how it receives, how replies leave, its state, and the way into it', async () => {
  const main = { ...channel(MAIN, 'support@acme.com'), config: emailConfig('support@acme.com') };
  const billing = channel(BILLING, 'billing@acme.com', { status: waiting, last_inbound_at: null, is_default: false });
  channelsApi.list.mockResolvedValue({ items: [main, billing], types: types(2) });
  channelsApi.email.mockImplementation((id: string) => Promise.resolve(id === MAIN
    ? settings('support@acme.com', { gmail: { ...settings(null).gmail, connection: connection('google', 'support@acme.com') } })
    : settings('billing@acme.com')));
  show('type/email');
  const table = await screen.findByRole('table', { name: 'Support addresses' });
  expect(within(table).getAllByRole('columnheader').map((cell) => cell.textContent)).toEqual(['Support address', 'Receives by', 'Replies through', 'Status', 'Action']);
  const [, first, second] = within(table).getAllByRole('row');
  expect(first).toHaveTextContent('support@acme.com');
  expect(first).toHaveTextContent('Default');
  await waitFor(() => expect(first).toHaveTextContent('Gmail'));
  expect(first).toHaveTextContent('support@acme.colliedesk.test');
  expect(first).toHaveTextContent('Working');
  expect(within(first).getByRole('link', { name: 'Manage support@acme.com' })).toHaveAttribute('href', `/settings/channels/${MAIN}`);
  expect(second).toHaveTextContent('Waiting for first email');
  expect(within(second).getByRole('link', { name: 'Continue setup of billing@acme.com' })).toHaveAttribute('href', `/settings/channels/${BILLING}/setup`);
  expect(screen.getByRole('link', { name: 'Add another address' })).toHaveAttribute('href', '/settings/channels/new/email');
});

test('the rarely used settings are folded under the table; the workspace address name is in Change forwarding address, not on the channel list', async () => {
  const main = { ...channel(MAIN, 'support@acme.com'), config: emailConfig('support@acme.com') };
  channelsApi.list.mockResolvedValue({ items: [main], types: types(1) });
  channelsApi.get.mockResolvedValue(main);
  channelsApi.email.mockResolvedValue(settings('support@acme.com'));
  show('type/email');
  await screen.findByRole('table', { name: 'Support addresses' });
  const folded = Array.from(document.querySelectorAll('details.email-home-fold > summary')).map((summary) => summary.textContent);
  expect(folded).toEqual(['Sender name', 'Your domain · acme.com not verified', 'Send a test message', 'Change forwarding address or disconnect']);
  const change = screen.getByText('Change forwarding address or disconnect').closest('details') as HTMLElement;
  expect(within(change).getByRole('textbox', { name: 'Workspace address name' })).toHaveValue('acme');
  expect(within(change).getByRole('link', { name: 'Manage support@acme.com' })).toHaveAttribute('href', `/settings/channels/${MAIN}`);
  const sender = screen.getByText('Sender name', { selector: 'summary' }).closest('details') as HTMLElement;
  expect(within(sender).getByRole('textbox', { name: 'Sender name' })).toHaveValue('Acme Support');
});

test('the channel list no longer carries the workspace address name', async () => {
  const main = { ...channel(MAIN, 'support@acme.com'), config: emailConfig('support@acme.com') };
  channelsApi.list.mockResolvedValue({ items: [main], types: types(1) });
  channelsApi.email.mockResolvedValue(settings('support@acme.com'));
  show('');
  expect((await screen.findAllByText('support@acme.com')).length).toBeGreaterThan(0);
  expect(screen.queryByRole('textbox', { name: 'Workspace address name' })).toBeNull();
});

test('step 2 gives the forwarding address to copy first, then provider tabs with numbered steps; Continue waits for the first email, Skip never does', async () => {
  waitingChannel();
  show(`${MAIN}/setup`);
  const forward = await screen.findByRole('group', { name: /Forward its mail to Colliedesk/ });
  expect(forward).toHaveTextContent('Step 2 of 4');
  expect(within(forward).getByRole('textbox', { name: 'Forward to this address' })).toHaveValue('support@acme.colliedesk.test');
  expect(within(forward).getByRole('button', { name: 'Copy' })).toHaveClass('primary-action');
  expect(within(forward).getAllByRole('tab').map((tab) => tab.textContent)).toEqual(['Gmail', 'Google Workspace', 'Microsoft 365', 'Other / own server']);
  const gmail = within(forward).getByRole('tabpanel');
  expect(within(gmail).getAllByRole('listitem').length).toBeGreaterThanOrEqual(4);
  expect(within(gmail).getByRole('link', { name: /Open Gmail/ })).toHaveAttribute('target', '_blank');
  fireEvent.click(within(forward).getByRole('tab', { name: 'Microsoft 365' }));
  expect(within(forward).getByRole('tabpanel')).toHaveTextContent('5.7.520');
  expect(within(forward).getByRole('status')).toHaveTextContent('Waiting for first email');
  expect(within(forward).getByRole('button', { name: 'Continue' })).toBeDisabled();
  expect(within(forward).getByText('Continue unlocks when the first email arrives.')).toBeInTheDocument();
  expect(within(forward).getByRole('button', { name: 'Skip for now' })).toBeEnabled();
});

test('step 2 flips to First email received when inbox activity brings a conversation, without a reload, and Continue opens step 3', async () => {
  const detail = waitingChannel();
  const { client } = show(`${MAIN}/setup`);
  const forward = await screen.findByRole('group', { name: /Forward its mail to Colliedesk/ });
  expect(within(forward).getByRole('status')).toHaveTextContent('Waiting for first email');
  channelsApi.get.mockResolvedValue({ ...detail, status: working, last_inbound_at: '2026-10-29T00:33:00Z' });
  act(() => { client.setQueryData(['inbox-activity'], { now: '2026-10-29T00:33:05Z', new_conversations: [{ id: CONVERSATION, created_at: '2026-10-29T00:33:00Z' }], assigned_to_me: [], updated: [], viewing: [] }); });
  await waitFor(() => expect(within(forward).getByRole('status')).toHaveTextContent('First email received 12:33 AM'));
  fireEvent.click(within(forward).getByRole('button', { name: 'Continue' }));
  expect(step(/Send replies from support@acme.com/)).toHaveTextContent('Step 3 of 4');
});

test('Skip for now leaves step 2 for step 3 and is remembered; the skipped step can be opened again', async () => {
  waitingChannel();
  show(`${MAIN}/setup`);
  fireEvent.click(await screen.findByRole('button', { name: 'Skip for now' }));
  expect(step(/Send replies from/)).toHaveTextContent('Step 3 of 4');
  expect(step(/Forward its mail to Colliedesk/)).toHaveTextContent('Skipped');
  expect(JSON.parse(localStorage.getItem(`colliedesk.email-setup.${MAIN}`) ?? '{}')).toMatchObject({ skipped: ['forward'] });
  fireEvent.click(within(step(/Forward its mail to Colliedesk/)).getByRole('button', { name: 'Set up' }));
  expect(step(/Forward its mail to Colliedesk/)).toHaveTextContent('Step 2 of 4');
});

test('step 3 recommends the reply choice that matches the provider tab picked in step 2', async () => {
  waitingChannel();
  show(`${MAIN}/setup`);
  fireEvent.click(await screen.findByRole('tab', { name: 'Microsoft 365' }));
  fireEvent.click(screen.getByRole('button', { name: 'Skip for now' }));
  const replies = step(/Send replies from/);
  expect(within(replies).getByRole('radio', { name: /Microsoft 365/ })).toBeChecked();
  expect(within(replies).getByRole('radio', { name: /Microsoft 365/ }).closest('label')).toHaveTextContent('Recommended');
  expect(within(replies).getByRole('radio', { name: /^Gmail/ }).closest('label')).not.toHaveTextContent('Recommended');
  expect(within(replies).getByRole('button', { name: 'Connect Microsoft 365' })).toBeInTheDocument();
  fireEvent.click(within(replies).getByRole('radio', { name: /Colliedesk sends/ }));
  expect(within(replies).getByRole('textbox', { name: 'TXT record name' })).toHaveValue('_colliedesk.acme.com');
  expect(within(replies).getByRole('button', { name: 'Skip for now — replies go from reply@mail.example.com' })).toBeEnabled();
});

test('the Other tab recommends Colliedesk sends; Gmail and Google Workspace recommend Gmail', async () => {
  waitingChannel();
  localStorage.setItem(`colliedesk.email-setup.${MAIN}`, JSON.stringify({ provider: 'other', skipped: ['forward'] }));
  show(`${MAIN}/setup`);
  expect(await screen.findByRole('radio', { name: /Colliedesk sends/ })).toBeChecked();
  localStorage.setItem(`colliedesk.email-setup.${MAIN}`, JSON.stringify({ provider: 'workspace', skipped: ['forward'] }));
  show(`${MAIN}/setup`);
  await waitFor(() => expect(screen.getAllByRole('radio', { name: /^Gmail/ }).at(-1)).toBeChecked());
});

test('a server without forwarding addresses opens Other / own server, where the inbound secret is created', async () => {
  waitingChannel();
  channelsApi.email.mockResolvedValue(settings('support@acme.com', { hosted_address: null }));
  channelsApi.emailSecret.mockResolvedValue({ inbound_secret: 'abc.def', inbound_url: 'https://app.example.com/api/v1/channels/email/inbound' });
  show(`${MAIN}/setup`);
  const forward = await screen.findByRole('group', { name: /Forward its mail to Colliedesk/ });
  expect(within(forward).getByRole('tab', { name: 'Other / own server' })).toHaveAttribute('aria-selected', 'true');
  expect(within(forward).getByRole('textbox', { name: 'Inbound URL' })).toHaveValue('https://app.example.com/api/v1/channels/email/inbound');
  fireEvent.click(within(forward).getByRole('button', { name: 'Create inbound secret' }));
  expect(await within(forward).findByRole('textbox', { name: /Inbound secret/ })).toHaveValue('abc.def');
});

test('step 4 sends the test message, remembers its conversation, and is done once the reply to it is delivered', async () => {
  waitingChannel({ status: working, last_inbound_at: '2026-10-29T00:33:00Z' });
  channelsApi.email.mockResolvedValue(settings('support@acme.com', { sending_from: 'support@acme.com' }));
  channelsApi.emailTestMessage.mockResolvedValue({ conversation_id: CONVERSATION, message_id: '88888888-8888-4888-8888-888888888888', sent_to: 'sam@example.com' });
  const { navigate } = show(`${MAIN}/setup`);
  const test = await screen.findByRole('group', { name: /Try it with a test message/ });
  expect(test).toHaveTextContent('Step 4 of 4');
  fireEvent.click(within(test).getByRole('button', { name: 'Send yourself a test message' }));
  await waitFor(() => expect(navigate).toHaveBeenCalledWith(`/conversations/${CONVERSATION}`));
  expect(JSON.parse(localStorage.getItem(`colliedesk.email-setup.${MAIN}`) ?? '{}')).toMatchObject({ testConversation: CONVERSATION });
});

test('the test step reads Reply delivered once an outgoing reply on the test conversation was sent', async () => {
  waitingChannel({ status: working, last_inbound_at: '2026-10-29T00:33:00Z' });
  channelsApi.email.mockResolvedValue(settings('support@acme.com', { sending_from: 'support@acme.com' }));
  localStorage.setItem(`colliedesk.email-setup.${MAIN}`, JSON.stringify({ testConversation: CONVERSATION }));
  api.messages.mockResolvedValue({ items: [{ kind: 'incoming', delivery: null }, { kind: 'outgoing', delivery: { status: 'sent' } }] });
  show(`${MAIN}/setup`);
  const test = await screen.findByRole('group', { name: /Try it with a test message/ });
  await waitFor(() => expect(test).toHaveTextContent('Reply delivered'));
  expect(api.messages).toHaveBeenCalledWith(CONVERSATION);
  expect(screen.getByRole('status', { name: 'Email is set up' })).toBeInTheDocument();
});

test('a done step shows what it holds and opens again with Change', async () => {
  waitingChannel();
  show(`${MAIN}/setup`);
  const address = await screen.findByRole('group', { name: /Your support address/ });
  expect(address).toHaveTextContent('support@acme.com');
  fireEvent.click(within(address).getByRole('button', { name: 'Change' }));
  expect(within(step(/Your support address/)).getByRole('textbox', { name: 'Support address' })).toHaveValue('support@acme.com');
});

test('Customers see the To line and the From line replies carry, from live values', async () => {
  waitingChannel();
  show(`${MAIN}/setup`);
  const preview = await screen.findByRole('complementary', { name: 'Customers see' });
  await waitFor(() => expect(preview).toHaveTextContent('To: support@acme.com'));
  expect(preview).toHaveTextContent('Replies from: Acme Support <reply@mail.example.com>');
});

test('a teammate reads the setup without the forms that change it', async () => {
  api.me.mockResolvedValue(me('agent'));
  channelsApi.list.mockResolvedValue({ items: [], types: types(0) });
  show('type/email');
  expect(await screen.findByRole('heading', { name: 'Connect your support email' })).toBeInTheDocument();
  expect(screen.getByText(/Only an admin can connect email/)).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Save support address' })).toBeNull();
});

test('back from Google during the setup, the channel page sends the browser back into the setup with the outcome', async () => {
  waitingChannel();
  sessionStorage.setItem('colliedesk.email-setup.return', MAIN);
  const { navigate } = show(MAIN, { tab: 'replies', gmail: 'connected' });
  await waitFor(() => expect(navigate).toHaveBeenCalledWith(`/settings/channels/${MAIN}/setup?gmail=connected`, true));
});

test('step 2 waits for Gmail\'s confirmation email, then shows its code, the confirm link in a new tab and its conversation', async () => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  try {
    waitingChannel();
    show(`${MAIN}/setup`);
    const forward = await screen.findByRole('group', { name: /Forward its mail to Colliedesk/ });
    const box = within(forward).getByRole('region', { name: "Gmail's confirmation email" });
    expect(box).toHaveTextContent('Waiting');
    await waitFor(() => expect(channelsApi.emailForwardingConfirmation).toHaveBeenCalledWith(MAIN));
    channelsApi.emailForwardingConfirmation.mockResolvedValue({ code: '482913557', confirm_url: 'https://mail-settings.google.com/mail/vf-abc',
      received_at: '2026-10-29T00:31:00Z', conversation_id: CONVERSATION });
    await act(async () => { await vi.advanceTimersByTimeAsync(5_000); });
    await waitFor(() => expect(box).toHaveTextContent('Confirmation code 482913557'));
    const link = within(box).getByRole('link', { name: /Open confirm link/ });
    expect(link).toHaveAttribute('href', 'https://mail-settings.google.com/mail/vf-abc');
    expect(link).toHaveAttribute('target', '_blank');
    expect(link).toHaveAttribute('rel', 'noreferrer');
    expect(within(box).getByRole('link', { name: 'Open its conversation' })).toHaveAttribute('href', `/conversations/${CONVERSATION}`);
    // Found: the step stops asking.
    const calls = channelsApi.emailForwardingConfirmation.mock.calls.length;
    await act(async () => { await vi.advanceTimersByTimeAsync(15_000); });
    expect(channelsApi.emailForwardingConfirmation.mock.calls.length).toBe(calls);
  } finally { vi.useRealTimers(); }
});

test('a confirmation that failed the sender check shows its code and conversation, never a confirm link', async () => {
  waitingChannel();
  channelsApi.emailForwardingConfirmation.mockResolvedValue({ code: '482913557', confirm_url: null, received_at: '2026-10-29T00:31:00Z', conversation_id: CONVERSATION });
  show(`${MAIN}/setup`);
  const box = await screen.findByRole('region', { name: "Gmail's confirmation email" });
  await waitFor(() => expect(box).toHaveTextContent('482913557'));
  expect(within(box).queryByRole('link', { name: /Open confirm link/ })).toBeNull();
  expect(within(box).getByRole('link', { name: 'Open its conversation' })).toBeInTheDocument();
});

test('the confirmation is asked for only while step 2 is open: never on another step or another provider', async () => {
  waitingChannel();
  localStorage.setItem(`colliedesk.email-setup.${MAIN}`, JSON.stringify({ provider: 'microsoft', skipped: ['forward'] }));
  show(`${MAIN}/setup`);
  await screen.findByRole('group', { name: /Send replies from/ });
  expect(channelsApi.emailForwardingConfirmation).not.toHaveBeenCalled();
});
