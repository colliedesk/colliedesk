import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect } from 'react';
import { Link, Outlet, createRootRoute, redirect, createRoute, createRouter, useRouterState } from '@tanstack/react-router';
import { api, UnauthorizedError } from './api/client';
import { ContactsPart } from './features/contacts/contact-part';
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
import { SeatPausedScreen } from './features/shell/seat-paused-screen';
import { PlanNotices } from './features/shell/plan-notices';
import { TrialSurvey } from './features/shell/trial-survey';
import { messages } from './messages';
import { InboxIcon, RequestsIcon, SettingsIcon } from './ui/icons';
import { PartUnavailable } from './ui/lazy-part';
import { LogoMark } from './ui/logo';
import { resetProductAnalytics, useProductAnalytics } from './lib/product-analytics';
import { inboxQueryFromSearch } from './lib/inbox-query';

function AppLayout() {
  const path = useRouterState({ select: (state) => state.location.pathname });
  const onSignIn = path === '/sign-in';
  const me = useQuery({ queryKey: ['me'], queryFn: api.me, enabled: !onSignIn });
  const signedOut = !onSignIn && me.error instanceof UnauthorizedError;
  const config = useQuery({ queryKey: ['public-config'], queryFn: api.config, enabled: !onSignIn });
  const client = useQueryClient();
  // After sign out /auth/me answers 401, which turns the view into the signed-out screen.
  const signOut = useMutation({ mutationFn: api.logout, onSuccess: () => { resetProductAnalytics(); return client.invalidateQueries({ queryKey: ['me'] }); } });
  // A paused agent (more agents than the plan allows) gets the same treatment as a lock: nothing but why and sign out.
  const paused = me.data?.plan?.seat_paused === true;
  const locked = me.data?.plan?.locked === true || paused;
  // A locked workspace chooses its plan on the locked screen, so a remembered plan must not fire later.
  useEffect(() => { if (locked) clearCheckoutIntent(); }, [locked]);
  // New, newly assigned and changed conversations are picked up on every signed-in page, not only in the inbox.
  useInboxActivity(!onSignIn && Boolean(me.data) && !signedOut && !locked, me.data?.agent?.id);
  // The server's config enables it unless telemetry is off; otherwise the library is never loaded.
  useProductAnalytics(config.data?.product_analytics, me.data);
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
  return <SettingsNavLinkContext.Provider value={settingsRouterLink}><AppShell me={me.data} appVersion={config.data?.version} signedOut={signedOut} signIn={signIn}
    notice={me.data && <><PlanNotices me={me.data} onBillingPage={inBilling} /><TrialSurvey me={me.data} analytics={config.data?.product_analytics} /></>} lockedScreen={locked && me.data ? (paused && me.data.plan ? <SeatPausedScreen plan={messages.plans[me.data.plan.plan]} onSignOut={() => signOut.mutate()} /> : <LockedRoute me={me.data} />) : undefined}
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
// The inbox filters are in the address, so a link opens the same list (src/lib/inbox-query.ts).
const inboxRoute = createRoute({ getParentRoute: () => rootRoute, path: '/', validateSearch: inboxQueryFromSearch, component: InboxRoute });
const conversationRoute = createRoute({ getParentRoute: () => rootRoute, path: '/conversations/$conversationId', validateSearch: inboxQueryFromSearch, component: InboxRoute });
// The customer page is its own chunk (contact-part.tsx), so the inbox bundle does not carry it.
const contactRoute = createRoute({ getParentRoute: () => rootRoute, path: '/contacts/$contactId',
  component: () => <ContactsPart render={(chunk) => <chunk.ContactRoute />} failed={<main className="settings-page"><PartUnavailable /></main>} /> });
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

export const routeTree = rootRoute.addChildren([inboxRoute, conversationRoute, contactRoute, settingsRoute, requestsRoute, billingRoute, signInRoute]);
export const router = createRouter({ routeTree });

declare module '@tanstack/react-router' {
  interface Register { router: typeof router }
}
