import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { InboxRoute } from './inbox-route';

const api = vi.hoisted(() => ({
  announcePresence: vi.fn(async (_id: string, _body: unknown): Promise<{ viewers: unknown[]; heartbeat_seconds: number }> => ({ viewers: [], heartbeat_seconds: 5 })), leavePresence: vi.fn(async () => undefined),
  me: vi.fn(async () => ({ timezone: 'UTC' })), conversations: vi.fn(async () => ({ items: [], page: 1, page_size: 50 })), conversation: vi.fn(),
  agents: vi.fn(async () => ({ items: [] })), tags: vi.fn(async () => ({ items: [] })), messages: vi.fn(async () => ({ items: [] })),
  timeEntries: vi.fn(async () => ({ items: [] })), savedReplies: vi.fn(async () => ({ items: [] })), config: vi.fn(),
  emailChannel: vi.fn(async () => ({ channel: null })), contacts: vi.fn(), agentRequests: vi.fn(async () => ({ items: [] })),
}));
vi.mock('../../api/client', () => ({ api, ReplyConflictError: class extends Error {}, TooManyRequestsError: class extends Error {} }));
vi.mock('@tanstack/react-router', async () => (await import('./test-router')).routerModule);
// The window's chunk cannot be loaded: offline, or an old tab after a deploy.
vi.mock('./new-conversation-dialog', () => { throw new Error('Failed to fetch dynamically imported module'); });

test('a window whose chunk cannot load says so, takes the focus, and closes with Escape or Close back to its button', async () => {
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><InboxRoute /></QueryClientProvider>);
  const opener = await screen.findByRole('button', { name: 'New conversation' });
  opener.focus();
  fireEvent.click(opener);
  const unavailable = await screen.findByRole('alertdialog', { name: 'New conversation' });
  expect(unavailable).toHaveTextContent('This part of the page could not be loaded.');
  expect(within(unavailable).getByRole('button', { name: 'Reload the page' })).toBeInTheDocument();
  expect(unavailable).toHaveAttribute('aria-modal', 'true');
  expect(unavailable).toHaveAccessibleDescription(/could not be loaded/);
  await waitFor(() => expect(unavailable).toHaveFocus());
  // As in the window it replaces, a click on the backdrop changes nothing, and it keeps the focus (the
  // browser would move it to the page), so Escape still closes it.
  const pressed = fireEvent.mouseDown(unavailable.parentElement!);
  fireEvent.click(unavailable.parentElement!);
  expect(pressed).toBe(false);
  expect(screen.getByRole('alertdialog')).toBeInTheDocument();
  fireEvent.keyDown(unavailable, { key: 'Escape' });
  await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument());
  expect(opener).toHaveFocus();

  fireEvent.click(opener);
  fireEvent.click(within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Close' }));
  await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument());
  expect(opener).toHaveFocus();
});
