import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { json, serve } from '../kb/kb.test.fixture';
import { AutofixSettingsRoute } from './autofix-settings';
import { settingsOf } from './autofix.test.fixture';

afterEach(() => vi.unstubAllGlobals());

const me = {
  agent: { id: '11111111-1111-4111-8111-111111111111', email: 'sam@example.com', display_name: '', role: 'admin', timezone_override: null },
  workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'beta-desk', name: 'Beta Desk', timezone: 'UTC' },
  timezone: 'UTC', scope: null, permissions: [], plan: null,
};

// The repositories of an installation are read only while it exists: disconnecting GitHub must not ask GitHub's list
// of the installation it just removed (that answers 404, a console error for a thing that worked).
it('does not read the repositories of an installation it just disconnected', async () => {
  const connected = settingsOf();
  const installation = connected.installations[0].id;
  let disconnected = false;
  const calls = serve({
    'GET /api/v1/auth/me': () => json(200, me),
    'GET /api/v1/config': () => json(200, { autofix: { available: true, missing_settings: [] }, billing: { enabled: false }, cloud_sign_in: { enabled: false, publishable_key: null }, product_analytics: { enabled: false, key: null, host: null, realm: null } }),
    'GET /api/v1/autofix/settings': () => json(200, disconnected ? settingsOf({ installations: [], repos: [] }) : connected),
    'GET /api/v1/autofix/status': () => json(200, { connected_repos: 0, needs_person: 0 }),
    [`GET /api/v1/autofix/github/installations/${installation}/repositories`]: () => (disconnected ? json(404, { error: { code: 'not_found', message: 'Not found' } }) : json(200, { items: [] })),
    [`DELETE /api/v1/autofix/github/installations/${installation}`]: () => { disconnected = true; return json(200, settingsOf({ installations: [], repos: [] })); },
  });
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><AutofixSettingsRoute /></QueryClientProvider>);
  fireEvent.click((await screen.findAllByRole('button', { name: 'Disconnect' }))[0]);
  fireEvent.click(within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Disconnect' }));
  await waitFor(() => expect(calls.some((call) => call.method === 'DELETE')).toBe(true));
  await waitFor(() => expect(screen.queryAllByRole('button', { name: 'Disconnect' })).toHaveLength(0));
  const afterDelete = calls.slice(calls.findIndex((call) => call.method === 'DELETE'));
  expect(afterDelete.filter((call) => call.path.endsWith('/repositories'))).toEqual([]);
});
