import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import type { Me, SetupProgress } from '../../api/schemas';
import { permissionsOf, rolesOf } from '../../test-roles';
import { QuickStart } from './quick-start';

// Evaluating the module is the browser asking for the checklist's chunk.
const chunk = vi.hoisted(() => ({ requested: false }));
vi.mock('./setup-checklist', async (original) => { chunk.requested = true; return original(); });
vi.mock('../../lib/product-analytics', () => ({ track: vi.fn() }));

const me = {
  agent: { id: '11111111-1111-4111-8111-111111111111', email: 'sam@example.com', display_name: 'Sam', roles: rolesOf('admin'), timezone_override: null },
  workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'beta-desk', name: 'Beta Desk', timezone: 'UTC' }, timezone: 'UTC', token: null,
  permissions: permissionsOf('admin'), plan: null, ai_features: { triage: false, draft: false }, onboarding_goal: null, setup_guide_hidden: false,
} as Me;

// The six steps in the order the server gives them.
const order = ['email', 'import', 'ai_drafts', 'reply_targets', 'invite', 'first_reply'];
function progress(states: Record<string, string> = {}): SetupProgress {
  const all = { email: 'none', import: 'none', ai_drafts: 'none', reply_targets: 'none', invite: 'alone', first_reply: 'none', ...states } as Record<string, string>;
  return { steps: order.map((key) => ({ key, name: key, state: all[key], detail: null })), complete: false, recommended: null } as unknown as SetupProgress;
}
const setUpDone = { email: 'working', import: 'done', first_reply: 'sent' };
const allDone = { ...setUpDone, ai_drafts: 'on', reply_targets: 'on', invite: 'joined' };

const show = (value: SetupProgress | undefined, who: Me = me) =>
  render(<QueryClientProvider client={new QueryClient()}><QuickStart progress={value} me={who} /></QueryClientProvider>);

afterEach(() => localStorage.clear());

test('the checklist chunk is asked for as soon as a step is left, before the icon is pressed', async () => {
  show(progress());
  await waitFor(() => expect(chunk.requested).toBe(true));
});

test('the icon counts the steps left in its name and shows the amber dot while a Set up step is left', () => {
  const { container, unmount } = show(progress({ email: 'working', import: 'done' }));
  const button = screen.getByRole('button', { name: 'Quick start, 4 steps left' });
  expect(button).toHaveAttribute('data-tip', 'Quick start');
  expect(button).toHaveAttribute('aria-pressed', 'false');
  expect(container.querySelector('.quick-start-dot')).not.toBeNull();
  unmount();
  // Set up is done; only the Try more steps are left: no dot.
  const again = show(progress({ ...setUpDone, ai_drafts: 'on' }));
  expect(screen.getByRole('button', { name: 'Quick start, 2 steps left' })).toBeInTheDocument();
  expect(again.container.querySelector('.quick-start-dot')).toBeNull();
});

test('one step left reads in the singular', () => {
  show(progress({ ...allDone, invite: 'alone' }));
  expect(screen.getByRole('button', { name: 'Quick start, 1 step left' })).toBeInTheDocument();
});

test('the icon is pressed while its checklist is open under it, and Escape closes it and gives the focus back', async () => {
  show(progress({ email: 'working', import: 'done' }));
  const button = screen.getByRole('button', { name: /^Quick start/ });
  fireEvent.click(button);
  expect(button).toHaveAttribute('aria-pressed', 'true');
  const popover = screen.getByRole('dialog', { name: 'Get Beta Desk ready' });
  expect(await within(popover).findByText('2 of 6')).toBeInTheDocument();
  expect(within(popover).getByRole('progressbar', { name: 'Setup progress' })).toHaveAttribute('aria-valuenow', '2');
  expect(within(popover).getAllByRole('heading', { level: 3 }).map((heading) => heading.textContent)).toEqual(['Set up', 'Try more']);
  fireEvent.keyDown(within(popover).getByRole('link', { name: /Answer your first email/ }), { key: 'Escape' });
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  expect(button).toHaveAttribute('aria-pressed', 'false');
  expect(button).toHaveFocus();
});

test('a press outside the checklist closes it', () => {
  show(progress());
  fireEvent.click(screen.getByRole('button', { name: /^Quick start/ }));
  fireEvent.pointerDown(document.body);
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
});

test('choosing a step closes the checklist, so the step opens on a clear page', async () => {
  show(progress());
  fireEvent.click(screen.getByRole('button', { name: /^Quick start/ }));
  fireEvent.click(await screen.findByRole('link', { name: /Receive customer email/ }));
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
});

test('Quick start is gone once every step is done, and while the person hid the setup guide', () => {
  const { unmount } = show(progress(allDone));
  expect(screen.queryByRole('button', { name: /^Quick start/ })).not.toBeInTheDocument();
  unmount();
  show(progress(), { ...me, setup_guide_hidden: true });
  expect(screen.queryByRole('button', { name: /^Quick start/ })).not.toBeInTheDocument();
});

test('nothing shows before the steps are known', () => {
  show(undefined);
  expect(screen.queryByRole('button', { name: /^Quick start/ })).not.toBeInTheDocument();
});

test('Skip these leaves Try more out of the list and the count, Show these brings it back, and the browser keeps the choice', async () => {
  const { unmount } = show(progress({ email: 'working', import: 'done' }));
  fireEvent.click(screen.getByRole('button', { name: /^Quick start/ }));
  const popover = screen.getByRole('dialog', { name: 'Get Beta Desk ready' });
  fireEvent.click(await within(popover).findByRole('button', { name: 'Skip these' }));
  expect(within(popover).getByText('2 of 3')).toBeInTheDocument();
  expect(within(popover).queryByRole('link', { name: /Invite your team/ })).not.toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Quick start, 1 step left' })).toBeInTheDocument();
  unmount();
  show(progress({ email: 'working', import: 'done' }));
  expect(screen.getByRole('button', { name: 'Quick start, 1 step left' })).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: /^Quick start/ }));
  fireEvent.click(await screen.findByRole('button', { name: 'Show these' }));
  expect(screen.getByRole('link', { name: /Invite your team/ })).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Quick start, 4 steps left' })).toBeInTheDocument();
});

test('with Set up done, skipping Try more finishes Quick start and the icon goes', async () => {
  show(progress(setUpDone));
  fireEvent.click(screen.getByRole('button', { name: /^Quick start/ }));
  fireEvent.click(await screen.findByRole('button', { name: 'Skip these' }));
  expect(screen.queryByRole('button', { name: /^Quick start/ })).not.toBeInTheDocument();
});
