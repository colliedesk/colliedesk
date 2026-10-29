import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useCallback, useEffect } from 'react';
import { Link, Navigate, Outlet, createRootRoute, redirect, rootRouteId, createRoute, createRouter, useRouterState, useNavigate } from '@tanstack/react-router';
import { api, UnauthorizedError } from './api/client';
import { ContactsPart } from './features/contacts/contact-part';
import { InboxRoute } from './features/inbox/inbox-route';
import { useInboxActivity } from './features/inbox/use-inbox-activity';
import { usePendingRequests } from './features/requests/use-pending-requests';
import { RedeemRoute } from './features/auth/redeem-route';
import { HandoffSignIn } from './features/auth/handoff-sign-in';
import { clearCheckoutIntent, readCheckoutIntent, rememberCheckoutIntent, signInCheckoutIntent } from './features/settings/checkout-intent';
import { SettingsNavLinkContext } from './features/settings/settings-nav';
import type { NavItem } from './features/settings/settings-nav';
import type { ReactNode } from 'react';
import { AppShell } from './features/shell/app-shell';
import { SeatPausedScreen } from './features/shell/seat-paused-screen';
import { PlanNotices } from './features/shell/plan-notices';
import { messages } from './messages';
import { BookIcon, ChartIcon, InboxIcon, RequestsIcon, SettingsIcon } from './ui/icons';
import { autofixPart } from './features/autofix/autofix-part';
import { autofixPreviewOn } from './features/autofix/autofix-preview';
import { kbPart } from './features/kb/kb-part';
import { channelsRoutePart, preloadChannelsPage } from './features/settings/channels/channels-preload';
import { lazyPart, Part, PartUnavailable } from './ui/lazy-part';
import { LogoMark } from './ui/logo';
import { resetProductAnalytics, useProductAnalytics } from './lib/product-analytics';
import { forgetAllDrafts } from './features/inbox/reply-draft-storage';
import { inboxQueryFromSearch } from './lib/inbox-query';
import { signInFormReady } from './lib/sign-in-ready';
import { useInAppLinks } from './lib/in-app-links';

// Billing, the locked screen and the trial survey are seen by few sessions, so they load from their own chunks.
// Settings is its own chunk: the rail link starts loading it on hover and focus, and the inbox never waits for it.
const settingsRoutePart = lazyPart(() => import('./features/settings/settings-route'));
const billingRoutePart = lazyPart(() => import('./features/settings/billing-route'));
const importRoutePart = lazyPart(() => import('./features/settings/import-route'));
const aiRoutePart = lazyPart(() => import('./features/settings/ai-route'));
// Results is its own chunk too: the rail link starts loading it on hover and focus.
const resultsRoutePart = lazyPart(() => import('./features/results/results-route'));
// Agent requests are their own chunk too: the rail link starts loading it on hover and focus.
const requestsRoutePart = lazyPart(() => import('./features/requests/requests-route'));
const resultsSharePart = lazyPart(() => import('./features/results/results-share-route'));
const lockedRoutePart = lazyPart(() => import('./features/shell/locked-route'));
const trialSurveyPart = lazyPart(() => import('./features/shell/trial-survey'));

function AppLayout() {
  const path = useRouterState({ select: (state) => state.location.pathname });
  // A bare link to a page of the app opens without reloading, wherever it is written.
  useInAppLinks();
  // The public page of a results share link has no session and no rail.
  const onSignIn = path === '/sign-in' || path === '/r';
  // An address that matches no page is no rail destination either.
  const notFound = useRouterState({ select: (state) => state.matches.at(-1)?.routeId === rootRouteId });
  const me = useQuery({ queryKey: ['me'], queryFn: api.me, enabled: !onSignIn });
  const signedOut = !onSignIn && me.error instanceof UnauthorizedError;
  const config = useQuery({ queryKey: ['public-config'], queryFn: api.config, enabled: !onSignIn });
  const client = useQueryClient();
  // After sign out /auth/me answers 401, which turns the view into the signed-out screen.
  const signOut = useMutation({ mutationFn: api.logout, onSuccess: () => { resetProductAnalytics(); forgetAllDrafts(); return client.invalidateQueries({ queryKey: ['me'] }); } });
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
  // On a sign-in page the analytics library waits for the sign-in form, so a slow connection loads the form first.
  useProductAnalytics(config.data?.product_analytics, me.data, onSignIn ? signInFormReady : undefined);
  // Agent requests waiting for a person, for the rail badge; a locked workspace has none to decide.
  const waiting = usePendingRequests(!onSignIn && Boolean(me.data) && !signedOut && !locked).data?.items.length ?? 0;
  // Fixes is in the Autofix chunk, loaded only where the server runs Autofix and this browser turned its preview on.
  const fixes = autofixPreviewOn() && config.data?.autofix?.available === true && Boolean(me.data) && !signedOut && !locked;
  // Sign-in pages have nowhere to navigate to yet.
  if (onSignIn) return <div className="app-main"><Outlet /></div>;
  // Billing exists only on the hosted cloud and only admins manage it.
  const inBilling = path.startsWith('/settings/billing');
  // A signed-out visitor who came from the website to buy a plan has no workspace yet: they go
  // straight to sign-up with the plan in the address, not to the screen meant for invited agents.
  const buying = signedOut && inBilling ? readCheckoutIntent() : undefined;
  if (buying && config.data?.cloud_sign_in.enabled) return <Navigate to="/sign-in" search={{ buy: buying }} replace />;
  if (buying && config.isPending) return <div className="app-main" />;
  // On an instance with cloud sign-in the way back in is the sign-in page, not a new pairing link.
  // The marker makes that page end the identity provider session too, so the next sign-in is a
  // deliberate one (sign out, a revoked or an expired session alike).
  if (signedOut && config.data?.cloud_sign_in.enabled) return <Navigate to="/sign-in" search={{ signed_out: true }} replace />;
  if (signedOut && config.isPending) return <div className="app-main" />;
  // Plan and billing is a page inside Settings, so Settings stays the current rail destination there.
  const inSettings = path.startsWith('/settings');
  const inRequests = path.startsWith('/requests');
  const inKnowledge = path.startsWith('/knowledge');
  const inResults = path.startsWith('/results');
  const inFixes = path.startsWith('/fixes');
  return <SettingsNavLinkContext.Provider value={settingsRouterLink}><AppShell me={me.data} appVersion={config.data?.version} signedOut={signedOut}
    notice={me.data && <><PlanNotices me={me.data} onBillingPage={inBilling} />
      {/* The survey only ever asks a cloud trial admin, so no other session loads its chunk. */}
      {config.data?.product_analytics.realm === 'cloud' && me.data.agent.role === 'admin' && me.data.plan?.plan === 'trial'
        && <Part part={trialSurveyPart} render={({ TrialSurvey }) => <TrialSurvey me={me.data} analytics={config.data?.product_analytics} />} />}</>} lockedScreen={locked && me.data ? (paused && me.data.plan ? <SeatPausedScreen plan={messages.plans[me.data.plan.plan]} isAdmin={me.data.agent.role === 'admin'} onSignOut={() => signOut.mutate()} /> : <Part part={lockedRoutePart} failed={<main className="settings-page"><PartUnavailable /></main>} render={({ LockedRoute }) => <LockedRoute me={me.data} />} />) : undefined}
    onSignOut={() => signOut.mutate()} signOutFailed={signOut.isError} signingOut={signOut.isPending}
    brand={<Link to="/" className="rail-brand" aria-label={messages.brand}><LogoMark /></Link>}
    links={locked ? undefined : <>
      <Link to="/" className="rail-link" data-tip={messages.inbox} aria-label={messages.inbox} aria-current={inSettings || inRequests || inKnowledge || inResults || inFixes || notFound ? undefined : 'page'}><InboxIcon /><span className="rail-label" aria-hidden="true">{messages.inbox}</span></Link>
      <Link to="/requests" className="rail-link" data-tip={messages.requests} aria-label={waiting > 0 ? messages.requestsWaiting(waiting) : messages.requests}
        aria-current={inRequests ? 'page' : undefined} onPointerEnter={requestsRoutePart.preload} onFocus={requestsRoutePart.preload}><RequestsIcon /><span className="rail-label" aria-hidden="true">{messages.requests}</span>
        {waiting > 0 && <span className="rail-badge" aria-hidden="true">{waiting > 99 ? '99+' : waiting}</span>}</Link>
      {fixes && <Part part={autofixPart} render={({ FixesRailLink }) => <FixesRailLink current={inFixes} />} />}
      <Link to="/knowledge" className="rail-link" data-tip={messages.knowledge} aria-label={messages.knowledge} aria-current={inKnowledge ? 'page' : undefined}
        onPointerEnter={kbPart.preload} onFocus={kbPart.preload}><BookIcon /><span className="rail-label" aria-hidden="true">{messages.knowledge}</span></Link>
      <Link to="/results" className="rail-link" data-tip={messages.results} aria-label={messages.results} aria-current={inResults ? 'page' : undefined}
        onPointerEnter={resultsRoutePart.preload} onFocus={resultsRoutePart.preload}><ChartIcon /><span className="rail-label" aria-hidden="true">{messages.results}</span></Link>
      <Link to="/settings" className="rail-link" data-tip={messages.settings} aria-label={messages.settings} aria-current={inSettings ? 'page' : undefined}
        onPointerEnter={settingsRoutePart.preload} onFocus={settingsRoutePart.preload}><SettingsIcon /><span className="rail-label" aria-hidden="true">{messages.settings}</span></Link>
    </>}>
    <Outlet />
  </AppShell></SettingsNavLinkContext.Provider>;
}

// Settings sections link through the router, so moving between Settings and Plan and billing needs no reload.
// exact + includeHash: otherwise every section link would count as current on any settings page.
function settingsRouterLink(item: NavItem) {
  const preload = item.to.startsWith('/settings/channels') ? channelsRoutePart.preload : undefined;
  // /settings/channels, the type homes under it and /settings/ai are routes with a splat, paths the route types do not list.
  return <Link to={item.to as Exclude<NavItem['to'], `/settings/channels${string}` | '/settings/ai'>} hash={item.hash} activeOptions={{ exact: true, includeHash: true }}
    aria-current={item.current ? 'page' : undefined} onPointerEnter={preload} onFocus={preload}>{item.label}</Link>;
}

// An unknown address inside the app says so and offers the way back, in the frame of every other page.
function NotFoundPage() {
  return <main className="settings-page">
    <header><h1>{messages.pageNotFound}</h1><p>{messages.pageNotFoundExplanation}</p></header>
    <Link to="/">{messages.backToInbox}</Link>
  </main>;
}

const rootRoute = createRootRoute({ component: AppLayout, notFoundComponent: NotFoundPage });
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
const requestsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/requests',
  component: () => <Part part={requestsRoutePart} failed={<main className="settings-page"><PartUnavailable /></main>} render={({ RequestsRoute }) => <RequestsRoute />} />,
});
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
    useQuery({ queryKey: ['billing'], queryFn: api.billing, enabled: me.data?.agent.role === 'admin' && (config.data?.billing.enabled === true || config.data?.billing.switched_off === true) });
    return <Part part={billingRoutePart} failed={<main className="settings-page"><PartUnavailable /></main>} render={({ BillingRoute }) => <BillingRoute checkoutReturned={checkoutReturned} />} />;
  },
});
const importRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/settings/import-freshdesk',
  component: function Import() {
    return <Part part={importRoutePart} failed={<main className="settings-page"><PartUnavailable /></main>} render={({ ImportRoute }) => <ImportRoute />} />;
  },
});
// Settings → Built-in AI, with a chat's transcript below it (/settings/ai/chats/<id>): the chunk reads its path.
const aiRoute = createRoute({
  getParentRoute: () => rootRoute, path: '/settings/ai/$',
  component: function AISettings() {
    // The path is read here, not in render: the part calls render only once its chunk has loaded.
    const path = aiRoute.useParams()._splat ?? '';
    return <Part part={aiRoutePart} failed={<main className="settings-page"><PartUnavailable /></main>} render={({ AIRoute }) => <AIRoute path={path} />} />;
  },
});
// Settings → Channels is its own chunk; one component reads the rest of the address (channels-route.tsx).
type ChannelsSearch = { tab?: string; gmail?: string; gmail_error?: string; microsoft?: string; microsoft_error?: string };
// Passed through as it is: the pages read only string values (channels-route.tsx), and the Gmail return
// (gmail, gmail_error) and the Microsoft one (microsoft, microsoft_error) must survive the hop from type/email to the channel.
const channelsSearch = (search: Record<string, unknown>) => search as ChannelsSearch;
function ChannelsSettings({ path, search }: { path: string; search: ChannelsSearch }) {
  const navigate = useNavigate();
  const go = useCallback((href: string, replace?: boolean) => void navigate({ href, replace }), [navigate]);
  // A page opened without a hover first (a typed address, a redirect) still asks for everything at once.
  const client = useQueryClient();
  useEffect(() => { preloadChannelsPage(client, `/settings/channels/${path}`); }, [client, path]);
  return <Part part={channelsRoutePart} failed={<main className="settings-page"><PartUnavailable /></main>} render={({ ChannelsRoute }) => <ChannelsRoute path={path} search={search} navigate={go} />} />;
}
// /settings/channels itself is this route with an empty splat.
const channelRoute = createRoute({
  getParentRoute: () => rootRoute, path: '/settings/channels/$', validateSearch: channelsSearch,
  component: function Channel() { return <ChannelsSettings path={channelRoute.useParams()._splat ?? ''} search={channelRoute.useSearch()} />; },
});
const resultsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/results',
  validateSearch: (search: Record<string, unknown>): { window?: '7d' } => (search.window === '7d' ? { window: '7d' } : {}),
  component: function Results() {
    const { window } = resultsRoute.useSearch();
    const navigate = useNavigate();
    return <Part part={resultsRoutePart} failed={<main className="settings-page"><PartUnavailable /></main>} render={({ ResultsRoute }) => <ResultsRoute window={window ?? '30d'}
      onWindow={(value) => void navigate({ to: '/results', search: value === '7d' ? { window: '7d' } : {}, replace: true })} />} />;
  },
});
// Fixes and Settings → Autofix are in the Autofix chunk (features/autofix/autofix-chunk.ts), shown only where this
// browser turned the Autofix preview on; the Fixes screen reads and checks its filters in the address.
function AutofixScreen({ render }: { render: (chunk: typeof import('./features/autofix/autofix-chunk')) => ReactNode }) {
  if (!autofixPreviewOn()) return <NotFoundPage />;
  return <Part part={autofixPart} failed={<main className="settings-page"><PartUnavailable /></main>} render={render} />;
}
const fixesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/fixes',
  validateSearch: (search: Record<string, unknown>): { repo?: unknown; agent?: unknown } => search,
  component: () => <AutofixScreen render={({ FixesScreen }) => <FixesScreen />} />,
});
const autofixSettingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/settings/autofix',
  component: () => <AutofixScreen render={({ AutofixSettingsRoute }) => <AutofixSettingsRoute />} />,
});
const resultsShareRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/r',
  component: () => <Part part={resultsSharePart} failed={<main className="settings-page"><PartUnavailable /></main>} render={({ ResultsShareRoute }) => <ResultsShareRoute />} />,
});
const signInRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/sign-in',
  // `signed_out` marks the arrival right after the session ended (see AppLayout). `intent=trial` is the
  // website's "Start free trial" link: the page is titled for a new team. `next` is the page a hand-off
  // from APP_ORIGIN opens once its token (in the fragment, `#handoff=…`) signs in on this workspace host.
  validateSearch: (search: Record<string, unknown>): { token?: string; buy?: string; signed_out?: true; intent?: 'trial'; next?: string } => ({
    ...(typeof search.token === 'string' ? { token: search.token } : {}),
    ...(typeof search.buy === 'string' && search.buy !== '' ? { buy: search.buy } : {}),
    ...(search.signed_out === true ? { signed_out: true as const } : {}),
    ...(search.intent === 'trial' ? { intent: 'trial' as const } : {}),
    ...(typeof search.next === 'string' ? { next: search.next } : {}),
  }),
  // `buy` stays in the address through the identity provider's round trip, so the address decides
  // which plan, if any, this sign-in is for (see signInCheckoutIntent).
  beforeLoad: ({ search }) => { signInCheckoutIntent(search); },
  component: function SignIn() {
    const { token, buy, signed_out: signedOut, intent, next } = signInRoute.useSearch();
    const handoff = useRouterState({ select: (state) => /^handoff=(.+)$/.exec(state.location.hash)?.[1] });
    if (handoff) return <HandoffSignIn token={handoff} next={next} />;
    return <RedeemRoute token={token ?? ''} buy={buy} trial={intent === 'trial'} signedOut={signedOut === true} />;
  },
});

export const routeTree = rootRoute.addChildren([inboxRoute, conversationRoute, contactRoute, knowledgeRoute, mediaRoute, unusedMediaRoute, mediaTrashRoute, newArticleRoute, articleRoute, settingsRoute, requestsRoute, billingRoute, importRoute, aiRoute, autofixSettingsRoute, channelRoute, resultsRoute, fixesRoute, resultsShareRoute, signInRoute]);
export const router = createRouter({ routeTree });

declare module '@tanstack/react-router' {
  interface Register { router: typeof router }
}
