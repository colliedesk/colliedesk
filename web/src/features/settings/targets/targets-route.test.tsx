import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, expect, test, vi } from 'vitest';
import { permissionsOf, rolesOf } from '../../../test-roles';
import type { ResponseTargets } from '../../../api/response-targets';

// One Save for the page: it sends every card that changed.
// It waits for the button to be Save again: while a save is on its way the bar says Saving….
const pressSave = async () => fireEvent.click(await screen.findByRole('button', { name: 'Save' }));

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
  agent: { id: '11111111-1111-4111-8111-111111111111', email: 'sam@example.com', display_name: '', roles: rolesOf(role), timezone_override: null }, permissions: permissionsOf(role),
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
  await pressSave();
  expect(within(hours).getByLabelText('Monday to')).toHaveAccessibleDescription(/^Ends before it starts\./);
  expect(responseTargetsApi.set).not.toHaveBeenCalled();
  fireEvent.change(within(hours).getByLabelText('Monday from'), { target: { value: '06:30' } });
  fireEvent.click(within(hours).getByRole('button', { name: 'Copy Monday to weekdays' }));
  expect(within(hours).getByLabelText('Friday from')).toHaveValue('06:30');
  fireEvent.click(within(hours).getByLabelText('Saturday open'));
  await pressSave();
  await waitFor(() => expect(responseTargetsApi.set).toHaveBeenCalledTimes(1));
  expect(sent().hours[4]).toEqual({ day: 'fri', open: true, from: '06:30', to: '07:00' });
  expect(sent().hours[5]).toMatchObject({ day: 'sat', open: true });
  expect(sent().targets).toEqual(doc().targets);
  expect(sent()).toHaveProperty('updated_at', '2026-10-31T00:00:00Z');
  expect(sent()).not.toHaveProperty('enabled_at');
});

// A target is a number and a unit: whole and half hours show in hours, anything else in minutes.
const box = (region: HTMLElement, label: string, unit: string) => ({ value: within(region).getByLabelText(label), unit: within(region).getByRole('combobox', { name: unit }) });
const lowFirstBox = (region: HTMLElement) => box(region, 'First reply target for Low, in business time', 'Unit of the first reply target for Low');
const lowResolveBox = (region: HTMLElement) => box(region, 'Resolve target for Low, in business time', 'Unit of the resolve target for Low');
const warnBox = (region: HTMLElement) => box(region, 'Due soon, in business time before the target', 'Unit of due soon');
const typeIn = (field: { value: HTMLElement; unit: HTMLElement }, amount: string, unit: 'min' | 'h') => {
  fireEvent.change(field.value, { target: { value: amount } });
  fireEvent.change(field.unit, { target: { value: unit } });
};
const saveTargets = (_region: HTMLElement) => pressSave();

test('targets show as people say them, take minutes or hours, and are minutes on the wire; an empty box is no target', async () => {
  responseTargetsApi.get.mockResolvedValue(doc({ targets: { urgent: { first_response_minutes: 15, resolution_minutes: 480 }, high: { first_response_minutes: 60, resolution_minutes: 510 },
    normal: { first_response_minutes: 90, resolution_minutes: 45 }, low: { first_response_minutes: null, resolution_minutes: null } } }));
  show();
  const targets = await screen.findByRole('region', { name: 'Targets' });
  const shown = (label: string, unitLabel: string) => { const field = box(targets, label, unitLabel); return [(field.value as HTMLInputElement).value, (field.unit as HTMLSelectElement).value].join(' '); };
  expect(shown('First reply target for Urgent, in business time', 'Unit of the first reply target for Urgent')).toBe('15 min');
  expect(shown('Resolve target for Urgent, in business time', 'Unit of the resolve target for Urgent')).toBe('8 h');
  expect(shown('First reply target for High, in business time', 'Unit of the first reply target for High')).toBe('1 h');
  expect(shown('Resolve target for High, in business time', 'Unit of the resolve target for High')).toBe('8.5 h');
  expect(shown('First reply target for Normal, in business time', 'Unit of the first reply target for Normal')).toBe('1.5 h');
  expect(shown('Resolve target for Normal, in business time', 'Unit of the resolve target for Normal')).toBe('45 min');
  typeIn(lowFirstBox(targets), '15', 'min');
  typeIn(lowResolveBox(targets), '1.5', 'h');
  fireEvent.change(box(targets, 'Resolve target for Urgent, in business time', 'Unit of the resolve target for Urgent').value, { target: { value: '' } });
  await saveTargets(targets);
  await waitFor(() => expect(responseTargetsApi.set).toHaveBeenCalledTimes(1));
  expect(sent().targets).toEqual({ urgent: { first_response_minutes: 15, resolution_minutes: null }, high: { first_response_minutes: 60, resolution_minutes: 510 },
    normal: { first_response_minutes: 90, resolution_minutes: 45 }, low: { first_response_minutes: 15, resolution_minutes: 90 } });
  expect(sent().warn_before_minutes).toBe(60);
});

test('a target of 0 or a part of a minute is refused with a clear message and nothing is sent; parts of an hour that are whole minutes are kept', async () => {
  show();
  const targets = await screen.findByRole('region', { name: 'Targets' });
  const rule = 'Use 1 minute or more, in whole minutes, or leave it empty for no target.';
  for (const [amount, unit] of [['0', 'h'], ['1.5', 'min'], ['0.01', 'h'], ['-15', 'min']] as const) {
    typeIn(lowFirstBox(targets), amount, unit);
    await saveTargets(targets);
    expect(lowFirstBox(targets).value).toHaveAccessibleDescription(rule);
  }
  expect(responseTargetsApi.set).not.toHaveBeenCalled();
  typeIn(lowFirstBox(targets), '1.1', 'h');
  typeIn(lowResolveBox(targets), '0.1', 'h');
  await saveTargets(targets);
  await waitFor(() => expect(responseTargetsApi.set).toHaveBeenCalledTimes(1));
  expect(sent().targets.low).toEqual({ first_response_minutes: 66, resolution_minutes: 6 });
});

test('due soon takes minutes or hours, and 0', async () => {
  show();
  const targets = await screen.findByRole('region', { name: 'Targets' });
  expect((warnBox(targets).value as HTMLInputElement).value).toBe('1');
  expect(warnBox(targets).unit).toHaveValue('h');
  typeIn(warnBox(targets), '5', 'min');
  await saveTargets(targets);
  await waitFor(() => expect(responseTargetsApi.set).toHaveBeenCalledTimes(1));
  expect(sent().warn_before_minutes).toBe(5);
  await waitFor(() => expect(within(targets).getByRole('status')).toHaveTextContent('Saved'));
  typeIn(warnBox(targets), '0', 'min');
  await saveTargets(targets);
  await waitFor(() => expect(responseTargetsApi.set).toHaveBeenCalledTimes(2));
  expect(sent().warn_before_minutes).toBe(0);
});

test('switching the unit keeps the number: a loaded 8 h switched to min sends 8', async () => {
  responseTargetsApi.get.mockResolvedValue(doc({ targets: { ...doc().targets, urgent: { first_response_minutes: 240, resolution_minutes: 480 } } }));
  show();
  const targets = await screen.findByRole('region', { name: 'Targets' });
  const urgentResolve = box(targets, 'Resolve target for Urgent, in business time', 'Unit of the resolve target for Urgent');
  fireEvent.change(urgentResolve.unit, { target: { value: 'min' } });
  expect(urgentResolve.value).toHaveValue(8);
  await saveTargets(targets);
  await waitFor(() => expect(responseTargetsApi.set).toHaveBeenCalledTimes(1));
  expect(sent().targets.urgent).toEqual({ first_response_minutes: 240, resolution_minutes: 8 });
});

test('a target above a year or a due soon above a week is refused, the limit named in the unit shown', async () => {
  show();
  const targets = await screen.findByRole('region', { name: 'Targets' });
  for (const [amount, unit, message] of [['525601', 'min', 'At most 525600 min (a year of business time).'], ['8761', 'h', 'At most 8760 h (a year of business time).'],
    ['1e20', 'h', 'At most 8760 h (a year of business time).']] as const) {
    typeIn(lowFirstBox(targets), amount, unit);
    await saveTargets(targets);
    expect(lowFirstBox(targets).value).toHaveAccessibleDescription(message);
  }
  for (const [amount, unit, message] of [['10081', 'min', 'At most 10080 min (a week of business time).'], ['169', 'h', 'At most 168 h (a week of business time).']] as const) {
    typeIn(warnBox(targets), amount, unit);
    await saveTargets(targets);
    expect(warnBox(targets).value).toHaveAccessibleDescription(message);
  }
  expect(responseTargetsApi.set).not.toHaveBeenCalled();
  typeIn(lowFirstBox(targets), '8760', 'h');
  typeIn(warnBox(targets), '168', 'h');
  await saveTargets(targets);
  await waitFor(() => expect(responseTargetsApi.set).toHaveBeenCalledTimes(1));
  expect(sent().targets.low.first_response_minutes).toBe(525600);
  expect(sent().warn_before_minutes).toBe(10080);
});

test('an agent reads each target the way people say it, and no due soon as no reminder', async () => {
  api.me.mockResolvedValue(me('agent'));
  responseTargetsApi.get.mockResolvedValue(doc({ targets: { urgent: { first_response_minutes: 15, resolution_minutes: 480 }, high: { first_response_minutes: 90, resolution_minutes: null },
    normal: { first_response_minutes: 60, resolution_minutes: null }, low: { first_response_minutes: null, resolution_minutes: null } }, warn_before_minutes: 0 }));
  show();
  const targets = await screen.findByRole('region', { name: 'Targets' });
  expect(targets).toHaveTextContent('First reply: 15 min · Resolve: 8 h');
  expect(targets).toHaveTextContent('First reply: 1.5 h · Resolve: No target');
  expect(targets).toHaveTextContent('First reply: 1 h · Resolve: No target');
  expect(targets).toHaveTextContent('Due soonNo reminder before the target');
  // No boxes to leave empty for someone who only reads them.
  expect(targets).not.toHaveTextContent(/Leave a box empty/);
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
  await pressSave();
  await waitFor(() => expect(sent()?.channel_ids).toEqual(['33333333-3333-4333-8333-333333333333']));
  // The page shows what was saved before the next change is made.
  await waitFor(() => expect(screen.queryByRole('region', { name: 'Unsaved changes' })).toBeNull());
  fireEvent.click(within(applies).getByRole('checkbox', { name: 'Email' }));
  await pressSave();
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
  // Saving needs a change: copying Monday to the weekdays is one, and Monday itself goes back as it was read.
  fireEvent.click(within(hours).getByRole('button', { name: 'Copy Monday to weekdays' }));
  await pressSave();
  await waitFor(() => expect(responseTargetsApi.set).toHaveBeenCalledTimes(1));
  expect(sent().hours[0]).toEqual({ day: 'mon', open: true, from: '08:00', to: '24:00' });
});

test('a refusal of the whole hours or holidays shows on its card', async () => {
  responseTargetsApi.set.mockRejectedValueOnce(new mocks.InvalidRequestError('Check the targets.', { fields: { hours: 'Give each day once.' } }))
    .mockRejectedValueOnce(new mocks.InvalidRequestError('Check the targets.', { fields: { holidays: 'At most 100 holidays.' } }));
  show();
  const hours = await screen.findByRole('region', { name: 'Business hours' });
  fireEvent.change(within(hours).getByLabelText('Monday from'), { target: { value: '09:00' } });
  await pressSave();
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
  typeIn(lowFirstBox(targets), '2', 'h');
  await pressSave();
  await waitFor(() => expect(responseTargetsApi.set).toHaveBeenCalledTimes(1));
  await waitFor(() => expect(within(targets).getByRole('status')).toHaveTextContent('Saved'));
  expect(screen.getByRole('switch', { name: 'Email them to me' })).not.toBeChecked();
});

test('a day open from 00:00 to 00:00 is open all day', async () => {
  show();
  const hours = await screen.findByRole('region', { name: 'Business hours' });
  fireEvent.change(within(hours).getByLabelText('Monday from'), { target: { value: '00:00' } });
  fireEvent.change(within(hours).getByLabelText('Monday to'), { target: { value: '00:00' } });
  await pressSave();
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
  await pressSave();
  await waitFor(() => expect(within(hours).getByRole('status')).toHaveTextContent('Saved'));
  expect(within(hours).getByLabelText('Tuesday to')).toHaveValue('18:30');
  expect(within(holidays).getByLabelText('Name')).toHaveValue('Tết');
});

test('a save made after someone else saved is refused: the card says so, shows theirs, and keeps what is being typed', async () => {
  const theirs = doc({ holidays: [{ date: '2026-12-25', name: 'Christmas' }], updated_at: '2026-10-31T02:00:00Z' });
  responseTargetsApi.set.mockRejectedValueOnce(new mocks.ChangedElsewhereError('Someone saved the response targets since you read them.', { code: 'changed_elsewhere', current: theirs }));
  show();
  const targets = await screen.findByRole('region', { name: 'Targets' });
  fireEvent.change(within(targets).getByLabelText('First reply target for Low, in business time'), { target: { value: '2' } });
  await pressSave();
  await waitFor(() => expect(sent()).toMatchObject({ updated_at: '2026-10-31T00:00:00Z' }));
  expect(await within(targets).findByRole('alert')).toHaveTextContent(/Someone else changed the response targets; your change was not saved\. They show as they are now/);
  expect(within(targets).getByLabelText('First reply target for Low, in business time')).toHaveValue(2);
  expect(screen.getByRole('region', { name: 'Holidays' })).toHaveTextContent('Christmas');
  // Saving again goes with the version now shown.
  await pressSave();
  await waitFor(() => expect(responseTargetsApi.set).toHaveBeenCalledTimes(2));
  expect(sent()).toMatchObject({ updated_at: '2026-10-31T02:00:00Z', holidays: [{ date: '2026-12-25', name: 'Christmas' }] });
  expect(sent().targets.low.first_response_minutes).toBe(120);
});

test('after a refusal, the fields nobody here touched show and save the other admin\'s values; the touched one keeps its words', async () => {
  const theirs = doc({ warn_before_minutes: 90, holidays: [{ date: '2026-12-25', name: 'Christmas' }], updated_at: '2026-10-31T02:00:00Z' });
  responseTargetsApi.set.mockRejectedValueOnce(new mocks.ChangedElsewhereError('Someone saved the response targets since you read them.', { code: 'changed_elsewhere', current: theirs }));
  show();
  const targets = await screen.findByRole('region', { name: 'Targets' });
  fireEvent.change(within(targets).getByLabelText('First reply target for Normal, in business time'), { target: { value: '2' } });
  await pressSave();
  await within(targets).findByRole('alert');
  expect(within(targets).getByLabelText('Due soon, in business time before the target')).toHaveValue(1.5);
  expect(within(targets).getByLabelText('First reply target for Normal, in business time')).toHaveValue(2);
  await pressSave();
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
  await pressSave();
  await within(hours).findByText(/Someone else changed the response targets/);
  expect(within(hours).getByLabelText('Monday to')).toHaveValue('18:00');
  expect(within(hours).getByLabelText('Monday from')).toHaveValue('07:00');
  expect(within(hours).getByLabelText('Tuesday to')).toHaveValue('19:00');
});


test('turning measuring off with hours typed asks first; declining keeps the switch on and the typing', async () => {
  const confirm = vi.spyOn(window, 'confirm').mockReturnValueOnce(false).mockReturnValueOnce(true);
  show();
  const hours = await screen.findByRole('region', { name: 'Business hours' });
  fireEvent.change(within(hours).getByLabelText('Monday from'), { target: { value: '07:30' } });
  fireEvent.click(screen.getByRole('switch', { name: 'Measure response targets' }));
  expect(confirm).toHaveBeenCalledTimes(1);
  expect(responseTargetsApi.set).not.toHaveBeenCalled();
  expect(within(hours).getByLabelText('Monday from')).toHaveValue('07:30');
  expect(screen.getByRole('switch', { name: 'Measure response targets' })).toBeChecked();
  fireEvent.click(screen.getByRole('switch', { name: 'Measure response targets' }));
  await waitFor(() => expect(responseTargetsApi.set).toHaveBeenCalledTimes(1));
  expect(sent()).toMatchObject({ enabled: false });
});

test('Undo after turning measuring off turns it on again from the toast, and says when the server refuses', async () => {
  show();
  await screen.findByRole('region', { name: 'Business hours' });
  fireEvent.click(screen.getByRole('switch', { name: 'Measure response targets' }));
  await screen.findByRole('region', { name: 'Response targets are off' });
  responseTargetsApi.set.mockRejectedValueOnce(new mocks.InvalidRequestError('Business hours are not valid.'));
  fireEvent.click(await screen.findByRole('button', { name: 'Undo' }));
  expect(await screen.findByText('Business hours are not valid.')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Undo' }));
  await waitFor(() => expect(sent()).toMatchObject({ enabled: true }));
  await waitFor(() => expect(screen.queryByRole('button', { name: 'Undo' })).toBeNull());
});

test('Save with an invalid targets box stops the run: hours already saved stay saved, the refused card keeps its typing, and a holiday being typed is left alone', async () => {
  show();
  const hours = await screen.findByRole('region', { name: 'Business hours' });
  const targets = screen.getByRole('region', { name: 'Targets' });
  const holidays = screen.getByRole('region', { name: 'Holidays' });
  fireEvent.change(within(hours).getByLabelText('Monday from'), { target: { value: '07:30' } });
  typeIn(lowFirstBox(targets), '0', 'h');
  fireEvent.change(within(holidays).getByLabelText('Name'), { target: { value: 'Tết' } });
  await pressSave();
  await waitFor(() => expect(responseTargetsApi.set).toHaveBeenCalledTimes(1));
  expect(sent().hours[0]).toMatchObject({ day: 'mon', from: '07:30' });
  expect(lowFirstBox(targets).value).toHaveAccessibleDescription('Use 1 minute or more, in whole minutes, or leave it empty for no target.');
  expect(within(targets).getByLabelText('First reply target for Low, in business time')).toHaveValue(0);
  expect(within(holidays).getByLabelText('Name')).toHaveValue('Tết');
  expect(screen.getByRole('region', { name: 'Unsaved changes' })).toHaveTextContent('Targets');
});


test('a holiday being typed has its own Add button and draws no bar, but closing the tab with it typed asks', async () => {
  show();
  const holidays = await screen.findByRole('region', { name: 'Holidays' });
  expect(window.dispatchEvent(new Event('beforeunload', { cancelable: true }))).toBe(true);
  fireEvent.change(within(holidays).getByLabelText('Name'), { target: { value: 'Tết' } });
  await waitFor(() => expect(window.dispatchEvent(new Event('beforeunload', { cancelable: true }))).toBe(false));
  expect(screen.queryByRole('region', { name: 'Unsaved changes' })).toBeNull();
});


test('turning measuring off with a holiday half typed asks first too', async () => {
  const confirm = vi.spyOn(window, 'confirm').mockReturnValueOnce(false);
  show();
  const holidays = await screen.findByRole('region', { name: 'Holidays' });
  fireEvent.change(within(holidays).getByLabelText('Name'), { target: { value: 'Tết' } });
  await waitFor(() => expect(window.dispatchEvent(new Event('beforeunload', { cancelable: true }))).toBe(false));
  fireEvent.click(screen.getByRole('switch', { name: 'Measure response targets' }));
  expect(confirm).toHaveBeenCalledTimes(1);
  expect(responseTargetsApi.set).not.toHaveBeenCalled();
});
