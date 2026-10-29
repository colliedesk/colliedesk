import { act, fireEvent, render, screen } from '@testing-library/react';
import { Outlet, RouterProvider, createMemoryHistory, createRootRoute, createRoute, createRouter } from '@tanstack/react-router';
import { expect, test } from 'vitest';
import { useInAppLinks } from './in-app-links';

function Root() {
  useInAppLinks();
  return <Outlet />;
}

function Links() {
  return <main>
    <a href="/results?window=7d">Results</a>
    <a href="/settings/channels/type/email">Email channel</a>
    <a href="/api/v1/attachments/1">Attachment</a>
    <a href="/help/getting-started">Help center</a>
    <a href="/results" download>Download</a>
    <a href="/results" target="_blank">New tab</a>
    <a href="https://colliedesk.com/pricing">Website</a>
    <a href="/results" onClick={(event) => event.preventDefault()}>Handled</a>
  </main>;
}

async function openApp() {
  const rootRoute = createRootRoute({ component: Root });
  const routeTree = rootRoute.addChildren([
    createRoute({ getParentRoute: () => rootRoute, path: '/', component: Links }),
    createRoute({ getParentRoute: () => rootRoute, path: '/results', component: () => <h1>Results page</h1> }),
    createRoute({ getParentRoute: () => rootRoute, path: '/settings/channels/$', component: () => <h1>Channels page</h1> }),
  ]);
  const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: ['/'] }) });
  render(<RouterProvider router={router} />);
  await screen.findByText('Results');
  return router;
}

// click reports whether the browser would still load the address itself.
function click(name: string, init: MouseEventInit = {}) {
  let browserLoads = true;
  act(() => { browserLoads = fireEvent.click(screen.getByText(name), init); });
  return browserLoads;
}

test('a plain link to a page of the app opens it without reloading', async () => {
  const router = await openApp();
  expect(click('Results')).toBe(false);
  expect(await screen.findByText('Results page')).toBeTruthy();
  expect(router.state.location.href).toBe('/results?window=7d');
});

test('a link to a page under a catch-all route opens in the app too', async () => {
  const router = await openApp();
  expect(click('Email channel')).toBe(false);
  expect(await screen.findByText('Channels page')).toBeTruthy();
  expect(router.state.location.pathname).toBe('/settings/channels/type/email');
});

test('links the app has no page for, downloads, new tabs and other sites are left to the browser', async () => {
  const router = await openApp();
  for (const name of ['Attachment', 'Help center', 'Download', 'New tab', 'Website']) expect(click(name)).toBe(true);
  expect(router.state.location.pathname).toBe('/');
});

test('a click with a modifier key or another button keeps opening a new tab or window', async () => {
  const router = await openApp();
  expect(click('Results', { metaKey: true })).toBe(true);
  expect(click('Results', { ctrlKey: true })).toBe(true);
  expect(click('Results', { shiftKey: true })).toBe(true);
  expect(click('Results', { button: 1 })).toBe(true);
  expect(router.state.location.pathname).toBe('/');
});

test('a link that follows itself is not followed a second time', async () => {
  const router = await openApp();
  expect(click('Handled')).toBe(false);
  expect(router.state.location.pathname).toBe('/');
});
