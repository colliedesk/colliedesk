import { Link, Outlet, createRootRoute, createRoute, createRouter, useRouterState } from '@tanstack/react-router';
import { InboxRoute } from './features/inbox/inbox-route';
import { RedeemRoute } from './features/auth/redeem-route';
import { SettingsRoute } from './features/settings/settings-route';
import { messages } from './messages';
import { InboxIcon, SettingsIcon } from './ui/icons';

function AppLayout() {
  const path = useRouterState({ select: (state) => state.location.pathname });
  // Sign-in pages have nowhere to navigate to yet.
  if (path === '/redeem') return <Outlet />;
  const inSettings = path.startsWith('/settings');
  return (
    <div className="app-shell">
      <nav className="app-rail" aria-label={messages.brand}>
        <Link to="/" className="rail-brand" aria-label={messages.brand}><span aria-hidden="true">{messages.brandInitial}</span></Link>
        <Link to="/" className="rail-link" title={messages.inbox} aria-current={inSettings ? undefined : 'page'}>
          <InboxIcon /><span className="sr-only">{messages.inbox}</span></Link>
        <Link to="/settings" className="rail-link" title={messages.settings} aria-current={inSettings ? 'page' : undefined}>
          <SettingsIcon /><span className="sr-only">{messages.settings}</span></Link>
      </nav>
      <div className="app-main"><Outlet /></div>
    </div>
  );
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
