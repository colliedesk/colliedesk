import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render as renderPlain, screen, within } from '@testing-library/react';
import type { ReactElement, ReactNode } from 'react';
import { afterEach, expect, test, vi } from 'vitest';
import { SetupChecklist } from './setup-checklist';
import type { SetupProgress } from '../../api/schemas';

const client = new QueryClient();
const render = (ui: ReactElement) => renderPlain(ui, { wrapper: ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider> });

const tracked = vi.hoisted(() => vi.fn());
vi.mock('../../lib/product-analytics', () => ({ track: tracked }));

afterEach(() => { tracked.mockReset(); localStorage.clear(); });

function progress(states: { email?: string; import?: string; invite?: string; first_reply?: string }, order = ['email', 'import', 'invite', 'first_reply'], recommended: string | null = null): SetupProgress {
  const state = { email: 'none', import: 'none', invite: 'alone', first_reply: 'none', ...states } as Record<string, string>;
  const steps = order.map((key) => ({ key, state: state[key], detail: null }));
  const done = state.email === 'working' && state.import === 'done' && state.invite === 'joined' && state.first_reply === 'sent';
  return { steps, complete: done, recommended } as SetupProgress;
}

const rows = () => within(screen.getByRole('region', { name: 'Finish setting up' })).getAllByRole('listitem');

test('an admin sees the four steps in the order the server gives, each with its state', () => {
  render(<SetupChecklist progress={progress({ email: 'waiting' })} admin defaultOpen onHide={vi.fn()} />);
  expect(screen.getByRole('heading', { name: 'Finish setting up' })).toBeInTheDocument();
  expect(screen.getByText('0 of 4')).toBeInTheDocument();
  expect(rows().map((row) => row.textContent)).toEqual([
    expect.stringContaining('Receive customer email'),
    expect.stringContaining('Import from Freshdesk'),
    expect.stringContaining('Invite your team'),
    expect.stringContaining('Answer your first email'),
  ]);
  expect(rows()[0]).toHaveTextContent('Waiting for the first email');
  expect(rows()[1]).toHaveTextContent('Moves your history over');
  expect(rows()[2]).toHaveTextContent('Just you so far');
  expect(rows()[3]).toHaveTextContent('Not yet');
});

test('finished steps read Done, and the bar counts them', () => {
  render(<SetupChecklist progress={progress({ email: 'working', import: 'done' })} admin defaultOpen onHide={vi.fn()} />);
  expect(screen.getByText('2 of 4')).toBeInTheDocument();
  expect(screen.getByRole('progressbar', { name: 'Setup progress' })).toHaveAttribute('aria-valuenow', '2');
  expect(screen.getByRole('progressbar', { name: 'Setup progress' })).toHaveAttribute('aria-valuemax', '4');
  expect(rows()[0]).toHaveTextContent('Done');
  expect(rows()[1]).toHaveTextContent('Done');
  expect(rows()[2]).not.toHaveTextContent('Done');
  expect(rows()[0]).toHaveAttribute('data-state', 'done');
});

test('a finished step is marked with a check, an open one with an empty circle', () => {
  render(<SetupChecklist progress={progress({ email: 'working' })} admin defaultOpen onHide={vi.fn()} />);
  expect(rows()[0].querySelector('.setup-mark svg.icon')).not.toBeNull();
  expect(rows()[1].querySelector('.setup-mark svg')).toBeNull();
});

test('every step links to the screen that finishes it, with plain links', () => {
  render(<SetupChecklist progress={progress({})} admin defaultOpen onHide={vi.fn()} />);
  expect(screen.getByRole('link', { name: /Receive customer email/ })).toHaveAttribute('href', '/settings/channels/type/email');
  expect(screen.getByRole('link', { name: /Import from Freshdesk/ })).toHaveAttribute('href', '/settings/import-freshdesk');
  expect(screen.getByRole('link', { name: /Invite your team/ })).toHaveAttribute('href', '/settings');
  expect(screen.getByRole('link', { name: /Answer your first email/ })).toHaveAttribute('href', '/');
});

test('only the first step still to do is the primary action', () => {
  render(<SetupChecklist progress={progress({ email: 'working' })} admin defaultOpen onHide={vi.fn()} />);
  const links = screen.getAllByRole('link');
  expect(links.filter((link) => link.classList.contains('primary-action')).map((link) => link.textContent)).toEqual([expect.stringContaining('Import from Freshdesk')]);
});

test('the recommended step from the sign-up goal is marked and listed first', () => {
  render(<SetupChecklist progress={progress({}, ['import', 'email', 'invite', 'first_reply'], 'import')} admin defaultOpen onHide={vi.fn()} />);
  expect(rows()[0]).toHaveTextContent('Import from Freshdesk');
  expect(rows()[0]).toHaveTextContent('Recommended');
  expect(rows()[1]).not.toHaveTextContent('Recommended');
});

test('an agent who is not an admin sees only the steps they can take', () => {
  render(<SetupChecklist progress={progress({})} admin={false} defaultOpen onHide={vi.fn()} />);
  expect(rows()).toHaveLength(1);
  expect(rows()[0]).toHaveTextContent('Answer your first email');
  expect(screen.getByText('0 of 1')).toBeInTheDocument();
  expect(screen.queryByRole('link', { name: /Receive customer email/ })).not.toBeInTheDocument();
  expect(screen.queryByRole('link', { name: /Invite your team/ })).not.toBeInTheDocument();
});

test('the goal ai_agents puts the AI agent guide first, and it is not counted', () => {
  render(<SetupChecklist progress={progress({ email: 'working' })} admin goal="ai_agents" defaultOpen onHide={vi.fn()} />);
  expect(rows()[0]).toHaveTextContent('Connect an AI agent');
  expect(screen.getByRole('link', { name: /Connect an AI agent/ })).toHaveAttribute('href', 'https://colliedesk.com/docs/mcp');
  expect(screen.getByText('1 of 4')).toBeInTheDocument();
  expect(rows()).toHaveLength(5);
});

test('Hide asks to hide the guide', () => {
  const onHide = vi.fn();
  render(<SetupChecklist progress={progress({})} admin defaultOpen onHide={onHide} />);
  fireEvent.click(screen.getByRole('button', { name: 'Hide' }));
  expect(onHide).toHaveBeenCalledTimes(1);
});

test('selecting a step reports it started, with the step key only', () => {
  render(<SetupChecklist progress={progress({})} admin defaultOpen onHide={vi.fn()} />);
  fireEvent.click(screen.getByRole('link', { name: /Invite your team/ }));
  expect(tracked).toHaveBeenCalledWith('onboarding_step_started', { step: 'invite' });
});

test('a step that turns done while the guide is on screen is reported once, and the first render reports nothing', () => {
  const { rerender } = render(<SetupChecklist progress={progress({ email: 'waiting' })} admin defaultOpen onHide={vi.fn()} />);
  expect(tracked).not.toHaveBeenCalled();
  rerender(<SetupChecklist progress={progress({ email: 'working' })} admin defaultOpen onHide={vi.fn()} />);
  expect(tracked).toHaveBeenCalledTimes(1);
  expect(tracked).toHaveBeenCalledWith('onboarding_step_completed', { step: 'email' });
  rerender(<SetupChecklist progress={progress({ email: 'working' })} admin defaultOpen onHide={vi.fn()} />);
  expect(tracked).toHaveBeenCalledTimes(1);
});

test('closed, the guide is one button in the corner that carries the count; pressing it opens the steps', () => {
  render(<SetupChecklist progress={progress({ email: 'working' })} admin onHide={vi.fn()} />);
  expect(screen.queryByRole('region', { name: 'Finish setting up' })).not.toBeInTheDocument();
  const launcher = screen.getByRole('button', { name: /Finish setting up/ });
  expect(launcher).toHaveTextContent('1 of 4');
  expect(launcher).toHaveAttribute('aria-expanded', 'false');
  fireEvent.click(launcher);
  expect(rows()).toHaveLength(4);
});

test('Minimize folds the steps back into the button, focus goes back to it, and the next visit starts folded', () => {
  const { unmount } = render(<SetupChecklist progress={progress({})} admin defaultOpen onHide={vi.fn()} />);
  fireEvent.click(screen.getByRole('button', { name: 'Minimize' }));
  expect(screen.queryByRole('region', { name: 'Finish setting up' })).not.toBeInTheDocument();
  expect(screen.getByRole('button', { name: /Finish setting up/ })).toHaveFocus();
  unmount();
  render(<SetupChecklist progress={progress({})} admin defaultOpen onHide={vi.fn()} />);
  expect(screen.queryByRole('region', { name: 'Finish setting up' })).not.toBeInTheDocument();
});

test('a guide the agent opened is open again on the next visit, and focus moves into it', () => {
  const { unmount } = render(<SetupChecklist progress={progress({})} admin onHide={vi.fn()} />);
  fireEvent.click(screen.getByRole('button', { name: /Finish setting up/ }));
  expect(screen.getByRole('button', { name: 'Minimize' })).toHaveFocus();
  unmount();
  render(<SetupChecklist progress={progress({})} admin onHide={vi.fn()} />);
  expect(screen.getByRole('region', { name: 'Finish setting up' })).toBeInTheDocument();
});

test('Escape inside the open guide folds it', () => {
  render(<SetupChecklist progress={progress({})} admin defaultOpen onHide={vi.fn()} />);
  fireEvent.keyDown(screen.getByRole('link', { name: /Invite your team/ }), { key: 'Escape' });
  expect(screen.queryByRole('region', { name: 'Finish setting up' })).not.toBeInTheDocument();
  expect(screen.getByRole('button', { name: /Finish setting up/ })).toHaveFocus();
});
