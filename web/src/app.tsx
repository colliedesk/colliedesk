import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect } from 'react';
import { Link, Outlet, createRootRoute, redirect, createRoute, createRouter, useRouterState } from '@tanstack/react-router';
import { api, UnauthorizedError } from './api/client';
import { InboxRoute } from './features/inbox/inbox-route';
import { useInboxActivity } from './features/inbox/use-inbox-activity';
import { RequestsRoute } from './features/requests/requests-route';
import { usePendingRequests } from './features/requests/use-pending-requests';
import { RedeemRoute } from './features/auth/redeem-route';
import { BillingRoute } from './features/settings/billing-route';
import { clearCheckoutIntent, rememberCheckoutIntent } from './features/settings/checkout-intent';
import { SettingsRoute } from './features/settings/settings-route';
import { SettingsNavLinkContext } from './features/settings/settings-nav';
import type { NavItem } from './features/settings/settings-nav';
import { AppShell } from './features/shell/app-shell';
import { LockedRoute } from './features/shell/locked-route';
import { PlanNotices } from './features/shell/plan-notices';
import { messages } from './messages';
import { InboxIcon, RequestsIcon, SettingsIcon } from './ui/icons';
import { LogoMark } from './ui/logo';

function AppLayout() {
  const path = useRouterState({ select: (state) => state.location.pathname });
  const onSignIn = path === '/sign-in';
  const me = useQuery({ queryKey: ['me'], queryFn: api.me, enabled: !onSignIn });
  const signedOut = !onSignIn && me.error instanceof UnauthorizedError;
  const config = useQuery({ queryKey: ['public-config'], queryFn: api.config, enabled: !onSignIn });
  const client = useQueryClient();
  // After sign out /auth/me answers 401, which turns the view into the signed-out screen.
  const signOut = useMutation({ mutationFn: api.logout, onSuccess: () => client.invalidateQueries({ queryKey: ['me'] }) });
  const locked = me.data?.plan?.locked === true;
  // A locked workspace chooses its plan on the locked screen, so a remembered plan must not fire later.
  useEffect(() => { if (locked) clearCheckoutIntent(); }, [locked]);
  // New, newly assigned and changed conversations are picked up on every signed-in page, not only in the inbox.
  useInboxActivity(!onSignIn && Boolean(me.data) && !signedOut && !locked, me.data?.agent?.id);
  // Agent requests waiting for a person, for the rail badge; a locked workspace has none to decide.
  const waiting = usePendingRequests(!onSignIn && Boolean(me.data) && !signedOut && !locked).data?.items.length ?? 0;
  // On an instance with cloud sign-in the way back in is the sign-in page, not a new pairing link.
  const signIn = config.data?.cloud_sign_in.enabled ? <Link to="/sign-in">{messages.signIn}</Link> : undefined;
  // Sign-in pages have nowhere to navigate to yet.
  if (onSignIn) return <div className="app-main"><Outlet /></div>;
  // Billing exists only on the hosted cloud and only admins manage it.
  const inBilling = path.startsWith('/settings/billing');
  // Plan and billing is a page inside Settings, so Settings stays the current rail destination there.
  const inSettings = path.startsWith('/settings');
  const inRequests = path.startsWith('/requests');
  return <SettingsNavLinkContext.Provider value={settingsRouterLink}><AppShell me={me.data} signedOut={signedOut} signIn={signIn}
    notice={me.data && <PlanNotices me={me.data} onBillingPage={inBilling} />} lockedScreen={locked && me.data ? <LockedRoute me={me.data} /> : undefined}
    onSignOut={() => signOut.mutate()} signOutFailed={signOut.isError} signingOut={signOut.isPending}
    brand={<span className="rail-brand" role="img" aria-label={messages.brand}><LogoMark /></span>}
    links={locked ? undefined : <>
      <Link to="/" className="rail-link" data-tip={messages.inbox} aria-label={messages.inbox} aria-current={inSettings || inRequests ? undefined : 'page'}><InboxIcon /><span className="rail-label" aria-hidden="true">{messages.inbox}</span></Link>
      <Link to="/requests" className="rail-link" data-tip={messages.requests} aria-label={waiting > 0 ? messages.requestsWaiting(waiting) : messages.requests}
        aria-current={inRequests ? 'page' : undefined}><RequestsIcon /><span className="rail-label" aria-hidden="true">{messages.requests}</span>
        {waiting > 0 && <span className="rail-badge" aria-hidden="true">{waiting > 99 ? '99+' : waiting}</span>}</Link>
      <Link to="/settings" className="rail-link" data-tip={messages.settings} aria-label={messages.settings} aria-current={inSettings ? 'page' : undefined}><SettingsIcon /><span className="rail-label" aria-hidden="true">{messages.settings}</span></Link>
    </>}>
    <Outlet />
  </AppShell></SettingsNavLinkContext.Provider>;
}

// Settings sections link through the router, so moving between Settings and Plan and billing needs no reload.
// exact + includeHash: otherwise every section link would count as current on any settings page.
function settingsRouterLink(item: NavItem) {
  return <Link to={item.to} hash={item.hash} activeOptions={{ exact: true, includeHash: true }}
    aria-current={item.current ? 'page' : undefined}>{item.label}</Link>;
}

const rootRoute = createRootRoute({ component: AppLayout });
const inboxRoute = createRoute({ getParentRoute: () => rootRoute, path: '/', component: InboxRoute });
const conversationRoute = createRoute({ getParentRoute: () => rootRoute, path: '/conversations/$conversationId', component: InboxRoute });
const settingsRoute = createRoute({ getParentRoute: () => rootRoute, path: '/settings', component: SettingsRoute });
const requestsRoute = createRoute({ getParentRoute: () => rootRoute, path: '/requests', component: RequestsRoute });
const billingRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/settings/billing',
  // `buy=<plan>` is the link the website uses to send a visitor straight into that plan's checkout.
  validateSearch: (search: Record<string, unknown>): { checkout: 'success' | ''; buy?: string } => ({
    checkout: search.checkout === 'success' ? 'success' : '',
    ...(typeof search.buy === 'string' && search.buy !== '' ? { buy: search.buy } : {}),
  }),
  // Runs before sign-in is known, so the plan is kept through the sign-in and sign-up round trip.
  // `buy` is then removed from the address: Back or a reload after the checkout must not start another.
  beforeLoad: ({ search }) => {
    if (!search.buy) return;
    rememberCheckoutIntent(search.buy);
    throw redirect({ to: '/settings/billing', search: { checkout: search.checkout }, replace: true });
  },
  component: () => <BillingRoute checkoutReturned={billingRoute.useSearch().checkout === 'success'} />,
});
const signInRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/sign-in',
  validateSearch: (search: Record<string, unknown>): { token?: string } => (typeof search.token === 'string' ? { token: search.token } : {}),
  component: () => <RedeemRoute token={signInRoute.useSearch().token ?? ''} />,
});

export const routeTree = rootRoute.addChildren([inboxRoute, conversationRoute, settingsRoute, requestsRoute, billingRoute, signInRoute]);
export const router = createRouter({ routeTree });

declare module '@tanstack/react-router' {
  interface Register { router: typeof router }
}
