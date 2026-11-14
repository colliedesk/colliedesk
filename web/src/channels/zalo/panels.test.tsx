import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import type { ReactElement } from 'react';
import { beforeEach, expect, test, vi } from 'vitest';
import type { ChannelDetail } from '../../api/channels';
import { SettingsEditsProvider } from '../../features/settings/settings-edits';

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
    site_verification: null, verification_prefix_path: '/api/v1/channels/zalo/', verification_prefix_url: null, webhook_url: null,
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
  return render(<QueryClientProvider client={client}><SettingsEditsProvider>{ui}</SettingsEditsProvider></QueryClientProvider>);
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
  expect(screen.getByRole('status')).toHaveTextContent('Verify the URL prefix in your Zalo app, paste the webhook URL there, then add the tokens.');
  expect(screen.getByRole('button', { name: 'Add tokens' })).toBeInTheDocument();
  expect(screen.getAllByRole('button', { name: 'Replace' })).toHaveLength(2);
  expect(screen.getByText('Running')).toBeInTheDocument();
});

test('domain verification shows the URL prefix to verify and saves the code pasted from Zalo', async () => {
  const pasted = 'zalo_verifierOOMiTAgDDsCXlvT2meuJUq6BicIj_Ma_DJKs.html';
  view(<Tabs.connection channel={channel()} canManage timezone="UTC" />);
  const block = screen.getByRole('region', { name: 'Domain verification' });
  expect(within(block).getByText(`${window.location.origin}/api/v1/channels/zalo/`)).toBeInTheDocument();
  expect(within(block).getByRole('button', { name: 'Copy prefix' })).toBeInTheDocument();
  expect(within(block).getByText(/URL prefix/)).toBeInTheDocument();
  expect(within(block).getByText(/HTML file/)).toBeInTheDocument();
  expect(within(block).getByText('No code yet: Zalo cannot verify the prefix.')).toBeInTheDocument();
  const save = within(block).getByRole('button', { name: 'Save code' });
  expect(save).toBeDisabled();
  fireEvent.change(within(block).getByLabelText('Verification code'), { target: { value: `  ${pasted} ` } });
  fireEvent.click(save);
  await waitFor(() => expect(channelsApi.update).toHaveBeenCalledWith(ID, { config: { site_verification: pasted } }));
});

test('a saved code shows the file Zalo will fetch, and Remove code clears it', async () => {
  const code = 'OOMiTAgDDsCXlvT2meuJUq6BicIj_Ma_DJKs';
  view(<Tabs.connection channel={channel({ site_verification: code })} canManage timezone="UTC" />);
  const block = screen.getByRole('region', { name: 'Domain verification' });
  const file = within(block).getByRole('link', { name: `zalo_verifier${code}.html` });
  expect(file).toHaveAttribute('href', `${window.location.origin}/api/v1/channels/zalo/zalo_verifier${code}.html`);
  expect(within(block).getByText(/Zalo can fetch/)).toBeInTheDocument();
  fireEvent.click(within(block).getByRole('button', { name: 'Remove code' }));
  await waitFor(() => expect(channelsApi.update).toHaveBeenCalledWith(ID, { config: { site_verification: '' } }));
});

test('a code the server refuses says why next to the box', async () => {
  channelsApi.update.mockRejectedValueOnce(new mocks.InvalidRequestError('Check the Zalo channel.', { fields: { site_verification: 'Paste the code Zalo shows for the HTML file method.' } }));
  view(<Tabs.connection channel={channel()} canManage timezone="UTC" />);
  const block = screen.getByRole('region', { name: 'Domain verification' });
  fireEvent.change(within(block).getByLabelText('Verification code'), { target: { value: 'not a code' } });
  fireEvent.click(within(block).getByRole('button', { name: 'Save code' }));
  await waitFor(() => expect(within(block).getByLabelText('Verification code')).toHaveAccessibleDescription('Paste the code Zalo shows for the HTML file method.'));
  // Said once: neither the hint nor the form's generic refusal repeats it.
  expect(within(block).queryByText('Check the Zalo channel.')).toBeNull();
});

test('an agent sees whether the prefix can be verified and changes nothing', () => {
  view(<Tabs.connection channel={channel({ site_verification: 'OOMiTAgDDsCXlvT2meuJUq6BicIj_Ma_DJKs' })} canManage={false} timezone="UTC" />);
  const block = screen.getByRole('region', { name: 'Domain verification' });
  expect(within(block).queryByLabelText('Verification code')).toBeNull();
  expect(within(block).queryByRole('button', { name: 'Remove code' })).toBeNull();
  expect(within(block).getByRole('link', { name: /zalo_verifier/ })).toBeInTheDocument();
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
  expect(await screen.findByText('Add the OA to a group; it shows after its first message.')).toBeInTheDocument();
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
  fireEvent.click(await screen.findByRole('button', { name: 'Save' }));
  await waitFor(() => expect(channelsApi.update).toHaveBeenCalledTimes(1));
  expect(channelsApi.update).toHaveBeenCalledWith(ID, { settings: {
    join_policy: { kind: 'never' },
    commands: { open: { action: 'open', match: 'exact', phrases: ['tạo phiếu'], tags: ['zalo'] }, cancel: { action: 'cancel', match: 'fuzzy', phrases: ['hủy'] } },
    notices: { received: 'Đã nhận.', unmatched: 'Gõ “tạo phiếu” để tạo yêu cầu.', ask_confirm: '', cancelled: '', expired: '', reopen_not_found: '', reopen_legacy: '', reopened: '', reopen_noted: '', help: '', status_pending: '', status_resolved: '', status_closed: '', help_every_seconds: 300, unmatched_every_seconds: 300 },
    auto_close_hours: 0,
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
  fireEvent.click(await screen.findByRole('button', { name: 'Save' }));
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
  fireEvent.click(await screen.findByRole('button', { name: 'Save' }));
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
  fireEvent.click(await screen.findByRole('button', { name: 'Save' }));
  await waitFor(() => expect(channelsApi.update).toHaveBeenCalledTimes(1));
  expect(channelsApi.update.mock.calls[0][1].settings.scopes).toBeUndefined();
  expect(channelsApi.update.mock.calls[0][1].settings.notices.expired).toBe('Đã quá hạn.');
  fireEvent.change(field, { target: { value: '45' } });
  fireEvent.click(await screen.findByRole('button', { name: 'Save' }));
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
  fireEvent.click(await screen.findByRole('button', { name: 'Save' }));
  await waitFor(() => expect(channelsApi.update).toHaveBeenCalledTimes(1));
  expect(channelsApi.update.mock.calls[0][1].settings.scopes.direct).toEqual({ mode: 'commands', gather: { quiet_seconds: 5, confirm_seconds: 0 } });
});

test('turning the question off keeps the gathering and sets its wait to 0', async () => {
  const rules = { scopes: { direct: { mode: 'commands', gather: { quiet_seconds: 10, confirm_seconds: 60 } } } };
  view(<Tabs.messages channel={channel({}, { effective_settings: rules })} canManage timezone="UTC" />);
  fireEvent.click(screen.getByRole('switch', { name: 'Ask the customer to confirm' }));
  expect(screen.queryByLabelText('Open anyway after')).toBeNull();
  fireEvent.click(await screen.findByRole('button', { name: 'Save' }));
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
  expect(screen.queryByRole('button', { name: 'Save' })).toBeNull();
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
  // A pick from the list saves at once; the page says Saved with an Undo.
  await waitFor(() => expect(select).toHaveAccessibleDescription(`Next reference: CHD-${year}-0143`));
  await waitFor(() => expect(channelsApi.update).toHaveBeenCalledWith(ID, { settings: { join_policy: { kind: 'never' }, number_series: 'chd' } }));
  expect(screen.getByRole('link', { name: /Settings → Conversation numbers/ })).toHaveAttribute('href', '/settings/general#numbers-heading');
});

test('the reopen notices are saved with the others, and the received notice says what {ref} becomes', async () => {
  view(<Tabs.messages channel={channel({}, { effective_settings: {} })} canManage timezone="UTC" />);
  expect(screen.getByLabelText('When a conversation opens')).toHaveAccessibleDescription(/The customer can send the code back with Reopen/);
  fireEvent.change(screen.getByLabelText('When Reopen names a code that is not found'), { target: { value: 'Không tìm thấy {ref}.' } });
  fireEvent.change(screen.getByLabelText('When Reopen names a code from before Colliedesk'), { target: { value: 'Phiếu {old_ref} cũ, phiếu mới {ref}.' } });
  fireEvent.change(screen.getByLabelText('When a resolved or closed conversation is reopened'), { target: { value: 'Đã mở lại {ref}.' } });
  fireEvent.change(screen.getByLabelText('When Reopen names a conversation still open'), { target: { value: 'Đã ghi nhận vào {ref}.' } });
  fireEvent.click(await screen.findByRole('button', { name: 'Save' }));
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
  fireEvent.click(await screen.findByRole('button', { name: 'Save' }));
  await waitFor(() => expect(channelsApi.update).toHaveBeenCalledTimes(1));
  const sent = channelsApi.update.mock.calls[0][1].settings;
  expect(sent.commands.help).toEqual({ action: 'help', match: 'words', phrases: ['Hỗ trợ'] });
  expect(sent.notices.help).toBe('Gõ “tạo phiếu” để tạo yêu cầu.');
});

test('how often the help and the unmatched reply go to one chat is set in seconds, 0 for every time, and refused out of range', async () => {
  view(<Tabs.messages channel={channel({}, { effective_settings: { notices: { help_every_seconds: 300, unmatched_every_seconds: 300 } } })} canManage timezone="UTC" />);
  const helpEvery = screen.getByLabelText('Answer help in one chat at most every');
  expect(helpEvery).toHaveValue(300);
  fireEvent.change(helpEvery, { target: { value: '90000' } });
  fireEvent.click(await screen.findByRole('button', { name: 'Save' }));
  expect(helpEvery).toHaveAccessibleDescription(/0 to 86400/);
  expect(channelsApi.update).not.toHaveBeenCalled();
  fireEvent.change(helpEvery, { target: { value: '0' } });
  fireEvent.change(screen.getByLabelText('Send the unmatched reply in one chat at most every'), { target: { value: '600' } });
  fireEvent.click(await screen.findByRole('button', { name: 'Save' }));
  await waitFor(() => expect(channelsApi.update).toHaveBeenCalledTimes(1));
  expect(channelsApi.update.mock.calls[0][1].settings.notices).toMatchObject({ help_every_seconds: 0, unmatched_every_seconds: 600 });
});

test('a notice box shows every line of what it says, and each "at most every" sits under the notice it paces', () => {
  const help = ['Hướng dẫn:', '1. tạo phiếu', '2. hủy', '3. mở lại CHD-…', '4. xác nhận', '5. trợ giúp', 'Cảm ơn quý khách.'].join('\n');
  view(<Tabs.messages channel={channel({}, { effective_settings: { notices: { help, unmatched: 'Gõ “tạo phiếu”.' } } })} canManage timezone="UTC" />);
  expect(screen.getByLabelText('When a customer asks for help')).toHaveAttribute('rows', '7');
  expect(screen.getByLabelText('Reply to a one-to-one message that is not a request')).toHaveAttribute('rows', '2');
  const order = (label: string) => screen.getAllByRole('textbox').concat(screen.getAllByRole('spinbutton')).sort((a, b) => (a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING ? -1 : 1)).indexOf(screen.getByLabelText(label));
  expect(order('Send the unmatched reply in one chat at most every')).toBe(order('Reply to a one-to-one message that is not a request') + 1);
  expect(order('Answer help in one chat at most every')).toBe(order('When a customer asks for help') + 1);
});

test('what the OA says when the team sets a status is saved with the other notices, empty for quiet', async () => {
  view(<Tabs.messages channel={channel({}, { effective_settings: { notices: { status_resolved: 'Đã xử lý {ref}.' } } })} canManage timezone="UTC" />);
  const block = screen.getByRole('region', { name: 'When a conversation’s status changes' });
  expect(within(block).getByLabelText('When set to Resolved')).toHaveValue('Đã xử lý {ref}.');
  expect(within(block).getByLabelText('When set to Closed')).toHaveAccessibleDescription(/Also sent when a conversation closes by itself/);
  fireEvent.change(within(block).getByLabelText('When set to Pending'), { target: { value: '  Đang chờ {subject}  ' } });
  fireEvent.change(within(block).getByLabelText('When set to Closed'), { target: { value: 'Đã đóng {ref}.' } });
  fireEvent.click(await screen.findByRole('button', { name: 'Save' }));
  await waitFor(() => expect(channelsApi.update).toHaveBeenCalledTimes(1));
  const sent = channelsApi.update.mock.calls[0][1].settings;
  expect(sent.notices).toMatchObject({ status_pending: 'Đang chờ {subject}', status_resolved: 'Đã xử lý {ref}.', status_closed: 'Đã đóng {ref}.' });
  expect(sent.auto_close_hours).toBe(0);
});

test('the server refusing a status notice shows its words under that box', async () => {
  channelsApi.update.mockRejectedValueOnce(new mocks.InvalidRequestError('Check the channel settings.', { fields: { 'settings.notices.status_pending': 'This notice cannot fill {name}.' } }));
  view(<Tabs.messages channel={channel()} canManage timezone="UTC" />);
  fireEvent.change(screen.getByLabelText('When set to Pending'), { target: { value: 'Chờ {name}' } });
  fireEvent.click(await screen.findByRole('button', { name: 'Save' }));
  await waitFor(() => expect(screen.getByLabelText('When set to Pending')).toHaveAccessibleDescription(/This notice cannot fill \{name\}\./));
});

test('closing resolved conversations by themselves is a number of hours, empty or 0 for never, and says what it will do', async () => {
  view(<Tabs.messages channel={channel({}, { effective_settings: { notices: { status_closed: 'Đã đóng {ref}.' } } })} canManage timezone="UTC" />);
  const hours = screen.getByLabelText('Close resolved conversations after');
  expect(hours).toHaveValue(null);
  expect(hours).toHaveAccessibleDescription(/Resolved conversations stay resolved until closed\./);
  fireEvent.change(hours, { target: { value: '1' } });
  expect(hours).toHaveAccessibleDescription(/Closes after 1 hour resolved; the customer is told\./);
  fireEvent.change(screen.getByLabelText('When set to Closed'), { target: { value: '' } });
  fireEvent.change(hours, { target: { value: '48' } });
  expect(hours).toHaveAccessibleDescription(/Closes after 48 hours resolved, quietly\./);
  fireEvent.change(hours, { target: { value: '721' } });
  fireEvent.click(await screen.findByRole('button', { name: 'Save' }));
  expect(hours).toHaveAccessibleDescription(/A whole number of hours, from 0 to 720\./);
  expect(channelsApi.update).not.toHaveBeenCalled();
  fireEvent.change(hours, { target: { value: '48' } });
  fireEvent.click(await screen.findByRole('button', { name: 'Save' }));
  await waitFor(() => expect(channelsApi.update).toHaveBeenCalledTimes(1));
  expect(channelsApi.update.mock.calls[0][1].settings.auto_close_hours).toBe(48);
});

test('changing the hours while closing is on says what saving will close when fewer hours reach back further', () => {
  view(<Tabs.messages channel={channel({}, { settings: { auto_close_hours: 48 }, effective_settings: { auto_close_hours: 48, notices: { status_closed: 'Đã đóng.' } } })} canManage timezone="UTC" />);
  const hours = screen.getByLabelText('Close resolved conversations after');
  expect(hours).toHaveValue(48);
  expect(hours).toHaveAccessibleDescription(/A new number also applies to conversations resolved since it was turned on\./);
  fireEvent.change(hours, { target: { value: '72' } });
  expect(hours).not.toHaveAccessibleDescription(/Saving closes/);
  fireEvent.change(hours, { target: { value: '24' } });
  expect(hours).toHaveAccessibleDescription(/Saving closes conversations already resolved for 24 hours and tells their customers\./);
  fireEvent.change(screen.getByLabelText('When set to Closed'), { target: { value: '' } });
  expect(hours).toHaveAccessibleDescription(/Saving closes conversations already resolved for 24 hours\./);
});

test('Reopened is said when the team reopens too', () => {
  view(<Tabs.messages channel={channel()} canManage timezone="UTC" />);
  expect(screen.getByLabelText('When a resolved or closed conversation is reopened')).toHaveAccessibleDescription(/Sent when the customer’s Reopen command reopens it, and when your team does\./);
});

test('an agent reads the status notices and the closing rule', () => {
  view(<Tabs.messages channel={channel({}, { effective_settings: { auto_close_hours: 48, notices: { status_resolved: 'Đã xử lý.' } } })} canManage={false} timezone="UTC" />);
  const row = (label: string) => screen.getByText(label, { selector: 'dt' }).nextElementSibling;
  expect(row('When set to Resolved')).toHaveTextContent('Đã xử lý.');
  expect(row('When set to Pending')).toHaveTextContent('—');
  expect(row('Close resolved conversations after')).toHaveTextContent('48 hours');
});

// Quiet by default (docs/internals/design.md): a row keeps its label and one short line; the how and why sit in a tip.
const tip = (label: string) => screen.getByRole('button', { name: `About ${label}` });

test('connect keeps a field to its label: what the name and the OA id are is in a tip', () => {
  view(<Connect canManage timezone="UTC" create={vi.fn()} />);
  expect(tip('Name')).toHaveAccessibleDescription('Tells this OA apart in the inbox and in Settings. Leave it empty to use the type’s name.');
  expect(screen.getByLabelText('Name')).toHaveAccessibleDescription(/Tells this OA apart/);
  // Where to find the OA id is what the person filling the form needs now: it stays visible, like the App ID's.
  expect(screen.getByLabelText('OA ID')).toHaveAccessibleDescription(/In Zalo for Developers → your app → Official Account\./);
  expect(screen.getByText('In Zalo for Developers → your app → Official Account.')).toBeVisible();
  expect(screen.queryByText(/Tells this OA apart/, { selector: '.field-note' })).toBeNull();
});

test('Connection says each step in one line and keeps the background in the tip of its block', async () => {
  view(<Tabs.connection channel={channel()} canManage timezone="UTC" />);
  expect(screen.getByText('Colliedesk serves Zalo’s verification file for you.')).toBeInTheDocument();
  expect(tip('Domain verification')).toHaveAccessibleDescription('Zalo takes the webhook URL only on a verified address. Colliedesk serves the file, so there is no DNS change or upload.');
  expect(screen.getByText('In your Zalo app → Webhook, paste this address.')).toBeInTheDocument();
  expect(tip('Webhook URL')).toHaveAccessibleDescription('Events of other OAs on the same app are acknowledged and ignored.');
  expect(screen.getByText('Stored encrypted, never shown again.')).toBeInTheDocument();
  expect(tip('Secrets')).toHaveAccessibleDescription('Replacing one takes effect at once; removing the channel erases them all.');
  expect(screen.getByText('The code each conversation of this OA gets.')).toBeInTheDocument();
  expect(tip('References')).toHaveAccessibleDescription('The customer can send it back to reopen the conversation. Codes already given never change.');
  expect(screen.getByRole('link', { name: 'Manage number series in Settings → Conversation numbers' })).toHaveAttribute('href', '/settings/general#numbers-heading');
  expect(tip('Processing')).toHaveAccessibleDescription(/^Pause, at the top of this page, keeps every message from Zalo.*Turning the channel off is different/);
  expect(tip('Verification code')).toHaveAccessibleDescription(/The part of zalo_verifier<code>\.html between zalo_verifier and \.html/);
  expect(screen.getByLabelText('Verification code')).toHaveAccessibleDescription(/The part of zalo_verifier<code>\.html/);
});

test('the tokens dialog asks for the newest pair in one line and says in a tip what happens to the held one', async () => {
  view(<Tabs.connection channel={channel()} canManage timezone="UTC" />);
  fireEvent.click(screen.getByRole('button', { name: 'Add tokens' }));
  const dialog = await screen.findByRole('dialog', { name: 'Add the tokens of Hảo Đạt OA' });
  expect(within(dialog).getByText('Copy the newest pair: a refresh token works only once.')).toBeInTheDocument();
  expect(within(dialog).getByRole('button', { name: 'About Refresh token' })).toHaveAccessibleDescription('The one Colliedesk holds stops working when another tool refreshes it.');
});

test('Groups and Staff open with one line each and keep what the lists mean in a tip', async () => {
  view(<Tabs.groups channel={channel()} canManage timezone="UTC" />);
  expect(await screen.findByText('Add the OA to a group; it shows after its first message.')).toBeInTheDocument();
  expect(screen.getByText('Link each group to its customer’s organization.')).toBeInTheDocument();
  expect(tip('Zalo groups')).toHaveAccessibleDescription(/^Messages in a group are gathered into cards in Review.*Link each group to file its requests under the right customer\.$/);
  expect(tip('Zalo groups')).toHaveAccessibleDescription(/A group shows here after its first message\./);
});

test('linking a group says in one line whose conversations change, and the organization field keeps its how in a tip', async () => {
  channelsApi.threads.mockResolvedValue({ items: [thread('group', 'group:8821004410')], next_cursor: null });
  view(<Tabs.groups channel={channel()} canManage timezone="UTC" />);
  fireEvent.click(await screen.findByRole('button', { name: 'Link…' }));
  const dialog = await screen.findByRole('dialog', { name: 'Link “Group 8821004410” to an organization' });
  expect(within(dialog).getByText('New conversations belong to it; earlier ones keep theirs.')).toBeInTheDocument();
  expect(within(dialog).getByRole('button', { name: 'About Organization' })).toHaveAccessibleDescription('As your contacts’ organization is written, for example in the inbox filter. At most 200 characters.');
});

test('Staff says whose messages these are in one line, and the add dialog says what to do when nobody wrote yet', async () => {
  view(<Tabs.staff channel={channel()} canManage timezone="UTC" />);
  expect(screen.getByText('Messages from these accounts are your team’s.')).toBeInTheDocument();
  expect(tip('Staff accounts')).toHaveAccessibleDescription('In a group they never open a request, and what they write there is the team’s.');
  fireEvent.click(screen.getAllByRole('button', { name: 'Add staff account' })[0]);
  const dialog = await screen.findByRole('dialog', { name: 'Add staff account' });
  // Nobody wrote to the OA yet: there is no list, so the step the admin needs stays on screen.
  expect(await within(dialog).findByText('Nobody to pick: ask the teammate to message the OA.')).toBeInTheDocument();
  expect(within(dialog).getByText('Paste the teammate’s Zalo user id.')).toBeInTheDocument();
  expect(within(dialog).getByRole('button', { name: 'About Or paste a Zalo user id' })).toHaveAccessibleDescription('A user id shows once the teammate has written to the OA.');
});

test('Messages keeps each box to its label and a count; what a notice is for is in its tip', () => {
  view(<Tabs.messages channel={channel()} canManage timezone="UTC" />);
  expect(screen.getByText('What a customer can type to the OA, in a chat or a group.')).toBeInTheDocument();
  expect(tip('Match options')).toHaveAccessibleDescription(/^Allow one typo forgives one wrong letter.*Exact is the whole message, accents kept, in any case\.$/);
  // The tip describes each select it explains, named after the words the person sees.
  expect(screen.getByRole('combobox', { name: 'Match for Open a request' })).toHaveAccessibleDescription(/Allow one typo forgives one wrong letter/);
  expect(screen.getByText('Sent by the OA; not counted as your team’s first reply.')).toBeInTheDocument();
  expect(tip('When a customer sends only the command')).toHaveAccessibleDescription(/^Asks what the request is about when “Open a request” comes with nothing before it\./);
  expect(tip('When a customer asks for help')).toHaveAccessibleDescription(/^Sent for the Help command, in a chat or a group;/);
  expect(tip('When a resolved or closed conversation is reopened')).toHaveAccessibleDescription(/^Sent when the customer’s Reopen command reopens it/);
  expect(tip('When Reopen names a code from before Colliedesk')).toHaveAccessibleDescription(/^Sent instead of the received notice/);
  expect(screen.getByLabelText('When a customer asks for help')).toHaveAccessibleDescription(/0 \/ 1,000/);
});

test('the status notices, the closing rule and the question each say one line and keep the rest in a tip', () => {
  view(<Tabs.messages channel={channel({}, { settings: { auto_close_hours: 48 }, effective_settings: { auto_close_hours: 48, notices: { status_closed: 'Đã đóng.' } } })} canManage timezone="UTC" />);
  expect(screen.getByText('Sent in the conversation’s chat when its status changes.')).toBeInTheDocument();
  expect(tip('When a conversation’s status changes')).toHaveAccessibleDescription(/^One to one, as a reply to the customer’s first message\./);
  expect(screen.getByText('Hours; empty or 0 means never.')).toBeInTheDocument();
  expect(tip('Close resolved conversations after')).not.toHaveAccessibleDescription(/Empty or 0/);
  expect(tip('Close resolved conversations after')).toHaveAccessibleDescription(/^Clock hours, not business hours, from the resolve or the customer’s last message/);
  fireEvent.change(screen.getByLabelText('Close resolved conversations after'), { target: { value: '24' } });
  expect(screen.getByText('Saving closes conversations already resolved for 24 hours and tells their customers.')).toBeInTheDocument();
  expect(tip('Question')).toHaveAccessibleDescription(/^Name the Confirm and Cancel phrases/);
  fireEvent.click(screen.getByRole('switch', { name: 'Ask the customer to confirm' }));
  expect(screen.getByText('Seconds of silence.')).toBeInTheDocument();
  expect(tip('Wait for the customer to stop writing')).toHaveAccessibleDescription('Waited before the question and before asking for a description.');
});

test('asking before opening says what it does in a tip, and in one line when it can never be asked', () => {
  view(<Tabs.messages channel={channel()} canManage timezone="UTC" />);
  expect(tip('Ask the customer to confirm')).toHaveAccessibleDescription(/^A request opens as soon as the customer is done writing; nothing is asked\./);
  expect(screen.getByRole('switch', { name: 'Ask the customer to confirm' })).toHaveAccessibleDescription(/nothing is asked/);
  expect(screen.getByText('No command needed here: this is never asked.')).toBeInTheDocument();
});

test('the add staff dialog lists who wrote when someone did, and says to ask the teammate when nobody did', async () => {
  channelsApi.senders.mockResolvedValue({ items: [sender('6620001029', 'Minh Trần', 'Hảo Đạt chat')] });
  view(<Tabs.staff channel={channel()} canManage timezone="UTC" />);
  fireEvent.click(screen.getAllByRole('button', { name: 'Add staff account' })[0]);
  const dialog = await screen.findByRole('dialog', { name: 'Add staff account' });
  expect(await within(dialog).findByText(/Minh Trần/)).toBeInTheDocument();
  expect(within(dialog).getByText('Pick a person who wrote to the OA, or paste a user id.')).toBeInTheDocument();
  expect(within(dialog).queryByText(/Nobody to pick/)).toBeNull();
});

test('a notice box says on screen what its placeholders stand for, and keeps the rest of its how in a tip', () => {
  view(<Tabs.messages channel={channel()} canManage timezone="UTC" />);
  expect(screen.getByLabelText('When a conversation opens')).toHaveAccessibleDescription(/\{ref\} = its code\. 0 \/ 1,000/);
  expect(screen.getAllByText('{ref} = its code. 0 / 1,000')).toHaveLength(3);
  expect(screen.getByText('{ref} = the code typed. 0 / 1,000')).toBeVisible();
  expect(screen.getByText('{old_ref} = typed, {ref} = new. 0 / 1,000')).toBeVisible();
  expect(screen.getAllByText('{ref} = its code, {subject} = its subject. 0 / 1,000')).toHaveLength(3);
  expect(tip('When a conversation opens')).toHaveAccessibleDescription(/Leave empty to send nothing\./);
});

test('a one-to-one reply that can never be sent says why, in one line', () => {
  view(<Tabs.messages channel={channel({}, { effective_settings: { scopes: { direct: { mode: 'all_open' } } } })} canManage timezone="UTC" />);
  expect(screen.getByLabelText('Reply to a one-to-one message that is not a request')).toHaveAccessibleDescription(/No command needed here: this is never sent\./);
});


test('messages: typing a change and typing the saved text back leaves nothing unsaved', async () => {
  view(<Tabs.messages channel={channel({}, { effective_settings: { notices: { received: 'Đã nhận.' } } })} canManage timezone="UTC" />);
  const field = screen.getByLabelText('When a conversation opens', { exact: true });
  expect(screen.queryByRole('region', { name: 'Unsaved changes' })).toBeNull();
  fireEvent.change(field, { target: { value: 'Đã nhận rồi.' } });
  expect(screen.getByRole('region', { name: 'Unsaved changes' })).toHaveTextContent('Messages');
  fireEvent.change(field, { target: { value: 'Đã nhận.' } });
  expect(screen.queryByRole('region', { name: 'Unsaved changes' })).toBeNull();
});
