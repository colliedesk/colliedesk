import { useQuery } from '@tanstack/react-query';
import { Link, Outlet, createRootRoute, createRoute, createRouter, useRouterState } from '@tanstack/react-router';
import { api, UnauthorizedError } from './api/client';
import { InboxRoute } from './features/inbox/inbox-route';
import { RedeemRoute } from './features/auth/redeem-route';
import { SettingsRoute } from './features/settings/settings-route';
import { AppShell } from './features/shell/app-shell';
import { messages } from './messages';

function AppLayout() {
  const onRedeem = useRouterState({ select: (state) => state.location.pathname === '/redeem' });
  const me = useQuery({ queryKey: ['me'], queryFn: api.me, enabled: !onRedeem });
  return <AppShell me={onRedeem ? undefined : me.data} signedOut={!onRedeem && me.error instanceof UnauthorizedError}
    brand={<Link to="/" className="wordmark">{messages.brand}</Link>}
    links={<><Link to="/">{messages.inbox}</Link><Link to="/settings">{messages.settings}</Link></>}>
    <Outlet />
  </AppShell>;
}

const rootRoute = createRootRoute({ component: AppLayout });
const inboxRoute = createRoute({ getParentRoute: () => rootRoute, path: '/', component: InboxRoute });
const conversationRoute = createRoute({ getParentRoute: () => rootRoute, path: '/conversations/$conversationId', component: InboxRoute });
const settingsRoute = createRoute({ getParentRoute: () => rootRoute, path: '/settings', component: SettingsRoute });
const redeemRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/redeem',
  validateSearch: (search: Record<string, unknown>) => ({ token: typeof search.token === 'string' ? search.token : '' }),
  component: () => <RedeemRoute token={redeemRoute.useSearch().token} />,
});

const routeTree = rootRoute.addChildren([inboxRoute, conversationRoute, settingsRoute, redeemRoute]);
export const router = createRouter({ routeTree });

declare module '@tanstack/react-router' {
  interface Register { router: typeof router }
}
