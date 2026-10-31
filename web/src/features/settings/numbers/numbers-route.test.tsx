import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, expect, test, vi } from 'vitest';

// Settings → Conversation numbers (docs/user/inbox.md#conversation-numbers).
const mocks = vi.hoisted(() => {
  class InvalidRequestError extends Error {
    detail?: Record<string, unknown>;
    constructor(message: string, detail?: Record<string, unknown>) { super(message); this.detail = detail; }
  }
  const api = { me: vi.fn(), config: vi.fn(), billing: vi.fn() };
  const numberSeriesApi = { list: vi.fn(), set: vi.fn() };
  return { api, numberSeriesApi, InvalidRequestError };
});
vi.mock('../../../api/client', () => ({ api: mocks.api, InvalidRequestError: mocks.InvalidRequestError }));
vi.mock('../../../api/number-series', async (actual) => ({ ...(await actual<typeof import('../../../api/number-series')>()), numberSeriesApi: mocks.numberSeriesApi }));
const { api, numberSeriesApi, InvalidRequestError } = mocks;
const { NumbersRoute } = await import('./numbers-route');

const me = (role: 'admin' | 'agent') => ({
  agent: { id: '11111111-1111-4111-8111-111111111111', email: 'sam@example.com', display_name: '', role, timezone_override: null },
  workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'beta', name: 'Beta', timezone: 'Asia/Ho_Chi_Minh' }, timezone: 'Asia/Ho_Chi_Minh', plan: null,
});
const series = (name: string, format: string, next: number, change: Record<string, unknown> = {}) => ({ series: name, format, next, start: 1, start_year: null, updated_at: '2026-10-31T00:00:00Z', ...change });
const year = Number(new Intl.DateTimeFormat('en', { timeZone: 'Asia/Ho_Chi_Minh', year: 'numeric' }).format(new Date()));

function show() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><NumbersRoute /></QueryClientProvider>);
}

beforeEach(() => {
  vi.clearAllMocks();
  api.me.mockResolvedValue(me('admin'));
  api.config.mockResolvedValue({ billing: { enabled: false } });
  numberSeriesApi.list.mockResolvedValue({ items: [series('default', '#{n}', 13), series('chd', 'CHD-{yyyy}-{n:04}', 143, { start: 142, start_year: 2026 })] });
});

test('each series shows its format and the code the next conversation gets', async () => {
  show();
  const list = await screen.findByRole('list', { name: 'Number series' });
  expect(within(list).getByText('Default')).toBeInTheDocument();
  expect(within(list).getByText('Next: #13')).toBeInTheDocument();
  expect(within(list).getByText(`Next: CHD-${year}-0143`)).toBeInTheDocument();
  expect(within(list).getByText(/Codes below 142 \(from 2026 or before\) belong to the system before Colliedesk/)).toBeInTheDocument();
});

test('the next number only goes up: a lower one is refused before sending, the server’s words show when it refuses', async () => {
  numberSeriesApi.set.mockRejectedValueOnce(new InvalidRequestError('Check the series.', { fields: { next: 'The next number only counts on: use 150 or more.' } }))
    .mockResolvedValue(series('chd', 'CHD-{yyyy}-{n:04}', 160));
  show();
  fireEvent.click(await screen.findByRole('button', { name: 'Change the chd series' }));
  const dialog = screen.getByRole('dialog', { name: 'Change the chd series' });
  const next = within(dialog).getByLabelText('Next number');
  expect(next).toHaveAccessibleDescription(`The next conversation will be CHD-${year}-0143. It can go up, never down.`);
  fireEvent.change(next, { target: { value: '120' } });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
  expect(next).toHaveAccessibleDescription(/Must be 143 or more/);
  expect(numberSeriesApi.set).not.toHaveBeenCalled();
  fireEvent.change(next, { target: { value: '145' } });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
  await waitFor(() => expect(next).toHaveAccessibleDescription(/use 150 or more/));
  expect(numberSeriesApi.set).toHaveBeenCalledWith('chd', { next: 145 });
  fireEvent.change(next, { target: { value: '160' } });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
  expect(await screen.findByText('chd saved.')).toBeInTheDocument();
});

test('a new series needs a name and a format with {n}', async () => {
  numberSeriesApi.set.mockResolvedValue(series('web', 'WEB-{n}', 1));
  show();
  fireEvent.click(await screen.findByRole('button', { name: 'Add a series' }));
  const dialog = screen.getByRole('dialog', { name: 'Add a number series' });
  fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'chd' } });
  fireEvent.change(within(dialog).getByLabelText('Code format'), { target: { value: 'WEB-' } });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
  expect(within(dialog).getByLabelText('Name')).toHaveAccessibleDescription(/A series has this name already/);
  expect(within(dialog).getByLabelText('Code format')).toHaveAccessibleDescription(/Use \{n\} once/);
  fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'web' } });
  fireEvent.change(within(dialog).getByLabelText('Code format'), { target: { value: 'WEB-{n}' } });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
  await waitFor(() => expect(numberSeriesApi.set).toHaveBeenCalledWith('web', { format: 'WEB-{n}', next: 1 }));
});

test('an agent reads the series and changes nothing; a failed load says so with Try again', async () => {
  api.me.mockResolvedValue(me('agent'));
  numberSeriesApi.list.mockRejectedValueOnce(new Error('API 500'));
  show();
  expect(await screen.findByRole('alert')).toHaveTextContent('The number series could not be loaded.');
  fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
  expect(await screen.findByRole('list', { name: 'Number series' })).toBeInTheDocument();
  expect(screen.getByText('Only admins change number series.')).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /Change|Add a series/ })).toBeNull();
});
