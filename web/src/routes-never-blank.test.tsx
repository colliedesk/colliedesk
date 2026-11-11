import { QueryClientProvider } from '@tanstack/react-query';
import { RouterProvider, createMemoryHistory, createRouter } from '@tanstack/react-router';
import { cleanup, render } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { createQueryClient } from './query-client';
import { permissionsOf, rolesOf } from './test-roles';

// A page the router shows is never blank. Most pages load from a chunk of their own (ui/lazy-part.tsx), and a
// part renders nothing until its chunk is there; a route whose loader does not wait for the chunk is shown
// empty, rail and frame included, for as long as the chunk takes: it looks like the browser reloaded.
// Every route of the app is checked, so a route added later is checked too: each is opened in a fresh module
// graph (no chunk loaded yet), the router finishes loading it, and its very first render must show the page.

vi.mock('@clerk/clerk-react', () => ({
  ClerkProvider: ({ children }: { children: React.ReactNode }) => children,
  SignIn: () => <div>Clerk widget</div>,
  useAuth: () => ({ getToken: vi.fn(), isLoaded: true, isSignedIn: false }),
  useClerk: () => ({ signOut: vi.fn() }),
}));

const me = {
  agent: { id: '11111111-1111-4111-8111-111111111111', email: 'sam@example.com', display_name: 'Sam', roles: rolesOf('admin'), timezone_override: null },
  workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'beta-desk', name: 'Beta Desk', timezone: 'UTC' },
  timezone: 'UTC', token: null, permissions: permissionsOf('admin'), plan: null,
};
const config = { cloud_sign_in: { enabled: false, publishable_key: null }, billing: { enabled: false }, signup: { enabled: false },
  limits: { outgoing_attachments_bytes: 10485760, outgoing_attachments_files: 10 },
  product_analytics: { enabled: false, key: null, host: null, realm: null }, ai: { server_provider: false } };

// Every other request stays unanswered: a page must not be blank while its data is on its way either.
vi.stubGlobal('fetch', vi.fn(() => new Promise<Response>(() => {})));
afterEach(() => { cleanup(); });

const id = '33333333-3333-4333-8333-333333333333';
// addressOf fills a route's path with a value for each parameter; a splat is left empty.
function addressOf(path: string) {
  return path.replace(/\$locale\b/, 'fr').replace(/\$[A-Za-z]+/g, id).replace(/\/\$$/, '') || '/';
}

// Routes that show no page of their own, with the reason.
const noPage: Record<string, string> = {
  '/requests': 'redirects to the inbox',
};

async function paths() {
  const { routeTree } = await import('./app');
  const router = createRouter({ routeTree, context: {} });
  return Object.values(router.routesById).filter((route) => !route.isRoot).map((route) => route.fullPath as string).filter((path) => !(path in noPage));
}

test.each(await paths())('%s is not blank on its first render', async (path) => {
  vi.resetModules();
  const { routeTree } = await import('./app');
  const queryClient = createQueryClient();
  queryClient.setQueryData(['me'], me);
  queryClient.setQueryData(['public-config'], config);
  const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: [addressOf(path)] }), context: { queryClient } });
  await router.load();
  const { container } = render(<QueryClientProvider client={queryClient}><RouterProvider router={router} /></QueryClientProvider>);
  // A page draws its frame (main) at once, with its own loading state inside while its data is on its way.
  expect(container.querySelector('main'), `${path} shows no page until its chunk arrives: give the route a loader that waits for it, loader: () => thePart.ready()`).not.toBeNull();
}, 30_000);
