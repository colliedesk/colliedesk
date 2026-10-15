import { QueryClientProvider } from '@tanstack/react-query';
import { RouterProvider, createMemoryHistory, createRouter } from '@tanstack/react-router';
import { render, screen } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { routeTree } from './app';
import { createQueryClient } from './query-client';

const fetchMock = vi.fn();
vi.stubGlobal('fetch', fetchMock);
afterEach(() => fetchMock.mockReset());

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status });
const locked = { error: { code: 'workspace_locked', message: 'This workspace is locked because its trial or subscription ended.' } };
const meBody = (role: 'admin' | 'agent', plan: unknown) => ({
  agent: { id: '11111111-1111-4111-8111-111111111111', email: 'sam@example.com', display_name: '', role, timezone_override: null },
  workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'beta-desk', name: 'Beta Desk', timezone: 'UTC' },
  timezone: 'UTC', scope: null, plan,
});
const config = { cloud_sign_in: { enabled: true, publishable_key: 'pk' }, billing: { enabled: true }, signup: { enabled: true } };
const billing = {
  plan: 'none', trial_ends_at: '2026-10-01T09:00:00Z', locked: true, status: 'none', current_period_end: null, cancel_at_period_end: false,
  active_subscriptions: 0, complimentary: false, complimentary_until: null,
  limits: { agents: 0, replies_per_24h: null, storage_bytes: 2147483648, storage_warn_bytes: 1717986918 }, usage: { agents: 1, pending_invites: 0, replies_last_24h: 0, storage_bytes: 0 },
};

function serve(handlers: Record<string, () => Response>) {
  fetchMock.mockImplementation((url: string) => {
    const handler = handlers[url.split('?')[0]];
    return Promise.resolve(handler ? handler() : json(404, { error: { code: 'not_found', message: 'no' } }));
  });
}

function open() {
  const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: ['/'] }) });
  render(<QueryClientProvider client={createQueryClient()}><RouterProvider router={router} /></QueryClientProvider>);
}

test('a locked workspace sees the locked screen with the identity bar and an admin\'s plan choices', async () => {
  serve({
    '/api/v1/auth/me': () => json(200, meBody('admin', { plan: 'none', trial_ends_at: '2026-10-01T09:00:00Z', locked: true, trial_extended: false })),
    '/api/v1/config': () => json(200, config),
    '/api/v1/billing': () => json(200, billing),
    '/api/v1/conversations': () => json(402, locked),
  });
  open();
  expect(await screen.findByRole('heading', { level: 1, name: 'Your free trial has ended' })).toBeInTheDocument();
  expect(screen.getByRole('region', { name: 'Signed in as' })).toHaveTextContent('Beta Desk');
  expect(screen.getByRole('button', { name: /team, \$49 per month/i })).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Export all data' })).toBeInTheDocument();
});

test('a 402 workspace_locked from the inbox replaces the inbox with the locked screen', async () => {
  serve({
    '/api/v1/auth/me': () => json(200, meBody('agent', { plan: 'trial', trial_ends_at: '2026-10-30T09:00:00Z', locked: false, trial_extended: false })),
    '/api/v1/config': () => json(200, config),
    '/api/v1/conversations': () => json(402, locked),
    '/api/v1/agents': () => json(402, locked),
    '/api/v1/tags': () => json(402, locked),
    '/api/v1/channels/email': () => json(402, locked),
  });
  open();
  expect(await screen.findByRole('heading', { level: 1, name: 'This workspace is locked' })).toBeInTheDocument();
  expect(screen.getByText(/ask a workspace admin to choose a plan/i)).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Export all data' })).not.toBeInTheDocument();
  expect(screen.getByRole('region', { name: 'Signed in as' })).toBeInTheDocument();
});
