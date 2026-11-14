import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, within } from '@testing-library/react';
import { permissionsOf, rolesOf } from '../../../test-roles';
import { beforeEach, expect, test, vi } from 'vitest';

// "Quiet by default": Conversation numbers keeps its heading and one line of 60 characters or less per row. What a
// row used to say beyond that is the accessible description of its tip.
const mocks = vi.hoisted(() => {
  const api = { me: vi.fn(), config: vi.fn(), billing: vi.fn() };
  const numberSeriesApi = { list: vi.fn(), set: vi.fn() };
  return { api, numberSeriesApi };
});
vi.mock('../../../api/client', () => ({ api: mocks.api, InvalidRequestError: class extends Error {} }));
vi.mock('../../../api/number-series', async (actual) => ({ ...(await actual<typeof import('../../../api/number-series')>()), numberSeriesApi: mocks.numberSeriesApi }));
const { api, numberSeriesApi } = mocks;
const { NumbersSection } = await import('./numbers-section');

const series = (name: string, format: string, next: number, change: Record<string, unknown> = {}) => ({ series: name, format, next, start: 1, start_year: null, updated_at: '2026-10-31T00:00:00Z', ...change });
beforeEach(() => {
  vi.clearAllMocks();
  api.me.mockResolvedValue({
    agent: { id: '11111111-1111-4111-8111-111111111111', email: 'sam@example.com', display_name: '', roles: rolesOf('admin'), timezone_override: null }, permissions: permissionsOf('admin'),
    workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'beta', name: 'Beta', timezone: 'UTC' }, timezone: 'UTC', plan: null,
  });
  api.config.mockResolvedValue({ billing: { enabled: false } });
  numberSeriesApi.list.mockResolvedValue({ items: [series('default', '#{n}', 13), series('chd', 'CHD-{yyyy}-{n:04}', 143, { start: 142, start_year: 2026 })] });
});
function show() {
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><NumbersSection /></QueryClientProvider>);
}
function line(text: string, scope: HTMLElement | Screen = screen) {
  expect(text.length).toBeLessThanOrEqual(60);
  return (scope as Screen).getByText(text);
}
type Screen = typeof screen;

test('the heading keeps one line, and its tip says how series are used and that numbers never restart', async () => {
  show();
  await screen.findByRole('list', { name: 'Number series' });
  line('The code customers see on each conversation.');
  expect(screen.queryByText(/Every conversation is numbered in a series\./, { selector: 'p.muted-copy' })).toBeNull();
  expect(screen.getByRole('button', { name: 'About Conversation numbers' })).toHaveAccessibleDescription(
    'Every conversation is numbered in a series. Email and web forms use the default one (#12); a channel can use a series of its own, set on its page. Numbers count on and never restart, not even with the year.');
});

test('changing a series says in one line that a new format applies from now on, and the tip says what happens to codes given', async () => {
  show();
  fireEvent.click(await screen.findByRole('button', { name: 'Change the chd series' }));
  const dialog = screen.getByRole('dialog', { name: 'Change the chd series' });
  line('A new format applies from now on.', within(dialog) as never);
  expect(within(dialog).getByRole('button', { name: 'About Change the chd series' })).toHaveAccessibleDescription(
    'A new format applies to conversations opened from now on; codes already given stay as they are, and customers can still send them back.');
});

test('the fields of a new series keep one short line each, and a tip holds the rest', async () => {
  show();
  fireEvent.click(await screen.findByRole('button', { name: 'Add a series' }));
  const dialog = screen.getByRole('dialog', { name: 'Add a number series' });
  line('Lowercase letter first, then letters, digits, - or _.', within(dialog) as never);
  expect(within(dialog).getByRole('textbox', { name: 'Name' })).toHaveAccessibleDescription(/What a channel’s settings name it by; 20 characters at most\./);
  line('Use {n} once; {yyyy} adds the year it opens in.', within(dialog) as never);
  expect(within(dialog).getByRole('textbox', { name: 'Code format' })).toHaveAccessibleDescription(
    expect.stringContaining('Letters, digits, # - _ / . with {n} once ({n:04} pads to 4 digits) and {yyyy} for the year it opens in.'));
  line('Moving from another system? Enter its next number.', within(dialog) as never);
  expect(within(dialog).getByRole('spinbutton', { name: 'Taken over at' })).toHaveAccessibleDescription(
    expect.stringContaining('Moving from another system? The number it would have given next; with the year below, codes before it are that system’s.'));
});
