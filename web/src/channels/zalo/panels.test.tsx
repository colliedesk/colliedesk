import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import type { ReactElement } from 'react';
import { beforeEach, expect, test, vi } from 'vitest';
import type { ChannelDetail } from '../../api/channels';

// The Zalo OA channel's connect form and tabs (docs/user/zalo.md).
const mocks = vi.hoisted(() => {
  class InvalidRequestError extends Error {
    detail?: Record<string, unknown>;
    constructor(message: string, detail?: Record<string, unknown>) { super(message); this.detail = detail; }
  }
  class ConflictError extends InvalidRequestError {}
  const channelsApi = { setCredential: vi.fn(), threads: vi.fn(), senders: vi.fn(), update: vi.fn() };
  const numberSeriesApi = { list: vi.fn(), set: vi.fn() };
  const api = { me: vi.fn() };
  return { channelsApi, numberSeriesApi, api, InvalidRequestError, ConflictError };
});
vi.mock('../../api/client', () => ({ api: mocks.api, InvalidRequestError: mocks.InvalidRequestError, ConflictError: mocks.ConflictError }));
vi.mock('../../api/channels', () => ({ channelsApi: mocks.channelsApi }));
vi.mock('../../api/number-series', async (actual) => ({ ...(await actual<typeof import('../../api/number-series')>()), numberSeriesApi: mocks.numberSeriesApi }));
const { channelsApi } = mocks;
const { Connect, Tabs } = await import('./panels');

const ID = '44444444-4444-4444-8444-444444444444';
const credentials = (set: Partial<Record<'app_secret' | 'oa_secret_key' | 'access_token' | 'refresh_token', string>>) =>
  (['app_secret', 'oa_secret_key', 'access_token', 'refresh_token'] as const).map((name) => ({ name, hint: set[name] ?? null, set_at: set[name] ? '2026-10-30T03:42:00Z' : null }));
const channel = (config: Record<string, unknown> = {}, change: Partial<ChannelDetail> = {}): ChannelDetail => ({
  id: ID, type: 'zalo', name: 'Hảo Đạt OA', enabled: true, status: { state: 'working', reason_code: null, message: null }, summary: 'OA 4318…2207 · 1 group linked',
  capabilities: ['threads'], held_count: 0, unsent_count: 0, last_inbound_at: null, created_at: '2026-10-30T00:00:00Z', updated_at: '2026-10-30T00:00:00Z',
  config: { oa_id: '4318002291772207', app_id: '1188200455', staff_user_ids: [], groups: {}, webhook_path: '/api/v1/channels/zalo/webhook',
    credentials: credentials({ app_secret: 'a1f9', oa_secret_key: '7c20' }), ...config },
  settings: {}, effective_settings: {}, ...change,
} as ChannelDetail);
const sender = (value: string, name: string | null, place: string | null = null, role: 'customer' | 'staff' = 'customer') => ({
  kind: 'zalo_user', value, name, role, last_at: '2026-10-31T05:00:00Z', thread_id: crypto.randomUUID(), thread_name: place,
});
const item = (sender: { value: string; name: string }) => ({ sender: { kind: 'zalo_user', ...sender } });
const thread = (kind: 'direct' | 'group', external: string, sender?: { value: string; name: string }) => ({
  id: crypto.randomUUID(), channel_id: ID, kind, external_id: external, name: null, contact_id: null, opens_conversations: true,
  last_item_at: '2026-10-31T05:00:00Z', last_item: sender ? item(sender) : null,
});

const view = (ui: ReactElement) => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
};

beforeEach(() => {
  vi.clearAllMocks();
  channelsApi.threads.mockResolvedValue({ items: [], next_cursor: null });
  channelsApi.senders.mockResolvedValue({ items: [] });
  mocks.api.me.mockResolvedValue({ timezone: 'UTC', workspace: { timezone: 'UTC' } });
  mocks.numberSeriesApi.list.mockResolvedValue({ items: [
    { series: 'default', format: '#{n}', next: 13, start: 1, start_year: null, updated_at: '2026-10-31T00:00:00Z' },
    { series: 'chd', format: 'CHD-{yyyy}-{n:04}', next: 143, start: 142, start_year: 2026, updated_at: '2026-10-31T00:00:00Z' },
  ] });
  channelsApi.update.mockResolvedValue(channel());
  channelsApi.setCredential.mockResolvedValue({ name: 'x', hint: 'abcd' });
});

test('connect checks the ids, adds the OA with them, then stores its two secrets before the page opens', async () => {
  const create = vi.fn(async (_input: unknown, beforeOpen?: (created: ChannelDetail) => Promise<void>) => { await beforeOpen?.(channel()); return channel(); });
  view(<Connect canManage timezone="UTC" create={create} />);
  fireEvent.change(screen.getByLabelText('OA ID'), { target: { value: '43 18' } });
  fireEvent.click(screen.getByRole('button', { name: 'Connect Zalo OA' }));
  expect(screen.getByLabelText('OA ID')).toHaveAccessibleDescription(/Digits only/);
  expect(screen.getByLabelText('App ID')).toHaveAccessibleDescription(/Required/);
  expect(create).not.toHaveBeenCalled();
  fireEvent.change(screen.getByLabelText('OA ID'), { target: { value: '4318002291772207' } });
  fireEvent.change(screen.getByLabelText('App ID'), { target: { value: '1188200455' } });
  fireEvent.change(screen.getByLabelText('App secret'), { target: { value: 'app-secret-a1f9' } });
  fireEvent.change(screen.getByLabelText('OA secret key'), { target: { value: 'oa-key-7c20' } });
  fireEvent.click(screen.getByRole('button', { name: 'Connect Zalo OA' }));
  await waitFor(() => expect(channelsApi.setCredential).toHaveBeenCalledTimes(2));
  expect(create.mock.calls[0][0]).toEqual({ enabled: true, config: { oa_id: '4318002291772207', app_id: '1188200455' } });
  expect(channelsApi.setCredential).toHaveBeenCalledWith(ID, 'app_secret', 'app-secret-a1f9');
  expect(channelsApi.setCredential).toHaveBeenCalledWith(ID, 'oa_secret_key', 'oa-key-7c20');
});

test('a secret that could not be stored on the new channel does not stop the others, and Connection says which and why', async () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  channelsApi.setCredential.mockImplementation(async (_id: string, name: string) => {
    if (name === 'app_secret') throw new mocks.ConflictError('Secrets cannot be stored: this server has no MAILBOX_TOKEN_KEYS.', { code: 'secret_keys_missing' });
    return { name, hint: '7c20' };
  });
  const create = vi.fn(async (_input: unknown, beforeOpen?: (created: ChannelDetail) => Promise<void>) => { await beforeOpen?.(channel()); return channel(); });
  const { unmount } = render(<QueryClientProvider client={client}><Connect canManage timezone="UTC" create={create} /></QueryClientProvider>);
  fireEvent.change(screen.getByLabelText('OA ID'), { target: { value: '4318002291772207' } });
  fireEvent.change(screen.getByLabelText('App ID'), { target: { value: '1188200455' } });
  fireEvent.change(screen.getByLabelText('App secret'), { target: { value: 'app-secret-a1f9' } });
  fireEvent.change(screen.getByLabelText('OA secret key'), { target: { value: 'oa-key-7c20' } });
  fireEvent.click(screen.getByRole('button', { name: 'Connect Zalo OA' }));
  await waitFor(() => expect(create).toHaveBeenCalled());
  await waitFor(() => expect(channelsApi.setCredential).toHaveBeenCalledWith(ID, 'oa_secret_key', 'oa-key-7c20'));
  unmount();
  const unset = channel({ credentials: credentials({ oa_secret_key: '7c20' }) });
  const { rerender } = render(<QueryClientProvider client={client}><Tabs.connection channel={unset} canManage timezone="UTC" /></QueryClientProvider>);
  expect(screen.getByRole('alert')).toHaveTextContent('Hảo Đạt OA was added, but the app secret could not be saved: Secrets cannot be stored: this server has no MAILBOX_TOKEN_KEYS.');
  // Once it is set, the note has done its work.
  rerender(<QueryClientProvider client={client}><Tabs.connection channel={channel()} canManage timezone="UTC" /></QueryClientProvider>);
  expect(screen.queryByRole('alert')).toBeNull();
});

test('an OA connected to another channel, here or in another workspace, is said to be connected elsewhere', async () => {
  const create = vi.fn().mockRejectedValue(new mocks.ConflictError('That account is already connected to a channel.', { code: 'channel_account_taken' }));
  view(<Connect canManage timezone="UTC" create={create} />);
  fireEvent.change(screen.getByLabelText('OA ID'), { target: { value: '4318002291772207' } });
  fireEvent.change(screen.getByLabelText('App ID'), { target: { value: '1188200455' } });
  fireEvent.click(screen.getByRole('button', { name: 'Connect Zalo OA' }));
  await waitFor(() => expect(screen.getByLabelText('OA ID')).toHaveAccessibleDescription(/This OA is connected elsewhere\./));
});

test('connection shows the webhook, the ids and which secrets are set, never a value', () => {
  view(<Tabs.connection channel={channel()} canManage timezone="UTC" />);
  expect(screen.getByText(`${window.location.origin}/api/v1/channels/zalo/webhook`)).toBeInTheDocument();
  expect(screen.getByText('4318002291772207')).toBeInTheDocument();
  expect(screen.getByText(/Set · ••••a1f9/)).toBeInTheDocument();
  expect(screen.getByText('Not set: messages arrive, replies are not sent')).toBeInTheDocument();
  expect(screen.getByRole('status')).toHaveTextContent('Paste this webhook URL in your Zalo app, then add the tokens.');
  expect(screen.getByRole('button', { name: 'Add tokens' })).toBeInTheDocument();
  expect(screen.getAllByRole('button', { name: 'Replace' })).toHaveLength(2);
  expect(screen.getByText('Running')).toBeInTheDocument();
});

test('connection says since when processing is paused and how many messages are kept', () => {
  view(<Tabs.connection channel={channel({}, { processing_paused_at: '2026-10-31T07:05:00Z', paused_count: 12 })} canManage timezone="UTC" />);
  expect(screen.getByText(/^Paused since .* · 12 messages kept$/)).toBeInTheDocument();
});

test('replacing a secret stores the new one and says so', async () => {
  view(<Tabs.connection channel={channel()} canManage timezone="UTC" />);
  fireEvent.click(screen.getAllByRole('button', { name: 'Replace' })[0]);
  const dialog = screen.getByRole('dialog', { name: 'Replace the OA secret key of Hảo Đạt OA' });
  expect(within(dialog).getByText('••••7c20')).toBeInTheDocument();
  fireEvent.change(within(dialog).getByLabelText('New OA secret key'), { target: { value: 'new-key' } });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Replace' }));
  expect(await screen.findByText('OA secret key saved.')).toBeInTheDocument();
  expect(channelsApi.setCredential).toHaveBeenCalledWith(ID, 'oa_secret_key', 'new-key');
});

test('the tokens are saved refresh first; when only the refresh token got in, the dialog asks for the access token alone', async () => {
  channelsApi.setCredential.mockResolvedValueOnce({ name: 'refresh_token', hint: 'rrrr' }).mockRejectedValueOnce(new mocks.InvalidRequestError('Refused.'));
  view(<Tabs.connection channel={channel()} canManage timezone="UTC" />);
  fireEvent.click(screen.getByRole('button', { name: 'Add tokens' }));
  const dialog = screen.getByRole('dialog', { name: 'Add the tokens of Hảo Đạt OA' });
  fireEvent.change(within(dialog).getByLabelText('Access token'), { target: { value: 'access-1' } });
  fireEvent.change(within(dialog).getByLabelText('Refresh token'), { target: { value: 'refresh-1' } });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save tokens' }));
  expect(await within(dialog).findByRole('alert')).toHaveTextContent('The refresh token was saved; the access token was not.');
  expect(channelsApi.setCredential.mock.calls.map((call) => call[1])).toEqual(['refresh_token', 'access_token']);
  expect(within(dialog).queryByLabelText('Refresh token')).toBeNull();
  channelsApi.setCredential.mockResolvedValueOnce({ name: 'access_token', hint: 'aaaa' });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save tokens' }));
  expect(await screen.findByText('Tokens saved.')).toBeInTheDocument();
  expect(channelsApi.setCredential.mock.calls.map((call) => call[1])).toEqual(['refresh_token', 'access_token', 'access_token']);
});

test('groups lists the groups that wrote, links one to an organization and unlinks it, keeping the others', async () => {
  channelsApi.threads.mockResolvedValue({ items: [thread('group', 'group:6127000931'), thread('group', 'group:8821004410')], next_cursor: null });
  const linked = channel({ groups: { 8821004410: 'Hảo Đạt Co., Ltd.' } });
  view(<Tabs.groups channel={linked} canManage timezone="UTC" />);
  const list = await screen.findByRole('list', { name: 'Zalo groups' });
  expect(within(list).getAllByRole('listitem')).toHaveLength(2);
  expect(within(list).getByText('Not linked')).toBeInTheDocument();
  fireEvent.click(within(list).getByRole('button', { name: 'Link…' }));
  const dialog = screen.getByRole('dialog', { name: 'Link “Group 6127000931” to an organization' });
  fireEvent.change(within(dialog).getByLabelText('Organization'), { target: { value: 'HC Labs' } });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Link' }));
  await waitFor(() => expect(channelsApi.update).toHaveBeenCalledWith(ID, { config: { groups: { 8821004410: 'Hảo Đạt Co., Ltd.', 6127000931: 'HC Labs' } } }));
  fireEvent.click(within(list).getByRole('button', { name: 'Unlink Group 8821004410' }));
  await waitFor(() => expect(channelsApi.update).toHaveBeenLastCalledWith(ID, { config: { groups: {} } }));
  expect(await screen.findByText(/Unlinked “Group 8821004410”/)).toBeInTheDocument();
});

test('a group nobody wrote from yet is an empty list that says how to add one', async () => {
  view(<Tabs.groups channel={channel()} canManage timezone="UTC" />);
  expect(await screen.findByText(/No group has written to the OA yet/)).toBeInTheDocument();
});

test('staff: none says what that means; an account is added from everyone who wrote, and removed after a question', async () => {
  // A teammate who wrote earlier in a group, before a customer, is offered too.
  channelsApi.senders.mockResolvedValue({ items: [sender('6620001029', 'Nguyễn Văn A', 'Group 6127000931'), sender('7012003391', 'Long Bùi', 'Group 6127000931')] });
  const { rerender } = view(<Tabs.staff channel={channel()} canManage timezone="UTC" />);
  expect(screen.getByText(/No staff accounts yet/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Add staff account' }));
  const dialog = screen.getByRole('dialog', { name: 'Add staff account' });
  expect(await within(dialog).findByRole('radio', { name: /Nguyễn Văn A · uid 6620001029 · wrote .* in Group 6127000931/ })).toBeInTheDocument();
  fireEvent.click(await within(dialog).findByRole('radio', { name: /Long Bùi · uid 7012003391/ }));
  fireEvent.click(within(dialog).getByRole('button', { name: 'Add' }));
  await waitFor(() => expect(channelsApi.update).toHaveBeenCalledWith(ID, { config: { staff_user_ids: ['7012003391'] } }));
  expect(await screen.findByText('Long Bùi added as a staff account.')).toBeInTheDocument();
  rerender(<QueryClientProvider client={new QueryClient()}><Tabs.staff channel={channel({ staff_user_ids: ['7012003391', '9930004402'] })} canManage timezone="UTC" /></QueryClientProvider>);
  fireEvent.click(await screen.findByRole('button', { name: 'Remove Long Bùi' }));
  const ask = screen.getByRole('alertdialog', { name: 'Stop treating Long Bùi as staff?' });
  fireEvent.click(within(ask).getByRole('button', { name: 'Remove' }));
  await waitFor(() => expect(channelsApi.update).toHaveBeenLastCalledWith(ID, { config: { staff_user_ids: ['9930004402'] } }));
});

test('an account with no name is named once, by its user id', async () => {
  channelsApi.senders.mockResolvedValue({ items: [sender('7012003391', null)] });
  view(<Tabs.staff channel={channel({ staff_user_ids: ['9930004402'] })} canManage timezone="UTC" />);
  const row = screen.getByRole('listitem');
  expect(row.textContent?.match(/uid 9930004402/g)).toHaveLength(1);
  fireEvent.click(screen.getByRole('button', { name: 'Add staff account' }));
  const option = await screen.findByRole('radio', { name: /7012003391/ });
  expect(option.closest('label')?.textContent?.match(/7012003391/g)).toHaveLength(1);
});

test('people already on the staff list are not offered again, whatever their last message was taken as', async () => {
  channelsApi.senders.mockResolvedValue({ items: [sender('9930004402', 'Minh Trần', null, 'customer'), sender('5521000187', 'Lan Phạm', null, 'staff'), sender('6620001029', 'Khách A')] });
  view(<Tabs.staff channel={channel({ staff_user_ids: ['9930004402'] })} canManage timezone="UTC" />);
  expect(await screen.findByText('Minh Trần')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Add staff account' }));
  const dialog = screen.getByRole('dialog', { name: 'Add staff account' });
  expect(await within(dialog).findByRole('radio', { name: /Khách A/ })).toBeInTheDocument();
  expect(within(dialog).getByRole('radio', { name: /Lan Phạm/ })).toBeInTheDocument();
  expect(within(dialog).queryByRole('radio', { name: /Minh Trần/ })).toBeNull();
});

test('when the people who wrote cannot be loaded, the dialog says so, retries, and a pasted id still works', async () => {
  channelsApi.senders.mockRejectedValueOnce(new Error('API 500')).mockResolvedValue({ items: [sender('7012003391', 'Long Bùi')] });
  view(<Tabs.staff channel={channel()} canManage timezone="UTC" />);
  fireEvent.click(screen.getByRole('button', { name: 'Add staff account' }));
  const dialog = screen.getByRole('dialog', { name: 'Add staff account' });
  expect(await within(dialog).findByRole('alert')).toHaveTextContent('The people who wrote could not be loaded.');
  fireEvent.change(within(dialog).getByLabelText('Or paste a Zalo user id'), { target: { value: '9930004402' } });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Try again' }));
  expect(await within(dialog).findByRole('radio', { name: /Long Bùi/ })).toBeInTheDocument();
  fireEvent.click(within(dialog).getByRole('button', { name: 'Add' }));
  await waitFor(() => expect(channelsApi.update).toHaveBeenCalledWith(ID, { config: { staff_user_ids: ['9930004402'] } }));
});

test('while the people who wrote load, the dialog says it is loading', async () => {
  channelsApi.senders.mockReturnValue(new Promise(() => undefined));
  view(<Tabs.staff channel={channel()} canManage timezone="UTC" />);
  fireEvent.click(screen.getByRole('button', { name: 'Add staff account' }));
  expect(within(screen.getByRole('dialog', { name: 'Add staff account' })).getByText('Loading…').closest('[aria-busy="true"]')).not.toBeNull();
});

test('a pasted user id that is not a number, or already staff, is refused before anything is sent', async () => {
  view(<Tabs.staff channel={channel({ staff_user_ids: ['9930004402'] })} canManage timezone="UTC" />);
  fireEvent.click(screen.getByRole('button', { name: 'Add staff account' }));
  const dialog = screen.getByRole('dialog', { name: 'Add staff account' });
  const field = within(dialog).getByLabelText('Or paste a Zalo user id');
  fireEvent.change(field, { target: { value: 'abc' } });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Add' }));
  expect(field).toHaveAccessibleDescription(/Digits only/);
  fireEvent.change(field, { target: { value: '9930004402' } });
  expect(field).toHaveAccessibleDescription(/already a staff account/);
  expect(channelsApi.update).not.toHaveBeenCalled();
});

test('messages saves the commands, notices and one-to-one answer as the whole of the stored settings', async () => {
  const rules = {
    commands: { open: { action: 'open', match: 'fuzzy', phrases: ['tạo phiếu'], tags: ['zalo'] } },
    notices: { received: 'Đã nhận.', unmatched: '' },
    scopes: { direct: { mode: 'commands', unmatched: 'join_open' }, group: { mode: 'commands', gather: { quiet_seconds: 300, max_seconds: 1200 } } },
  };
  view(<Tabs.messages channel={channel({}, { settings: { join_policy: { kind: 'never' } }, effective_settings: rules })} canManage timezone="UTC" />);
  fireEvent.change(screen.getByLabelText('Match for Open a request'), { target: { value: 'exact' } });
  const cancel = screen.getByRole('group', { name: 'Phrases for Cancel' });
  fireEvent.click(within(cancel).getByRole('button', { name: 'Add a phrase' }));
  fireEvent.change(within(cancel).getByLabelText('New phrase for Cancel'), { target: { value: 'hủy' } });
  fireEvent.keyDown(within(cancel).getByLabelText('New phrase for Cancel'), { key: 'Enter' });
  fireEvent.change(screen.getByLabelText('Reply to a one-to-one message that is not a request'), { target: { value: 'Gõ “tạo phiếu” để tạo yêu cầu.' } });
  fireEvent.click(screen.getByLabelText('Answer such a message with this reply'));
  fireEvent.click(screen.getByRole('button', { name: 'Save messages' }));
  await waitFor(() => expect(channelsApi.update).toHaveBeenCalledTimes(1));
  expect(channelsApi.update).toHaveBeenCalledWith(ID, { settings: {
    join_policy: { kind: 'never' },
    commands: { open: { action: 'open', match: 'exact', phrases: ['tạo phiếu'], tags: ['zalo'] }, cancel: { action: 'cancel', match: 'fuzzy', phrases: ['hủy'] } },
    notices: { received: 'Đã nhận.', unmatched: 'Gõ “tạo phiếu” để tạo yêu cầu.', ask_confirm: '', cancelled: '', expired: '', reopen_not_found: '', reopen_legacy: '', reopened: '', reopen_noted: '', help: '' },
    scopes: { direct: { mode: 'commands', unmatched: 'notice' } },
  } });
});

test('asking before a one-to-one request opens sets the wait, the question and the Confirm command, and writes the scope whole', async () => {
  const rules = {
    commands: { open: { action: 'open', match: 'exact', phrases: ['tạo phiếu'] } },
    notices: { received: '', unmatched: '' },
    scopes: { direct: { mode: 'commands', unmatched: 'notice', gather: { quiet_seconds: 0, max_seconds: 1800 } } },
  };
  view(<Tabs.messages channel={channel({}, { settings: {}, effective_settings: rules })} canManage timezone="UTC" />);
  fireEvent.click(screen.getByRole('switch', { name: 'Ask the customer to confirm' }));
  expect(screen.getByLabelText('Wait for the customer to stop writing')).toHaveValue(10);
  expect(screen.getByLabelText('Open anyway after')).toHaveValue(60);
  fireEvent.change(screen.getByLabelText('Open anyway after'), { target: { value: '0' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save messages' }));
  expect(screen.getByLabelText('Open anyway after')).toHaveAccessibleDescription(/1 to 3600/);
  expect(channelsApi.update).not.toHaveBeenCalled();
  fireEvent.change(screen.getByLabelText('Open anyway after'), { target: { value: '90' } });
  expect(screen.getByText(/stops writing for 10 seconds.*90 seconds of silence/)).toBeInTheDocument();
  // The server asks nothing without the question's words.
  expect(screen.getByLabelText('Question')).toHaveAccessibleDescription(/Write the question the OA asks, or turn this off\./);
  expect(channelsApi.update).not.toHaveBeenCalled();
  fireEvent.change(screen.getByLabelText('Question'), { target: { value: 'Nhắn OK để tạo phiếu, hoặc Hủy.' } });
  fireEvent.change(screen.getByLabelText('When a request is cancelled'), { target: { value: 'Đã hủy.' } });
  fireEvent.change(screen.getByLabelText('When a customer sends only the command'), { target: { value: 'Anh/chị mô tả giúp vấn đề nhé.' } });
  const confirm = screen.getByRole('group', { name: 'Phrases for Confirm' });
  fireEvent.click(within(confirm).getByRole('button', { name: 'Add a phrase' }));
  fireEvent.change(within(confirm).getByLabelText('New phrase for Confirm'), { target: { value: 'ok' } });
  fireEvent.keyDown(within(confirm).getByLabelText('New phrase for Confirm'), { key: 'Enter' });
  expect(screen.getByLabelText('Match for Confirm')).toHaveValue('exact');
  fireEvent.click(screen.getByRole('button', { name: 'Save messages' }));
  await waitFor(() => expect(channelsApi.update).toHaveBeenCalledTimes(1));
  const sent = channelsApi.update.mock.calls[0][1].settings;
  // Confirm sets nothing about how long a request waits for its description.
  expect(sent.scopes.direct).toEqual({ mode: 'commands', unmatched: 'notice', gather: { quiet_seconds: 10, max_seconds: 1800, confirm_seconds: 90 } });
  expect(sent.scopes.group).toBeUndefined();
  expect(sent.commands.confirm).toEqual({ action: 'confirm', match: 'exact', phrases: ['ok'] });
  expect(sent.notices).toMatchObject({ ask_confirm: 'Nhắn OK để tạo phiếu, hoặc Hủy.', cancelled: 'Đã hủy.' });
  expect(sent.commands.open).toEqual({ action: 'open', match: 'exact', phrases: ['tạo phiếu'], ask_description: 'Anh/chị mô tả giúp vấn đề nhé.' });
});

test('a request waiting for its description is forgotten after the same minutes in every scope, written only when changed', async () => {
  const rules = {
    commands: { open: { action: 'open', phrases: ['tạo phiếu'], ask_description: 'Mô tả giúp?' } },
    scopes: { direct: { mode: 'commands', gather: { quiet_seconds: 10, expire_seconds: 1800 } }, group: { mode: 'commands', gather: { quiet_seconds: 300, max_seconds: 1200, expire_seconds: 20 } } },
  };
  view(<Tabs.messages channel={channel({}, { effective_settings: rules })} canManage timezone="UTC" />);
  const field = screen.getByLabelText('Forget a request still waiting for its description after');
  expect(field).toHaveValue(30);
  expect(field).toHaveAccessibleDescription(/Groups use 20 seconds; saving sets both\./);
  // Saving something else leaves both as they are.
  fireEvent.change(screen.getByLabelText('When a request waiting for its description is forgotten'), { target: { value: 'Đã quá hạn.' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save messages' }));
  await waitFor(() => expect(channelsApi.update).toHaveBeenCalledTimes(1));
  expect(channelsApi.update.mock.calls[0][1].settings.scopes).toBeUndefined();
  expect(channelsApi.update.mock.calls[0][1].settings.notices.expired).toBe('Đã quá hạn.');
  fireEvent.change(field, { target: { value: '45' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save messages' }));
  await waitFor(() => expect(channelsApi.update).toHaveBeenCalledTimes(2));
  expect(channelsApi.update.mock.calls[1][1].settings.scopes).toEqual({
    direct: { mode: 'commands', gather: { quiet_seconds: 10, expire_seconds: 2700 } },
    group: { mode: 'commands', gather: { quiet_seconds: 300, max_seconds: 1200, expire_seconds: 2700 } },
  });
});

test('a wait in seconds that is not whole minutes is shown as it is', () => {
  const rules = { scopes: { direct: { mode: 'commands', gather: { expire_seconds: 90 } }, group: { mode: 'commands', gather: { expire_seconds: 90 } } } };
  view(<Tabs.messages channel={channel({}, { effective_settings: rules })} canManage timezone="UTC" />);
  expect(screen.getByLabelText('Forget a request still waiting for its description after')).toHaveAccessibleDescription(/90 seconds now/);
});

test('turned off and on again, the question keeps the waits set in the form', () => {
  const rules = { scopes: { direct: { mode: 'commands', gather: { quiet_seconds: 0 } } } };
  view(<Tabs.messages channel={channel({}, { effective_settings: rules })} canManage timezone="UTC" />);
  const ask = screen.getByRole('switch', { name: 'Ask the customer to confirm' });
  fireEvent.click(ask);
  fireEvent.change(screen.getByLabelText('Open anyway after'), { target: { value: '20' } });
  fireEvent.click(ask);
  fireEvent.click(ask);
  expect(screen.getByLabelText('Open anyway after')).toHaveValue(20);
});

test('the quiet wait shows while a description is asked for, even with the question off, and says it serves both', async () => {
  const rules = { commands: { open: { action: 'open', phrases: ['tạo phiếu'] } }, scopes: { direct: { mode: 'commands', gather: { quiet_seconds: 10 } } } };
  view(<Tabs.messages channel={channel({}, { effective_settings: rules })} canManage timezone="UTC" />);
  expect(screen.queryByLabelText('Wait for the customer to stop writing')).toBeNull();
  fireEvent.change(screen.getByLabelText('When a customer sends only the command'), { target: { value: 'Mô tả giúp?' } });
  const quiet = screen.getByLabelText('Wait for the customer to stop writing');
  expect(quiet).toHaveAccessibleDescription(/before the question and before asking for a description/i);
  fireEvent.change(quiet, { target: { value: '5' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save messages' }));
  await waitFor(() => expect(channelsApi.update).toHaveBeenCalledTimes(1));
  expect(channelsApi.update.mock.calls[0][1].settings.scopes.direct).toEqual({ mode: 'commands', gather: { quiet_seconds: 5, confirm_seconds: 0 } });
});

test('turning the question off keeps the gathering and sets its wait to 0', async () => {
  const rules = { scopes: { direct: { mode: 'commands', gather: { quiet_seconds: 10, confirm_seconds: 60 } } } };
  view(<Tabs.messages channel={channel({}, { effective_settings: rules })} canManage timezone="UTC" />);
  fireEvent.click(screen.getByRole('switch', { name: 'Ask the customer to confirm' }));
  expect(screen.queryByLabelText('Open anyway after')).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Save messages' }));
  await waitFor(() => expect(channelsApi.update).toHaveBeenCalledTimes(1));
  expect(channelsApi.update.mock.calls[0][1].settings.scopes.direct).toEqual({ mode: 'commands', gather: { quiet_seconds: 10, confirm_seconds: 0 } });
});

test('where every one-to-one message opens a conversation, the page offers no answer to turn on', () => {
  view(<Tabs.messages channel={channel({}, { effective_settings: { scopes: { direct: { mode: 'all' } } } })} canManage timezone="UTC" />);
  expect(screen.queryByLabelText('Answer such a message with this reply')).toBeNull();
  expect(screen.getByLabelText('Reply to a one-to-one message that is not a request')).toHaveAccessibleDescription(/never sent/);
});

test('an agent reads the channel and changes nothing', async () => {
  channelsApi.threads.mockResolvedValue({ items: [thread('group', 'group:6127000931')], next_cursor: null });
  const config = { staff_user_ids: ['9930004402'], groups: { 6127000931: 'HC Labs' } };
  const { unmount } = view(<Tabs.connection channel={channel(config)} canManage={false} timezone="UTC" />);
  expect(screen.queryByRole('button', { name: /Replace|Add/ })).toBeNull();
  unmount();
  const groups = view(<Tabs.groups channel={channel(config)} canManage={false} timezone="UTC" />);
  expect(await screen.findByText('HC Labs')).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /Link|Change|Unlink/ })).toBeNull();
  groups.unmount();
  const staff = view(<Tabs.staff channel={channel(config)} canManage={false} timezone="UTC" />);
  expect(screen.getByText('Only admins change staff accounts.')).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /Remove|Add/ })).toBeNull();
  staff.unmount();
  view(<Tabs.messages channel={channel(config)} canManage={false} timezone="UTC" />);
  expect(screen.queryByRole('button', { name: 'Save messages' })).toBeNull();
});

test('once the tokens are set, Connection no longer says what to do first', () => {
  view(<Tabs.connection channel={channel({ credentials: credentials({ app_secret: 'a1f9', oa_secret_key: '7c20', access_token: 'aaaa', refresh_token: 'rrrr' }) })} canManage timezone="UTC" />);
  expect(screen.queryByText(/Paste this webhook URL/)).toBeNull();
});

test('references: the channel picks its number series, sees the next code, and saves it into its settings whole', async () => {
  const year = Number(new Intl.DateTimeFormat('en', { timeZone: 'UTC', year: 'numeric' }).format(new Date()));
  view(<Tabs.connection channel={channel({}, { settings: { join_policy: { kind: 'never' } }, effective_settings: { number_series: 'default' } })} canManage timezone="UTC" />);
  const select = await screen.findByLabelText('Number series');
  await waitFor(() => expect(select).toHaveAccessibleDescription('Next reference: #13'));
  fireEvent.change(select, { target: { value: 'chd' } });
  expect(select).toHaveAccessibleDescription(`Next reference: CHD-${year}-0143`);
  fireEvent.click(screen.getByRole('button', { name: 'Save series' }));
  await waitFor(() => expect(channelsApi.update).toHaveBeenCalledWith(ID, { settings: { join_policy: { kind: 'never' }, number_series: 'chd' } }));
  expect(screen.getByRole('link', { name: /Settings → Conversation numbers/ })).toHaveAttribute('href', '/settings/numbers');
});

test('the reopen notices are saved with the others, and the received notice says what {ref} becomes', async () => {
  view(<Tabs.messages channel={channel({}, { effective_settings: {} })} canManage timezone="UTC" />);
  expect(screen.getByLabelText('When a conversation opens')).toHaveAccessibleDescription(/\{ref\} becomes the conversation’s code/);
  fireEvent.change(screen.getByLabelText('When Reopen names a code that is not found'), { target: { value: 'Không tìm thấy {ref}.' } });
  fireEvent.change(screen.getByLabelText('When Reopen names a code from before Colliedesk'), { target: { value: 'Phiếu {old_ref} cũ, phiếu mới {ref}.' } });
  fireEvent.change(screen.getByLabelText('When Reopen reopens a resolved or closed conversation'), { target: { value: 'Đã mở lại {ref}.' } });
  fireEvent.change(screen.getByLabelText('When Reopen names a conversation still open'), { target: { value: 'Đã ghi nhận vào {ref}.' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save messages' }));
  await waitFor(() => expect(channelsApi.update).toHaveBeenCalledTimes(1));
  expect(channelsApi.update.mock.calls[0][1].settings.notices).toMatchObject({ reopen_not_found: 'Không tìm thấy {ref}.', reopen_legacy: 'Phiếu {old_ref} cũ, phiếu mới {ref}.',
    reopened: 'Đã mở lại {ref}.', reopen_noted: 'Đã ghi nhận vào {ref}.' });
});

test('references count the next code\'s year where the workspace is, not where the agent is', async () => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  vi.setSystemTime(new Date('2026-12-31T12:00:00Z'));
  try {
    mocks.api.me.mockResolvedValue({ timezone: 'Pacific/Kiritimati', workspace: { timezone: 'Pacific/Pago_Pago' } });
    view(<Tabs.connection channel={channel({}, { effective_settings: { number_series: 'chd' } })} canManage timezone="Pacific/Kiritimati" />);
    const select = await screen.findByLabelText('Number series');
    await waitFor(() => expect(select).toHaveAccessibleDescription('Next reference: CHD-2026-0143'));
  } finally {
    vi.useRealTimers();
  }
});

test('the Help command and its answer are saved with the others; Help matches as its phrases say', async () => {
  view(<Tabs.messages channel={channel({}, { effective_settings: {} })} canManage timezone="UTC" />);
  const help = screen.getByRole('group', { name: 'Phrases for Help' });
  fireEvent.click(within(help).getByRole('button', { name: 'Add a phrase' }));
  fireEvent.change(within(help).getByLabelText('New phrase for Help'), { target: { value: 'Hỗ trợ' } });
  fireEvent.keyDown(within(help).getByLabelText('New phrase for Help'), { key: 'Enter' });
  fireEvent.change(screen.getByLabelText('Match for Help'), { target: { value: 'words' } });
  fireEvent.change(screen.getByLabelText('When a customer asks for help'), { target: { value: 'Gõ “tạo phiếu” để tạo yêu cầu.' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save messages' }));
  await waitFor(() => expect(channelsApi.update).toHaveBeenCalledTimes(1));
  const sent = channelsApi.update.mock.calls[0][1].settings;
  expect(sent.commands.help).toEqual({ action: 'help', match: 'words', phrases: ['Hỗ trợ'] });
  expect(sent.notices.help).toBe('Gõ “tạo phiếu” để tạo yêu cầu.');
});
