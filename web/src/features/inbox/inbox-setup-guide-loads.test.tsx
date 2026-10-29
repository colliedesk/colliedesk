import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, waitFor } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { InboxRoute } from './inbox-route';
import { testRouter } from './test-router';

// The setup guide is the first thing an admin looks at, and it needs two downloads: its data and its chunk.
// They go out together, from the moment the inbox opens: not the chunk after the data, and not the data
// after the session (me) answers.
const api = vi.hoisted(() => ({
  me: vi.fn(() => new Promise(() => {})), conversations: vi.fn(() => new Promise(() => {})),
  agents: vi.fn(async () => ({ items: [] })), tags: vi.fn(async () => ({ items: [] })),
  setupProgress: vi.fn((): Promise<unknown> => new Promise(() => {})),
  config: vi.fn(), savedReplies: vi.fn(async () => ({ items: [] })), channelCounts: vi.fn(async () => ({ types: [] })),
  conversation: vi.fn(), messages: vi.fn(), timeEntries: vi.fn(), setSetupGuideHidden: vi.fn(),
}));
const chunk = vi.hoisted(() => ({ requested: false }));

vi.mock('../../api/client', () => ({ api, InvalidRequestError: class extends Error {}, ReplyConflictError: class extends Error {} }));
vi.mock('../../api/channels', () => ({ channelsApi: { list: async () => ({ items: [], types: [] }) } }));
vi.mock('./presence-api', () => ({ presenceApi: { announce: vi.fn(), leave: vi.fn() }, RetryLater: class extends Error {} }));
vi.mock('../../api/saved-views', () => ({ savedViewsApi: { list: async () => ({ items: [] }), trash: async () => ({ items: [] }) } }));
vi.mock('@tanstack/react-router', async () => (await import('./test-router')).routerModule);
// Evaluating the module is the browser asking for the chunk.
vi.mock('../onboarding/setup-checklist', async (original) => { chunk.requested = true; return original(); });

test('the setup data and the checklist chunk are requested together, before the session answers', async () => {
  testRouter.reset();
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><InboxRoute /></QueryClientProvider>);
  await waitFor(() => expect(api.setupProgress).toHaveBeenCalled());
  await waitFor(() => expect(chunk.requested).toBe(true));
  expect(api.me).toHaveBeenCalled();
});
