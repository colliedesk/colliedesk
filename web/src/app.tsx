import { Link, Outlet, createRootRoute, createRoute, createRouter } from '@tanstack/react-router';
import { InboxRoute } from './features/inbox/inbox-route';
import { RedeemRoute } from './features/auth/redeem-route';
import { SettingsRoute } from './features/settings/settings-route';
import { messages } from './messages';

function AppLayout() {
  return <><nav className="app-nav"><Link to="/" className="wordmark">{messages.brand}</Link>
    <div><Link to="/">{messages.inbox}</Link><Link to="/settings">{messages.settings}</Link></div></nav><Outlet /></>;
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
