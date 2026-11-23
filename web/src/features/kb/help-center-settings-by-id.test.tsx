import { QueryClientProvider } from '@tanstack/react-query';
import { RouterProvider, createMemoryHistory, createRouter } from '@tanstack/react-router';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { createQueryClient } from '../../query-client';
import { permissionsOf, rolesOf } from '../../test-roles';
import { hardwareCenter, json, primaryCenter, serve } from './kb.test.fixture';
import type { Call } from './kb.test.fixture';

vi.mock('@clerk/clerk-react', () => ({
  ClerkProvider: ({ children }: { children: React.ReactNode }) => children,
  SignIn: () => <div>Clerk widget</div>,
  useAuth: () => ({ getToken: vi.fn(), isLoaded: true, isSignedIn: false }),
  useClerk: () => ({ signOut: vi.fn() }),
}));

afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

const me = {
  agent: { id: '11111111-1111-4111-8111-111111111111', email: 'sam@example.com', display_name: 'Sam', roles: rolesOf('admin'), timezone_override: null },
  workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'acme', name: 'Acme', timezone: 'UTC' },
  timezone: 'UTC', token: null, permissions: permissionsOf('admin'), plan: null,
  kb_list_columns: { columns: ['status', 'author', 'updated'], default: true },
};
const config = { cloud_sign_in: { enabled: false, publishable_key: null }, billing: { enabled: false }, signup: { enabled: false },
  limits: { outgoing_attachments_bytes: 10485760, outgoing_attachments_files: 10 },
  product_analytics: { enabled: false, key: null, host: null, realm: null }, ai: { server_provider: false } };

const noColors = { primary: null, header: null, header_text: null, background: null, text: null, link: null, footer: null };
const theme = (primary: string | null) => ({
  logo_media_id: null, favicon_media_id: null, logo_alt: '', linkback_url: null, contact_url: null, contact_phone: null,
  colors: { ...noColors, primary }, body_font: null, heading_font: null, custom_css: '', content_version: 1, updated_at: null, checks: [],
});
const noDomain = { hostname: null, status: 'none', records: [], created_at: null, verified_at: null, checked_at: null, check_error: null, certificate_requested_at: null,
  failing_since: null, pending_reason: null, awaiting_payment: false, address_url: null, address_checked_at: null, address_error: null };
const noTokens = { google_site_verification: null, bing_site_verification: null, content_version: 1, updated_at: null };
const noAnalytics = { provider: null, enabled: false, ga4_measurement_id: null, ga4_api_secret_set: false, plausible_domain: null, plausible_base_url: 'https://plausible.io',
  last_sent_at: null, last_error: null, last_error_at: null, updated_at: null, updated_by_name: null };
const noLanguages = { primary: 'en', languages: [], unlisted: [] };
const primarySettings = { enabled: true, title: '', locale: 'en', require_approval: false, content_version: 3, updated_at: null, show_tags: false, url: primaryCenter.url };

const onHardware = (call: Call) => call.search.get('help_center_id') === hardwareCenter.id;

// openSettings opens the app on Knowledge base → Settings with every help center setting answered.
async function openSettings(routes: Parameters<typeof serve>[0] = {}) {
  const calls = serve({
    'GET /api/v1/help-centers': () => json(200, { help_centers: [primaryCenter, hardwareCenter], limit: 20 }),
    'GET /api/v1/help-center': () => json(200, primarySettings),
    'GET /api/v1/help-center/theme': (call) => json(200, theme(onHardware(call) ? '#0b5cad' : null)),
    'POST /api/v1/help-center/theme/preview': () => json(200, { html: '<!doctype html><h1>Preview</h1>', ok: true, checks: [], fields: {} }),
    'PUT /api/v1/help-center/theme': (call) => json(200, { ...theme(null), ...(call.body as object), content_version: 2 }),
    'GET /api/v1/help-center/domain': () => json(200, noDomain),
    'GET /api/v1/help-center/search-console': () => json(200, noTokens),
    'GET /api/v1/help-center/analytics': () => json(200, noAnalytics),
    'GET /api/v1/help-center/languages': () => json(200, noLanguages),
    ...routes,
  });
  vi.resetModules();
  const { routeTree } = await import('../../app');
  const queryClient = createQueryClient();
  queryClient.setQueryData(['me'], me);
  queryClient.setQueryData(['public-config'], config);
  const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: ['/knowledge/settings'] }), context: { queryClient } });
  render(<QueryClientProvider client={queryClient}><RouterProvider router={router} /></QueryClientProvider>);
  return calls;
}

// openHardware opens the settings of the second help center from the list and returns the calls made
// from then on; the primary's card, drawn on the list's page, has read its own settings by then.
async function openHardware(routes: Parameters<typeof serve>[0] = {}) {
  const calls = await openSettings(routes);
  const list = await screen.findByRole('list', { name: 'Help centers' }, { timeout: 10_000 });
  await waitFor(() => expect(calls.some((call) => call.method === 'GET' && call.path === '/api/v1/help-center/theme')).toBe(true), { timeout: 10_000 });
  calls.splice(0);
  const row = within(list).getAllByRole('listitem')[1]!;
  await act(async () => { fireEvent.click(within(row).getByRole('link', { name: 'Settings of Hardware' })); });
  await screen.findByRole('heading', { name: 'Hardware', level: 1 }, { timeout: 10_000 });
  return calls;
}

test('an admin opens a second help center from the list and saves its brand on it, never on the primary', async () => {
  const calls = await openHardware();
  const primary = await screen.findByRole('textbox', { name: 'Primary color' }, { timeout: 10_000 });
  await waitFor(() => expect(primary).toHaveValue('#0b5cad'));
  fireEvent.change(primary, { target: { value: '#7a1f5c' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save brand' }));
  await waitFor(() => expect(calls.some((call) => call.method === 'PUT' && call.path === '/api/v1/help-center/theme')).toBe(true));
  const saves = calls.filter((call) => call.method === 'PUT' && call.path === '/api/v1/help-center/theme');
  expect(saves).toHaveLength(1);
  expect(saves[0]!.search.get('help_center_id')).toBe(hardwareCenter.id);
  expect((saves[0]!.body as { colors: Record<string, string | null> }).colors.primary).toBe('#7a1f5c');
  // Each setting on the page is read for this help center, none for the primary.
  const reads = calls.filter((call) => call.method === 'GET' && /^\/api\/v1\/help-center\/(theme|domain|search-console|analytics|languages)$/.test(call.path));
  expect(new Set(reads.map((call) => call.path))).toEqual(new Set(['/api/v1/help-center/theme', '/api/v1/help-center/domain',
    '/api/v1/help-center/search-console', '/api/v1/help-center/analytics', '/api/v1/help-center/languages']));
  expect(reads.filter((call) => !onHardware(call))).toEqual([]);
  expect(calls.filter((call) => call.path === '/api/v1/help-center/theme/preview').every(onHardware)).toBe(true);
}, 30_000);

test('the general settings of a second help center are saved on it by id, without the primary-only approval and redirect', async () => {
  const calls = await openHardware({
    'PATCH /api/v1/help-centers/99999999-9999-4999-8999-999999999992': (call) => json(200, { ...hardwareCenter, ...(call.body as object) }),
  });
  const general = screen.getByRole('region', { name: 'General' });
  fireEvent.change(within(general).getByRole('textbox', { name: 'Title' }), { target: { value: 'Hardware answers' } });
  fireEvent.click(within(general).getByRole('checkbox', { name: 'Publish the help center' }));
  fireEvent.click(within(general).getByRole('button', { name: 'Save help center' }));
  await waitFor(() => expect(calls.some((call) => call.method === 'PATCH')).toBe(true));
  expect(calls.find((call) => call.method === 'PATCH')?.body).toEqual({ title: 'Hardware answers', enabled: true });
  expect(calls.some((call) => call.method === 'PUT' && call.path === '/api/v1/help-center')).toBe(false);
  expect(screen.queryByRole('heading', { name: /Freshdesk/ })).not.toBeInTheDocument();
  expect(screen.queryByRole('checkbox', { name: /approv/i })).not.toBeInTheDocument();
}, 30_000);
