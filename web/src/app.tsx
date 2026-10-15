import { useQuery } from '@tanstack/react-query';
import { Link, Outlet, createRootRoute, createRoute, createRouter, useRouterState } from '@tanstack/react-router';
import { api, UnauthorizedError } from './api/client';
import { InboxRoute } from './features/inbox/inbox-route';
import { RedeemRoute } from './features/auth/redeem-route';
import { BillingRoute } from './features/settings/billing-route';
import { SettingsRoute } from './features/settings/settings-route';
import { AppShell } from './features/shell/app-shell';
import { LockedRoute } from './features/shell/locked-route';
import { PlanNotices } from './features/shell/plan-notices';
import { messages } from './messages';
import { CreditCardIcon, InboxIcon, SettingsIcon } from './ui/icons';
import { LogoMark } from './ui/logo';

function AppLayout() {
  const path = useRouterState({ select: (state) => state.location.pathname });
  const onSignIn = path === '/sign-in';
  const me = useQuery({ queryKey: ['me'], queryFn: api.me, enabled: !onSignIn });
  const signedOut = !onSignIn && me.error instanceof UnauthorizedError;
  const config = useQuery({ queryKey: ['public-config'], queryFn: api.config, enabled: !onSignIn });
  // On an instance with cloud sign-in the way back in is the sign-in page, not a new pairing link.
  const signIn = config.data?.cloud_sign_in.enabled ? <Link to="/sign-in">{messages.signIn}</Link> : undefined;
  // Sign-in pages have nowhere to navigate to yet.
  if (onSignIn) return <div className="app-main"><Outlet /></div>;
  // Billing exists only on the hosted cloud and only admins manage it.
  const showBilling = config.data?.billing.enabled === true && me.data?.agent.role === 'admin';
  const inBilling = path.startsWith('/settings/billing');
  const inSettings = path.startsWith('/settings') && !inBilling;
  const locked = me.data?.plan?.locked === true;
  return <AppShell me={me.data} signedOut={signedOut} signIn={signIn}
    notice={me.data && <PlanNotices me={me.data} />} lockedScreen={locked && me.data ? <LockedRoute me={me.data} /> : undefined}
    brand={<Link to="/" className="rail-brand" aria-label={messages.brand} data-tip={messages.brand}><LogoMark /></Link>}
    links={locked ? undefined : <>
      <Link to="/" className="rail-link" data-tip={messages.inbox} aria-label={messages.inbox} aria-current={inSettings || inBilling ? undefined : 'page'}><InboxIcon /></Link>
      <Link to="/settings" className="rail-link" data-tip={messages.settings} aria-label={messages.settings} aria-current={inSettings ? 'page' : undefined}><SettingsIcon /></Link>
      {showBilling && <Link to="/settings/billing" search={{ checkout: '' }} className="rail-link" data-tip={messages.billing} aria-label={messages.billing} aria-current={inBilling ? 'page' : undefined}><CreditCardIcon /></Link>}
    </>}>
    <Outlet />
  </AppShell>;
}

const rootRoute = createRootRoute({ component: AppLayout });
const inboxRoute = createRoute({ getParentRoute: () => rootRoute, path: '/', component: InboxRoute });
const conversationRoute = createRoute({ getParentRoute: () => rootRoute, path: '/conversations/$conversationId', component: InboxRoute });
const settingsRoute = createRoute({ getParentRoute: () => rootRoute, path: '/settings', component: SettingsRoute });
const billingRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/settings/billing',
  validateSearch: (search: Record<string, unknown>) => ({ checkout: search.checkout === 'success' ? 'success' : '' }),
  component: () => <BillingRoute checkoutReturned={billingRoute.useSearch().checkout === 'success'} />,
});
const signInRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/sign-in',
  validateSearch: (search: Record<string, unknown>): { token?: string } => (typeof search.token === 'string' ? { token: search.token } : {}),
  component: () => <RedeemRoute token={signInRoute.useSearch().token ?? ''} />,
});

export const routeTree = rootRoute.addChildren([inboxRoute, conversationRoute, settingsRoute, billingRoute, signInRoute]);
export const router = createRouter({ routeTree });

declare module '@tanstack/react-router' {
  interface Register { router: typeof router }
}
