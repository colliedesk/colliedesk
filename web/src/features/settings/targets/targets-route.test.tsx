import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, expect, test, vi } from 'vitest';
import type { ResponseTargets } from '../../../api/response-targets';

// Settings → Response targets (docs/user/response-targets.md).
const mocks = vi.hoisted(() => {
  class InvalidRequestError extends Error {
    detail?: Record<string, unknown>;
    constructor(message: string, detail?: Record<string, unknown>) { super(message); this.detail = detail; }
  }
  class ConflictError extends InvalidRequestError {}
  class ChangedElsewhereError extends ConflictError {}
  const api = { me: vi.fn(), config: vi.fn(), billing: vi.fn() };
  const responseTargetsApi = { get: vi.fn(), set: vi.fn(), setMyAlerts: vi.fn() };
  const channelsApi = { list: vi.fn() };
  return { api, responseTargetsApi, channelsApi, InvalidRequestError, ConflictError, ChangedElsewhereError };
});
vi.mock('../../../api/client', () => ({ api: mocks.api, InvalidRequestError: mocks.InvalidRequestError, ConflictError: mocks.ConflictError, ChangedElsewhereError: mocks.ChangedElsewhereError }));
vi.mock('../../../api/response-targets', async (actual) => ({ ...(await actual<typeof import('../../../api/response-targets')>()), responseTargetsApi: mocks.responseTargetsApi }));
vi.mock('../../../api/channels', () => ({ channelsApi: mocks.channelsApi }));
const { api, responseTargetsApi, channelsApi } = mocks;
const { TargetsRoute } = await import('./targets-route');

const me = (role: 'admin' | 'agent') => ({
  agent: { id: '11111111-1111-4111-8111-111111111111', email: 'sam@example.com', display_name: '', role, timezone_override: null },
  workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'beta', name: 'Beta', timezone: 'Asia/Ho_Chi_Minh' }, timezone: 'Asia/Ho_Chi_Minh', plan: null,
});
const week = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'] as const;
const doc = (change: Partial<ResponseTargets> = {}): ResponseTargets => ({
  enabled: true, timezone: null,
  hours: week.map((day) => ({ day, open: !['sat', 'sun'].includes(day), from: '08:00', to: '17:00' })),
  holidays: [], targets: { urgent: { first_response_minutes: 240, resolution_minutes: 540 }, high: { first_response_minutes: 240, resolution_minutes: null },
    normal: { first_response_minutes: 240, resolution_minutes: null }, low: { first_response_minutes: null, resolution_minutes: null } },
  warn_before_minutes: 60, channel_ids: null, alert_email: true, my_alert_email: true, enabled_at: '2026-10-01T00:00:00Z', updated_at: '2026-10-31T00:00:00Z', ...change,
});
const channel = (id: string, name: string) => ({ id, name, type: 'zalo', deleted_at: undefined });
const sent = () => responseTargetsApi.set.mock.calls.at(-1)?.[0];

function show() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><TargetsRoute /></QueryClientProvider>);
}

beforeEach(() => {
  vi.clearAllMocks();
  api.me.mockResolvedValue(me('admin'));
  api.config.mockResolvedValue({ billing: { enabled: false } });
  responseTargetsApi.get.mockResolvedValue(doc());
  responseTargetsApi.set.mockImplementation(async (value: ResponseTargets) => ({ ...value, updated_at: '2026-10-31T01:00:00Z' }));
  channelsApi.list.mockResolvedValue({ items: [channel('33333333-3333-4333-8333-333333333333', 'Hảo Đạt OA')], types: [] });
});

test('business hours refuse a day that ends before it starts, copy Monday to the weekdays, and save the whole document', async () => {
  show();
  const hours = await screen.findByRole('region', { name: 'Business hours' });
  fireEvent.change(within(hours).getByLabelText('Monday to'), { target: { value: '07:00' } });
  fireEvent.click(within(hours).getByRole('button', { name: 'Save business hours' }));
  expect(within(hours).getByLabelText('Monday to')).toHaveAccessibleDescription(/^Ends before it starts\./);
  expect(responseTargetsApi.set).not.toHaveBeenCalled();
  fireEvent.change(within(hours).getByLabelText('Monday from'), { target: { value: '06:30' } });
  fireEvent.click(within(hours).getByRole('button', { name: 'Copy Monday to weekdays' }));
  expect(within(hours).getByLabelText('Friday from')).toHaveValue('06:30');
  fireEvent.click(within(hours).getByLabelText('Saturday open'));
  fireEvent.click(within(hours).getByRole('button', { name: 'Save business hours' }));
  await waitFor(() => expect(responseTargetsApi.set).toHaveBeenCalledTimes(1));
  expect(sent().hours[4]).toEqual({ day: 'fri', open: true, from: '06:30', to: '07:00' });
  expect(sent().hours[5]).toMatchObject({ day: 'sat', open: true });
  expect(sent().targets).toEqual(doc().targets);
  expect(sent()).toHaveProperty('updated_at', '2026-10-31T00:00:00Z');
  expect(sent()).not.toHaveProperty('enabled_at');
});

test('targets are hours on the page and minutes on the wire, in half hours; an empty box is no target', async () => {
  show();
  const targets = await screen.findByRole('region', { name: 'Targets' });
  expect(within(targets).getByLabelText('Resolve target for Urgent, in business hours')).toHaveValue(9);
  fireEvent.change(within(targets).getByLabelText('First reply target for Low, in business hours'), { target: { value: '0.3' } });
  fireEvent.click(within(targets).getByRole('button', { name: 'Save targets' }));
  expect(within(targets).getByLabelText('First reply target for Low, in business hours')).toHaveAccessibleDescription('In half hours, more than 0.');
  fireEvent.change(within(targets).getByLabelText('First reply target for Low, in business hours'), { target: { value: '8.5' } });
  fireEvent.change(within(targets).getByLabelText('Resolve target for Urgent, in business hours'), { target: { value: '' } });
  fireEvent.click(within(targets).getByRole('button', { name: 'Save targets' }));
  await waitFor(() => expect(responseTargetsApi.set).toHaveBeenCalledTimes(1));
  expect(sent().targets.low).toEqual({ first_response_minutes: 510, resolution_minutes: null });
  expect(sent().targets.urgent).toEqual({ first_response_minutes: 240, resolution_minutes: null });
  expect(sent().warn_before_minutes).toBe(60);
});

test('a holiday is added and removed; the same day twice is refused', async () => {
  responseTargetsApi.get.mockResolvedValue(doc({ holidays: [{ date: '2026-09-02', name: 'Quốc khánh' }] }));
  show();
  const holidays = await screen.findByRole('region', { name: 'Holidays' });
  fireEvent.change(within(holidays).getByLabelText('Date'), { target: { value: '2026-09-02' } });
  fireEvent.click(within(holidays).getByRole('button', { name: 'Add holiday' }));
  expect(within(holidays).getByText('This day is a holiday already.')).toBeInTheDocument();
  fireEvent.change(within(holidays).getByLabelText('Date'), { target: { value: '2027-01-01' } });
  fireEvent.change(within(holidays).getByLabelText('Name'), { target: { value: 'Tết Dương lịch' } });
  fireEvent.click(within(holidays).getByRole('button', { name: 'Add holiday' }));
  await waitFor(() => expect(sent()?.holidays).toEqual([{ date: '2026-09-02', name: 'Quốc khánh' }, { date: '2027-01-01', name: 'Tết Dương lịch' }]));
  fireEvent.click(within(holidays).getByRole('button', { name: 'Remove Quốc khánh' }));
  await waitFor(() => expect(responseTargetsApi.set).toHaveBeenCalledTimes(2));
});

test('Applies to shows only with two channels or more; every channel is saved as null', async () => {
  show();
  await screen.findByRole('region', { name: 'Targets' });
  expect(screen.queryByRole('region', { name: 'Applies to' })).toBeNull();
  channelsApi.list.mockResolvedValue({ items: [channel('33333333-3333-4333-8333-333333333333', 'Hảo Đạt OA'), channel('44444444-4444-4444-8444-444444444444', 'Email')], types: [] });
  document.body.innerHTML = '';
  show();
  const applies = await screen.findByRole('region', { name: 'Applies to' });
  fireEvent.click(within(applies).getByRole('checkbox', { name: 'Email' }));
  fireEvent.click(within(applies).getByRole('button', { name: 'Save channels' }));
  await waitFor(() => expect(sent()?.channel_ids).toEqual(['33333333-3333-4333-8333-333333333333']));
  fireEvent.click(within(applies).getByRole('checkbox', { name: 'Email' }));
  fireEvent.click(within(applies).getByRole('button', { name: 'Save channels' }));
  await waitFor(() => expect(sent()?.channel_ids).toBeNull());
});

test('an agent reads the targets and changes nothing; a failed load says so with Try again', async () => {
  api.me.mockResolvedValue(me('agent'));
  responseTargetsApi.get.mockRejectedValueOnce(new Error('API 500'));
  show();
  expect(await screen.findByRole('alert')).toHaveTextContent('The response targets could not be loaded.');
  fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
  expect(await screen.findByRole('region', { name: 'Targets' })).toHaveTextContent('Urgent');
  expect(screen.getByText('Only admins change response targets.')).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /Save/ })).toBeNull();
  // Their own alerts are the one thing an agent turns on or off.
  expect(screen.getAllByRole('switch').map((item) => item.getAttribute('aria-label') ?? item.closest('label')?.textContent)).toEqual(['Email them to me']);
});

test('alerts: an admin turns the workspace\'s emails off; each person turns their own off, while the workspace sends them', async () => {
  responseTargetsApi.setMyAlerts = vi.fn().mockResolvedValue({ email: false });
  show();
  const alerts = await screen.findByRole('region', { name: 'Alerts' });
  fireEvent.click(within(alerts).getByRole('switch', { name: 'Email them to me' }));
  await waitFor(() => expect(responseTargetsApi.setMyAlerts).toHaveBeenCalledWith(false));
  await waitFor(() => expect(within(alerts).getByRole('switch', { name: 'Email them to me' })).not.toBeChecked());
  fireEvent.click(within(alerts).getByRole('switch', { name: 'Email reminders and overdue alerts' }));
  await waitFor(() => expect(sent()).toMatchObject({ alert_email: false }));
  expect(sent()).not.toHaveProperty('my_alert_email');
  expect(await within(alerts).findByText(/The workspace sends none/)).toBeInTheDocument();
  expect(within(alerts).getByRole('switch', { name: 'Email them to me' })).toBeDisabled();
});

test('a day may close at midnight: 00:00 as its end is sent as 24:00, and 24:00 reads back as 00:00', async () => {
  responseTargetsApi.get.mockResolvedValue(doc({ hours: week.map((day) => ({ day, open: day === 'mon', from: '08:00', to: day === 'mon' ? '24:00' : '17:00' })) }));
  show();
  const hours = await screen.findByRole('region', { name: 'Business hours' });
  expect(within(hours).getByLabelText('Monday to')).toHaveValue('00:00');
  expect(within(hours).getByLabelText('Monday to')).toHaveAccessibleDescription(/midnight/);
  fireEvent.click(within(hours).getByRole('button', { name: 'Save business hours' }));
  await waitFor(() => expect(responseTargetsApi.set).toHaveBeenCalledTimes(1));
  expect(sent().hours[0]).toEqual({ day: 'mon', open: true, from: '08:00', to: '24:00' });
});

test('a refusal of the whole hours or holidays shows on its card', async () => {
  responseTargetsApi.set.mockRejectedValueOnce(new mocks.InvalidRequestError('Check the targets.', { fields: { hours: 'Give each day once.' } }))
    .mockRejectedValueOnce(new mocks.InvalidRequestError('Check the targets.', { fields: { holidays: 'At most 100 holidays.' } }));
  show();
  const hours = await screen.findByRole('region', { name: 'Business hours' });
  fireEvent.click(within(hours).getByRole('button', { name: 'Save business hours' }));
  expect(await within(hours).findByText('Give each day once.')).toBeInTheDocument();
  const holidays = screen.getByRole('region', { name: 'Holidays' });
  fireEvent.change(within(holidays).getByLabelText('Date'), { target: { value: '2027-01-01' } });
  fireEvent.click(within(holidays).getByRole('button', { name: 'Add holiday' }));
  expect(await within(holidays).findByText('At most 100 holidays.')).toBeInTheDocument();
});

test('saving any card keeps the reader\'s own email choice, which the save does not send back', async () => {
  responseTargetsApi.get.mockResolvedValue(doc({ my_alert_email: false }));
  responseTargetsApi.set.mockImplementation(async (value: ResponseTargets) => { const { my_alert_email: _mine, ...rest } = { ...value, updated_at: '2026-10-31T01:00:00Z' } as ResponseTargets; return rest; });
  show();
  const targets = await screen.findByRole('region', { name: 'Targets' });
  expect(screen.getByRole('switch', { name: 'Email them to me' })).not.toBeChecked();
  fireEvent.click(within(targets).getByRole('button', { name: 'Save targets' }));
  await waitFor(() => expect(responseTargetsApi.set).toHaveBeenCalledTimes(1));
  await waitFor(() => expect(within(targets).getByRole('status')).toHaveTextContent('Saved'));
  expect(screen.getByRole('switch', { name: 'Email them to me' })).not.toBeChecked();
});

test('a day open from 00:00 to 00:00 is open all day', async () => {
  show();
  const hours = await screen.findByRole('region', { name: 'Business hours' });
  fireEvent.change(within(hours).getByLabelText('Monday from'), { target: { value: '00:00' } });
  fireEvent.change(within(hours).getByLabelText('Monday to'), { target: { value: '00:00' } });
  fireEvent.click(within(hours).getByRole('button', { name: 'Save business hours' }));
  await waitFor(() => expect(responseTargetsApi.set).toHaveBeenCalledTimes(1));
  expect(sent().hours[0]).toEqual({ day: 'mon', open: true, from: '00:00', to: '24:00' });
  expect(within(hours).getByLabelText('Monday to')).toHaveValue('00:00');
});

test('saving one card leaves what is being typed in the others', async () => {
  show();
  const hours = await screen.findByRole('region', { name: 'Business hours' });
  fireEvent.change(within(hours).getByLabelText('Tuesday to'), { target: { value: '18:30' } });
  const holidays = screen.getByRole('region', { name: 'Holidays' });
  fireEvent.change(within(holidays).getByLabelText('Name'), { target: { value: 'Tết' } });
  const targets = screen.getByRole('region', { name: 'Targets' });
  fireEvent.click(within(targets).getByRole('button', { name: 'Save targets' }));
  await waitFor(() => expect(within(targets).getByRole('status')).toHaveTextContent('Saved'));
  expect(within(hours).getByLabelText('Tuesday to')).toHaveValue('18:30');
  expect(within(holidays).getByLabelText('Name')).toHaveValue('Tết');
});

test('a save made after someone else saved is refused: the card says so, shows theirs, and keeps what is being typed', async () => {
  const theirs = doc({ holidays: [{ date: '2026-12-25', name: 'Christmas' }], updated_at: '2026-10-31T02:00:00Z' });
  responseTargetsApi.set.mockRejectedValueOnce(new mocks.ChangedElsewhereError('Someone saved the response targets since you read them.', { code: 'changed_elsewhere', current: theirs }));
  show();
  const targets = await screen.findByRole('region', { name: 'Targets' });
  fireEvent.change(within(targets).getByLabelText('First reply target for Low, in business hours'), { target: { value: '2' } });
  fireEvent.click(within(targets).getByRole('button', { name: 'Save targets' }));
  await waitFor(() => expect(sent()).toMatchObject({ updated_at: '2026-10-31T00:00:00Z' }));
  expect(await within(targets).findByRole('alert')).toHaveTextContent(/Someone else changed the response targets; your change was not saved\. They show as they are now/);
  expect(within(targets).getByLabelText('First reply target for Low, in business hours')).toHaveValue(2);
  expect(screen.getByRole('region', { name: 'Holidays' })).toHaveTextContent('Christmas');
  // Saving again goes with the version now shown.
  fireEvent.click(within(targets).getByRole('button', { name: 'Save targets' }));
  await waitFor(() => expect(responseTargetsApi.set).toHaveBeenCalledTimes(2));
  expect(sent()).toMatchObject({ updated_at: '2026-10-31T02:00:00Z', holidays: [{ date: '2026-12-25', name: 'Christmas' }] });
  expect(sent().targets.low.first_response_minutes).toBe(120);
});

test('after a refusal, the fields nobody here touched show and save the other admin\'s values; the touched one keeps its words', async () => {
  const theirs = doc({ warn_before_minutes: 90, holidays: [{ date: '2026-12-25', name: 'Christmas' }], updated_at: '2026-10-31T02:00:00Z' });
  responseTargetsApi.set.mockRejectedValueOnce(new mocks.ChangedElsewhereError('Someone saved the response targets since you read them.', { code: 'changed_elsewhere', current: theirs }));
  show();
  const targets = await screen.findByRole('region', { name: 'Targets' });
  fireEvent.change(within(targets).getByLabelText('First reply target for Normal, in business hours'), { target: { value: '2' } });
  fireEvent.click(within(targets).getByRole('button', { name: 'Save targets' }));
  await within(targets).findByRole('alert');
  expect(within(targets).getByLabelText('Due soon, in business hours before the target')).toHaveValue(1.5);
  expect(within(targets).getByLabelText('First reply target for Normal, in business hours')).toHaveValue(2);
  fireEvent.click(within(targets).getByRole('button', { name: 'Save targets' }));
  await waitFor(() => expect(responseTargetsApi.set).toHaveBeenCalledTimes(2));
  expect(sent()).toMatchObject({ warn_before_minutes: 90, updated_at: '2026-10-31T02:00:00Z' });
  expect(sent().targets.normal.first_response_minutes).toBe(120);
});

test('business hours after a refusal: a day touched here keeps its hours, the others show the other admin\'s', async () => {
  const theirs = doc({ hours: week.map((day) => ({ day, open: true, from: '07:00', to: '19:00' })), updated_at: '2026-10-31T02:00:00Z' });
  responseTargetsApi.set.mockRejectedValueOnce(new mocks.ChangedElsewhereError('Someone saved.', { code: 'changed_elsewhere', current: theirs }));
  show();
  const hours = await screen.findByRole('region', { name: 'Business hours' });
  fireEvent.change(within(hours).getByLabelText('Monday to'), { target: { value: '18:00' } });
  fireEvent.click(within(hours).getByRole('button', { name: 'Save business hours' }));
  await within(hours).findByText(/Someone else changed the response targets/);
  expect(within(hours).getByLabelText('Monday to')).toHaveValue('18:00');
  expect(within(hours).getByLabelText('Monday from')).toHaveValue('07:00');
  expect(within(hours).getByLabelText('Tuesday to')).toHaveValue('19:00');
});
