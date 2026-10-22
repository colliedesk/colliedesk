import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect } from 'react';
import { Link, Navigate, Outlet, createRootRoute, redirect, createRoute, createRouter, useRouterState, useNavigate } from '@tanstack/react-router';
import { api, UnauthorizedError } from './api/client';
import { ContactsPart } from './features/contacts/contact-part';
import { InboxRoute } from './features/inbox/inbox-route';
import { useInboxActivity } from './features/inbox/use-inbox-activity';
import { RequestsRoute } from './features/requests/requests-route';
import { usePendingRequests } from './features/requests/use-pending-requests';
import { RedeemRoute } from './features/auth/redeem-route';
import { clearCheckoutIntent, readCheckoutIntent, rememberCheckoutIntent } from './features/settings/checkout-intent';
import { SettingsNavLinkContext } from './features/settings/settings-nav';
import type { NavItem } from './features/settings/settings-nav';
import { AppShell } from './features/shell/app-shell';
import { SeatPausedScreen } from './features/shell/seat-paused-screen';
import { PlanNotices } from './features/shell/plan-notices';
import { messages } from './messages';
import { BookIcon, InboxIcon, RequestsIcon, SettingsIcon } from './ui/icons';
import { kbPart } from './features/kb/kb-part';
import { lazyPart, Part, PartUnavailable } from './ui/lazy-part';
import { LogoMark } from './ui/logo';
import { resetProductAnalytics, useProductAnalytics } from './lib/product-analytics';
import { inboxQueryFromSearch } from './lib/inbox-query';

// Billing, the locked screen and the trial survey are seen by few sessions, so they load from their own chunks.
// Settings is its own chunk: the rail link starts loading it on hover and focus, and the inbox never waits for it.
const settingsRoutePart = lazyPart(() => import('./features/settings/settings-route'));
const billingRoutePart = lazyPart(() => import('./features/settings/billing-route'));
const importRoutePart = lazyPart(() => import('./features/settings/import-route'));
const aiRoutePart = lazyPart(() => import('./features/settings/ai-route'));
const lockedRoutePart = lazyPart(() => import('./features/shell/locked-route'));
const trialSurveyPart = lazyPart(() => import('./features/shell/trial-survey'));

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
  // An admin's locked screen shows the plans: billing is asked for while its chunk loads, not after.
  useQuery({ queryKey: ['billing'], queryFn: api.billing, enabled: locked && !paused && me.data?.agent.role === 'admin' });
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
  // A signed-out visitor who came from the website to buy a plan has no workspace yet: they go
  // straight to sign-up with the plan in the address, not to the screen meant for invited agents.
  const buying = signedOut && inBilling ? readCheckoutIntent() : undefined;
  if (buying && config.data?.cloud_sign_in.enabled) return <Navigate to="/sign-in" search={{ buy: buying }} replace />;
  if (buying && config.isPending) return <div className="app-main" />;
  // Plan and billing is a page inside Settings, so Settings stays the current rail destination there.
  const inSettings = path.startsWith('/settings');
  const inRequests = path.startsWith('/requests');
  const inKnowledge = path.startsWith('/knowledge');
  return <SettingsNavLinkContext.Provider value={settingsRouterLink}><AppShell me={me.data} appVersion={config.data?.version} signedOut={signedOut} signIn={signIn}
    notice={me.data && <><PlanNotices me={me.data} onBillingPage={inBilling} />
      {/* The survey only ever asks a cloud trial admin, so no other session loads its chunk. */}
      {config.data?.product_analytics.realm === 'cloud' && me.data.agent.role === 'admin' && me.data.plan?.plan === 'trial'
        && <Part part={trialSurveyPart} render={({ TrialSurvey }) => <TrialSurvey me={me.data} analytics={config.data?.product_analytics} />} />}</>} lockedScreen={locked && me.data ? (paused && me.data.plan ? <SeatPausedScreen plan={messages.plans[me.data.plan.plan]} isAdmin={me.data.agent.role === 'admin'} onSignOut={() => signOut.mutate()} /> : <Part part={lockedRoutePart} failed={<main className="settings-page"><PartUnavailable /></main>} render={({ LockedRoute }) => <LockedRoute me={me.data} />} />) : undefined}
    onSignOut={() => signOut.mutate()} signOutFailed={signOut.isError} signingOut={signOut.isPending}
    brand={<Link to="/" className="rail-brand" aria-label={messages.brand}><LogoMark /></Link>}
    links={locked ? undefined : <>
      <Link to="/" className="rail-link" data-tip={messages.inbox} aria-label={messages.inbox} aria-current={inSettings || inRequests || inKnowledge ? undefined : 'page'}><InboxIcon /><span className="rail-label" aria-hidden="true">{messages.inbox}</span></Link>
      <Link to="/requests" className="rail-link" data-tip={messages.requests} aria-label={waiting > 0 ? messages.requestsWaiting(waiting) : messages.requests}
        aria-current={inRequests ? 'page' : undefined}><RequestsIcon /><span className="rail-label" aria-hidden="true">{messages.requests}</span>
        {waiting > 0 && <span className="rail-badge" aria-hidden="true">{waiting > 99 ? '99+' : waiting}</span>}</Link>
      <Link to="/knowledge" className="rail-link" data-tip={messages.knowledge} aria-label={messages.knowledge} aria-current={inKnowledge ? 'page' : undefined}
        onPointerEnter={kbPart.preload} onFocus={kbPart.preload}><BookIcon /><span className="rail-label" aria-hidden="true">{messages.knowledge}</span></Link>
      <Link to="/settings" className="rail-link" data-tip={messages.settings} aria-label={messages.settings} aria-current={inSettings ? 'page' : undefined}
        onPointerEnter={settingsRoutePart.preload} onFocus={settingsRoutePart.preload}><SettingsIcon /><span className="rail-label" aria-hidden="true">{messages.settings}</span></Link>
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
// The knowledge base is its own chunk (features/kb/kb-chunk.ts); its filters are in the address.
const kbFailed = <main className="settings-page"><PartUnavailable /></main>;
const knowledgeRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/knowledge',
  validateSearch: (search: Record<string, unknown>): { collection?: string; q?: string; status?: 'draft' | 'published' } => ({
    ...(typeof search.collection === 'string' && search.collection !== '' ? { collection: search.collection } : {}),
    ...(typeof search.q === 'string' && search.q !== '' ? { q: search.q } : {}),
    ...(search.status === 'draft' || search.status === 'published' ? { status: search.status } : {}),
  }),
  component: function Knowledge() {
    const search = knowledgeRoute.useSearch();
    return <Part part={kbPart} failed={kbFailed} render={({ KnowledgeRoute }) => <KnowledgeRoute search={search} />} />;
  },
});
// The media library: its search and the item open in the panel are in the address.
const mediaSearch = (search: Record<string, unknown>): { q?: string; kind?: 'image' | 'video'; usage?: 'in_use' | 'unused' | 'missing_alt'; sort?: 'oldest' | 'name' | 'largest'; item?: string } => ({
  ...(typeof search.q === 'string' && search.q !== '' ? { q: search.q } : {}),
  ...(search.kind === 'image' || search.kind === 'video' ? { kind: search.kind } : {}),
  ...(search.usage === 'in_use' || search.usage === 'unused' || search.usage === 'missing_alt' ? { usage: search.usage } : {}),
  ...(search.sort === 'oldest' || search.sort === 'name' || search.sort === 'largest' ? { sort: search.sort } : {}),
  ...(typeof search.item === 'string' && /^[0-9a-f-]{36}$/.test(search.item) ? { item: search.item } : {}),
});
const mediaRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/knowledge/media',
  validateSearch: mediaSearch,
  component: function Media() {
    const search = mediaRoute.useSearch();
    return <Part part={kbPart} failed={kbFailed} render={({ MediaRoute }) => <MediaRoute view="library" search={search} />} />;
  },
});
const unusedMediaRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/knowledge/media/unused',
  component: () => <Part part={kbPart} failed={kbFailed} render={({ MediaRoute }) => <MediaRoute view="unused" search={{}} />} />,
});
const mediaTrashRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/knowledge/media/trash',
  component: () => <Part part={kbPart} failed={kbFailed} render={({ MediaRoute }) => <MediaRoute view="trash" search={{}} />} />,
});
const newArticleRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/knowledge/new',
  validateSearch: (search: Record<string, unknown>): { collection?: string } => (typeof search.collection === 'string' && search.collection !== '' ? { collection: search.collection } : {}),
  component: function NewArticle() {
    const { collection } = newArticleRoute.useSearch();
    return <Part part={kbPart} failed={kbFailed} render={({ ArticleRoute }) => <ArticleRoute collectionId={collection} />} />;
  },
});
const articleRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/knowledge/articles/$articleId',
  component: function Article() {
    const { articleId } = articleRoute.useParams();
    // key: another article starts with its own form, not the last one's edits.
    return <Part part={kbPart} failed={kbFailed} render={({ ArticleRoute }) => <ArticleRoute key={articleId} articleId={articleId} />} />;
  },
});
// While the chunk loads the page keeps its frame and title, so a slow network shows Settings at once and
// nothing jumps when the sections arrive. A chunk that cannot load says so, with Reload page: a browser
// keeps a failed import failed until the page loads again.
function SettingsPage() {
  const part = settingsRoutePart.use();
  if (part.status === 'ready') return <part.module.SettingsRoute />;
  return <main className="settings-page" aria-busy={part.status === 'loading'}>
    <header><h1>{messages.settings}</h1><p>{messages.settingsDescription}</p></header>
    {part.status === 'error' ? <PartUnavailable /> : <p role="status" className="muted-copy">{messages.loadingList}</p>}
  </main>;
}
const settingsRoute = createRoute({ getParentRoute: () => rootRoute, path: '/settings', component: SettingsPage });
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
  component: function Billing() {
    const checkoutReturned = billingRoute.useSearch().checkout === 'success';
    // Billing is asked for while the page's chunk loads, not after it (billing-route.tsx asks the same).
    const me = useQuery({ queryKey: ['me'], queryFn: api.me });
    const config = useQuery({ queryKey: ['public-config'], queryFn: api.config });
    useQuery({ queryKey: ['billing'], queryFn: api.billing, enabled: me.data?.agent.role === 'admin' && config.data?.billing.enabled === true });
    return <Part part={billingRoutePart} failed={<main className="settings-page"><PartUnavailable /></main>} render={({ BillingRoute }) => <BillingRoute checkoutReturned={checkoutReturned} />} />;
  },
});
const importRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/settings/import-freshdesk',
  component: function Import() {
    const navigate = useNavigate();
    return <Part part={importRoutePart} failed={<main className="settings-page"><PartUnavailable /></main>} render={({ ImportRoute }) => <ImportRoute onOpenInbox={() => void navigate({ to: '/' })}
      onOpenBilling={() => void navigate({ to: '/settings/billing', search: { checkout: '' } })} />} />;
  },
});
const aiRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/settings/ai',
  component: function AISettings() {
    return <Part part={aiRoutePart} failed={<main className="settings-page"><PartUnavailable /></main>} render={({ AIRoute }) => <AIRoute />} />;
  },
});
const signInRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/sign-in',
  validateSearch: (search: Record<string, unknown>): { token?: string; buy?: string } => ({
    ...(typeof search.token === 'string' ? { token: search.token } : {}),
    ...(typeof search.buy === 'string' && search.buy !== '' ? { buy: search.buy } : {}),
  }),
  // `buy` stays in the address through the identity provider's round trip; the plan is remembered
  // again here so a sign-in link that names it works on its own.
  beforeLoad: ({ search }) => { if (search.buy) rememberCheckoutIntent(search.buy); },
  component: function SignIn() {
    const { token, buy } = signInRoute.useSearch();
    return <RedeemRoute token={token ?? ''} buy={buy} />;
  },
});

export const routeTree = rootRoute.addChildren([inboxRoute, conversationRoute, contactRoute, knowledgeRoute, mediaRoute, unusedMediaRoute, mediaTrashRoute, newArticleRoute, articleRoute, settingsRoute, requestsRoute, billingRoute, importRoute, aiRoute, signInRoute]);
export const router = createRouter({ routeTree });

declare module '@tanstack/react-router' {
  interface Register { router: typeof router }
}
