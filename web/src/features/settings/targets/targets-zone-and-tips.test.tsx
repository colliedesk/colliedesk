import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, expect, test, vi } from 'vitest';
import { permissionsOf, rolesOf } from '../../../test-roles';
import type { ResponseTargets } from '../../../api/response-targets';

// The zone the response-target clock counts in, named next to the zone the person's own dates use
// (Reports), and the helper lines of the page that moved into tips (docs/user/response-targets.md).
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

// workspaceZone is the zone of the workspace; mine is the zone of the person reading (what Reports shows).
const me = (role: 'admin' | 'agent', workspaceZone: string, mine: string) => ({
  agent: { id: '11111111-1111-4111-8111-111111111111', email: 'sam@example.com', display_name: '', roles: rolesOf(role), timezone_override: mine === workspaceZone ? null : mine }, permissions: permissionsOf(role),
  workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'beta', name: 'Beta', timezone: workspaceZone }, timezone: mine, plan: null,
});
const week = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'] as const;
const doc = (change: Partial<ResponseTargets> = {}): ResponseTargets => ({
  enabled: true, timezone: null,
  hours: week.map((day) => ({ day, open: !['sat', 'sun'].includes(day), from: '08:00', to: '17:00' })),
  holidays: [], targets: { urgent: { first_response_minutes: 240, resolution_minutes: 540 }, high: { first_response_minutes: 240, resolution_minutes: null },
    normal: { first_response_minutes: 240, resolution_minutes: null }, low: { first_response_minutes: null, resolution_minutes: null } },
  warn_before_minutes: 60, channel_ids: null, alert_email: true, my_alert_email: true, enabled_at: '2026-10-01T00:00:00Z', updated_at: '2026-10-31T00:00:00Z', ...change,
});

function show() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><TargetsRoute /></QueryClientProvider>);
}

beforeEach(() => {
  vi.clearAllMocks();
  api.me.mockResolvedValue(me('admin', 'UTC', 'Asia/Ho_Chi_Minh'));
  api.config.mockResolvedValue({ billing: { enabled: false } });
  responseTargetsApi.get.mockResolvedValue(doc());
  responseTargetsApi.set.mockImplementation(async (value: ResponseTargets) => ({ ...value, updated_at: '2026-10-31T01:00:00Z' }));
  channelsApi.list.mockResolvedValue({ items: [], types: [] });
});

const optionTexts = (select: HTMLElement) => Array.from(select.querySelectorAll('option')).map((option) => option.textContent);

test('a workspace on UTC read by someone whose dates are in Asia/Ho_Chi_Minh names both zones and offers theirs first', async () => {
  show();
  const hours = await screen.findByRole('region', { name: 'Business hours' });
  const zone = within(hours).getByLabelText('Time zone');
  expect(zone).toHaveValue('');
  expect(optionTexts(zone).slice(0, 2)).toEqual(['The workspace’s (UTC)', 'Your time zone (Asia/Ho_Chi_Minh)']);
  expect(optionTexts(zone).filter((text) => text === 'Asia/Ho_Chi_Minh' || text === 'Asia/Saigon')).toEqual([]);
  expect(hours).toHaveTextContent('Hours count in UTC; your dates show Asia/Ho_Chi_Minh.');
});

test('choosing their own zone from the first choices and saving the hours sets it as the targets zone', async () => {
  show();
  const hours = await screen.findByRole('region', { name: 'Business hours' });
  const zone = within(hours).getByLabelText('Time zone');
  fireEvent.change(zone, { target: { value: 'Asia/Ho_Chi_Minh' } });
  fireEvent.keyDown(zone, { key: 'Enter' });
  fireEvent.click(within(hours).getByRole('button', { name: 'Save business hours' }));
  await waitFor(() => expect(responseTargetsApi.set).toHaveBeenCalledTimes(1));
  expect(responseTargetsApi.set.mock.calls[0][0]).toMatchObject({ timezone: 'Asia/Ho_Chi_Minh' });
  // Once the clock counts in their zone the two agree: nothing to warn about, and no second copy of the zone.
  await waitFor(() => expect(hours).not.toHaveTextContent('Hours count in'));
  expect(within(hours).getByLabelText('Time zone')).toHaveValue('Asia/Ho_Chi_Minh');
  expect(optionTexts(within(hours).getByLabelText('Time zone')).filter((text) => text?.includes('Ho_Chi_Minh') || text?.includes('Saigon'))).toHaveLength(1);
});

test('when the workspace zone is the reader\'s zone, nothing is said twice and no extra choice appears', async () => {
  api.me.mockResolvedValue(me('admin', 'Asia/Ho_Chi_Minh', 'Asia/Ho_Chi_Minh'));
  show();
  const hours = await screen.findByRole('region', { name: 'Business hours' });
  expect(optionTexts(within(hours).getByLabelText('Time zone'))[0]).toBe('The workspace’s (Asia/Ho_Chi_Minh)');
  expect(optionTexts(within(hours).getByLabelText('Time zone'))).not.toContain('Your time zone (Asia/Ho_Chi_Minh)');
  expect(hours).not.toHaveTextContent('Hours count in');
});

test('two names of one zone are one zone: no warning between Asia/Saigon and Asia/Ho_Chi_Minh', async () => {
  api.me.mockResolvedValue(me('admin', 'Asia/Saigon', 'Asia/Ho_Chi_Minh'));
  show();
  const hours = await screen.findByRole('region', { name: 'Business hours' });
  expect(hours).not.toHaveTextContent('Hours count in');
  expect(optionTexts(within(hours).getByLabelText('Time zone'))).not.toContain('Your time zone (Asia/Ho_Chi_Minh)');
});

test('a zone chosen for the targets is named against the reader\'s own zone', async () => {
  api.me.mockResolvedValue(me('admin', 'Asia/Ho_Chi_Minh', 'Asia/Ho_Chi_Minh'));
  responseTargetsApi.get.mockResolvedValue(doc({ timezone: 'Europe/Paris' }));
  show();
  const hours = await screen.findByRole('region', { name: 'Business hours' });
  expect(within(hours).getByLabelText('Time zone')).toHaveValue('Europe/Paris');
  expect(hours).toHaveTextContent('Hours count in Europe/Paris; your dates show Asia/Ho_Chi_Minh.');
});

test('an agent who only reads sees the same two zones', async () => {
  api.me.mockResolvedValue(me('agent', 'UTC', 'Asia/Ho_Chi_Minh'));
  show();
  const hours = await screen.findByRole('region', { name: 'Business hours' });
  expect(hours).toHaveTextContent('Time zoneThe workspace’s (UTC)');
  expect(hours).toHaveTextContent('Hours count in UTC; your dates show Asia/Ho_Chi_Minh.');
  expect(within(hours).queryByLabelText('Time zone')).toBeNull();
});

test('the page keeps what the rule moved into tips: alerts and targets say it on a button, not in a paragraph', async () => {
  show();
  const alerts = await screen.findByRole('region', { name: 'Alerts' });
  expect(alerts).toHaveTextContent('An email when a conversation is due soon or late.');
  expect(within(alerts).getByRole('button', { name: 'About Alerts' })).toHaveAccessibleDescription('An email when a conversation is due soon and when it is late: to whoever it is assigned to, or to the admins when nobody is.');
  const targets = screen.getByRole('region', { name: 'Targets' });
  expect(targets).toHaveTextContent('Business time, by priority. Empty means no target.');
  expect(within(targets).getByRole('button', { name: 'About Targets' })).toHaveAccessibleDescription('The clock runs only while a conversation is Open; Pending, Resolved and Closed stop it. Imported conversations have no target.');
});

test('an agent reads the same tip on Targets', async () => {
  api.me.mockResolvedValue(me('agent', 'UTC', 'UTC'));
  show();
  const targets = await screen.findByRole('region', { name: 'Targets' });
  expect(within(targets).getByRole('button', { name: 'About Targets' })).toHaveAccessibleDescription(/^The clock runs only while a conversation is Open/);
});
