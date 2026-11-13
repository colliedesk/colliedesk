import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, expect, test, vi } from 'vitest';
import { permissionsOf, rolesOf } from '../../../test-roles';
import type { ResponseTargets } from '../../../api/response-targets';

// Settings → Response targets while measuring is off (docs/user/response-targets.md): one card, no editor,
// and a turn-on that starts with sensible hours.
const mocks = vi.hoisted(() => {
  class InvalidRequestError extends Error { detail?: Record<string, unknown> }
  class ConflictError extends InvalidRequestError {}
  class ChangedElsewhereError extends ConflictError {}
  const api = { me: vi.fn(), config: vi.fn(), billing: vi.fn(), setupProgress: vi.fn() };
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
type Day = ResponseTargets['hours'][number]['day'];
const days: Day[] = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'];
const week = (from: string, to: string, open: Day[] = ['mon', 'tue', 'wed', 'thu', 'fri']) => days
  .map((day) => (open.includes(day) ? { day, open: true, from, to } : { day, open: false, from: '08:00', to: '17:00' }));
const none = { first_response_minutes: null, resolution_minutes: null };
const off = (change: Partial<ResponseTargets> = {}): ResponseTargets => ({
  enabled: false, timezone: null, hours: week('08:00', '17:00', []), holidays: [], targets: { urgent: none, high: none, normal: none, low: none },
  warn_before_minutes: 0, channel_ids: null, alert_email: true, my_alert_email: true, enabled_at: null, updated_at: null, ...change,
});
// What the server names for the setup flow: the hours a workspace without any starts with, and the preset targets.
const target = (first: number, resolve: number) => ({ first_response_minutes: first, resolution_minutes: resolve });
const setup = () => ({
  reply_target_hours: { kept: false, day_minutes: 540, hours: week('09:00', '18:00') },
  reply_target_presets: [{ key: 'same_day', name: 'Same day', targets: { urgent: target(60, 240), high: target(120, 540), normal: target(480, 1620), low: target(540, 2700) } }],
  reply_target_suggestion: { preset: 'same_day', basis: 'no_history', median_first_reply_minutes: null },
});
const sent = () => responseTargetsApi.set.mock.calls.at(-1)?.[0];

function show() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><TargetsRoute /></QueryClientProvider>);
}

beforeEach(() => {
  vi.clearAllMocks();
  api.me.mockResolvedValue(me('admin'));
  api.config.mockResolvedValue({ billing: { enabled: false } });
  api.setupProgress.mockResolvedValue(setup());
  responseTargetsApi.get.mockResolvedValue(off());
  responseTargetsApi.set.mockImplementation(async (value: ResponseTargets) => ({ ...value, updated_at: '2026-11-14T00:00:00Z' }));
  channelsApi.list.mockResolvedValue({ items: [], types: [] });
});

test('off: one card says targets are off and what turning them on starts with, and no editor is shown', async () => {
  show();
  const card = await screen.findByRole('region', { name: 'Response targets are off' });
  expect(await within(card).findByText('Starts with: Monday to Friday, 09:00 to 18:00, Asia/Ho_Chi_Minh · first reply Urgent 1 h · High 2 h · Normal 8 h · Low 1 day. You can change all of it.')).toBeInTheDocument();
  expect(within(card).getByText('Turn them on to mark late conversations in the inbox.')).toBeInTheDocument();
  expect(within(card).getByRole('button', { name: 'Turn on response targets' })).toBeEnabled();
  for (const name of ['Business hours', 'Holidays', 'Targets', 'Alerts']) expect(screen.queryByRole('region', { name })).not.toBeInTheDocument();
  expect(screen.queryByRole('switch')).not.toBeInTheDocument();
  expect(screen.queryByLabelText('Monday open')).not.toBeInTheDocument();
});

test('turning on with no hours ever saved saves the hours and targets the card names, in the same request', async () => {
  show();
  const button = await screen.findByRole('button', { name: 'Turn on response targets' });
  // The button waits for the hours it starts with.
  await waitFor(() => expect(button).toBeEnabled());
  fireEvent.click(button);
  await waitFor(() => expect(responseTargetsApi.set).toHaveBeenCalledTimes(1));
  expect(sent()).toMatchObject({ enabled: true, hours: week('09:00', '18:00'), warn_before_minutes: 30,
    targets: { urgent: target(60, 240), high: target(120, 540), normal: target(480, 1620), low: target(540, 2700) } });
  expect(sent()).not.toHaveProperty('enabled_at');
});

test('hours saved earlier are kept: the card names them, and turning on sends them and the saved targets unchanged', async () => {
  const saved = week('08:00', '17:00');
  const targets = { urgent: target(30, 120), high: none, normal: target(240, 540), low: none };
  api.setupProgress.mockResolvedValue({ ...setup(), reply_target_hours: { kept: true, day_minutes: 540, hours: saved } });
  responseTargetsApi.get.mockResolvedValue(off({ hours: saved, targets, warn_before_minutes: 60, timezone: 'Europe/Paris' }));
  show();
  const card = await screen.findByRole('region', { name: 'Response targets are off' });
  expect(await within(card).findByText('Starts with your saved hours: Monday to Friday, 08:00 to 17:00, Europe/Paris · first reply Urgent 30 min · High no target · Normal 4 h · Low no target. You can change all of it.')).toBeInTheDocument();
  fireEvent.click(within(card).getByRole('button', { name: 'Turn on response targets' }));
  await waitFor(() => expect(responseTargetsApi.set).toHaveBeenCalledTimes(1));
  expect(sent()).toMatchObject({ enabled: true, hours: saved, targets, warn_before_minutes: 60, timezone: 'Europe/Paris' });
});

test('on: the editor shows, and turning measuring off sends the same document with only enabled changed', async () => {
  const saved = off({ enabled: true, hours: week('08:00', '17:00'), warn_before_minutes: 60, enabled_at: '2026-11-01T00:00:00Z', updated_at: '2026-11-01T00:00:00Z',
    targets: { urgent: target(30, 120), high: none, normal: target(240, 540), low: none } });
  responseTargetsApi.get.mockResolvedValue(saved);
  show();
  expect(await screen.findByRole('region', { name: 'Business hours' })).toBeInTheDocument();
  expect(screen.queryByRole('region', { name: 'Response targets are off' })).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole('switch', { name: 'Measure response targets' }));
  await waitFor(() => expect(responseTargetsApi.set).toHaveBeenCalledTimes(1));
  expect(sent()).toMatchObject({ enabled: false, hours: saved.hours, targets: saved.targets, warn_before_minutes: 60 });
  // The save answers off: the page is the one card again, and the saved values are still in the document.
  expect(await screen.findByRole('region', { name: 'Response targets are off' })).toBeInTheDocument();
});

test('an agent reads that targets are off and is offered no button', async () => {
  api.me.mockResolvedValue(me('agent'));
  show();
  const card = await screen.findByRole('region', { name: 'Response targets are off' });
  expect(within(card).queryByRole('button')).not.toBeInTheDocument();
  expect(screen.getByText('Only admins change response targets.')).toBeInTheDocument();
});

test('the holiday help does not contradict itself', async () => {
  responseTargetsApi.get.mockResolvedValue(off({ enabled: true, hours: week('08:00', '17:00') }));
  show();
  const holidays = await screen.findByRole('region', { name: 'Holidays' });
  expect(within(holidays).queryByText(/Days off count as business hours/)).not.toBeInTheDocument();
  expect(within(holidays).getByText('No holidays added: every open weekday counts.')).toBeInTheDocument();
});
