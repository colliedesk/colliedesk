import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { QueryClient } from '@tanstack/react-query';
import { useCallback, useEffect } from 'react';
import { Link, Navigate, Outlet, createRootRouteWithContext, redirect, rootRouteId, createRoute, createRouter, useRouter, useRouterState, useNavigate } from '@tanstack/react-router';
import { api, UnauthorizedError } from './api/client';
import { ContactsPart, contactPart } from './features/contacts/contact-part';
import { InboxRoute } from './features/inbox/inbox-route';
import { useInboxActivity } from './features/inbox/use-inbox-activity';
import { can } from './lib/can';
import { closedScreen, firstOpenArea, knowledgeHome, railAreasFor } from './lib/areas';
import { isPhone } from './lib/use-phone';
import { keepContentScroll } from './lib/content-scroll';
import { PermissionsContext } from './lib/permissions-context';
import type { TokenPermission } from './api/schemas';
import { useIntakeSummary } from './features/review/use-intake-summary';
import { RedeemRoute } from './features/auth/redeem-route';
import { HandoffSignIn } from './features/auth/handoff-sign-in';
import { OAuthConsent, oauthRequestFrom } from './features/auth/oauth-consent';
import { clearCheckoutIntent, readCheckoutIntent, rememberCheckoutIntent, signInCheckoutIntent } from './features/settings/checkout-intent';
import { parsePlanIntent, signInPlanIntent } from './features/settings/plan-intent';
import { rememberSignupAttribution } from './lib/signup-attribution';
import type { PlanIntent } from './features/settings/plan-intent';
import { SettingsNavLinkContext } from './features/settings/settings-nav';
import { settingsAddress } from './features/settings/settings-anchors';
import type { NavItem } from './features/settings/settings-nav';
import type { ReactNode } from 'react';
import { AppShell } from './features/shell/app-shell';
import { askInbox } from './features/shell/inbox-intents';
import type { InboxIntent } from './features/shell/inbox-intents';
import { useSidebarData } from './features/shell/use-sidebar-data';
import { paletteEntries } from './features/shell/palette-entries';
import { preloadQuickStart, QuickStart } from './features/onboarding/quick-start';
import { SeatPausedScreen } from './features/shell/seat-paused-screen';
import { PlanNotices } from './features/shell/plan-notices';
import { messages } from './messages';
import { AtIcon, BookIcon, ChartIcon, ChatIcon, InboxIcon, SettingsIcon } from './ui/icons';
import { mentionMessages } from './features/inbox/mention-messages';
import { autofixPart } from './features/autofix/autofix-part';
import { autofixPreviewOn } from './features/autofix/autofix-preview';
import { kbPart } from './features/kb/kb-part';
import { channelsRoutePart, preloadChannelsPage } from './features/settings/channels/channels-preload';
import { preloadSetupStep, setupFlowPart } from './features/onboarding/setup-flow-part';
import { lazyPart, Part, PartUnavailable } from './ui/lazy-part';
import type { LazyPart } from './ui/lazy-part';
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
// Settings → Data carries the Freshdesk import's steps, so it is a chunk of its own.
const dataRoutePart = lazyPart(() => import('./features/settings/data-route'));
const rolesRoutePart = lazyPart(() => import('./features/settings/roles/roles-route'));
const targetsRoutePart = lazyPart(() => import('./features/settings/targets/targets-route'));
const aiRoutePart = lazyPart(() => import('./features/settings/ai-route'));
// Results is its own chunk too: the rail link starts loading it on hover and focus.
const resultsRoutePart = lazyPart(() => import('./features/results/results-route'));
const resultsSharePart = lazyPart(() => import('./features/results/results-share-route'));
// Review (the intake queue of chat channels) is its own chunk; only workspaces with a chat channel open it.
const reviewRoutePart = lazyPart(() => import('./features/review/review-route'));
const lockedRoutePart = lazyPart(() => import('./features/shell/locked-route'));
const trialSurveyPart = lazyPart(() => import('./features/shell/trial-survey'));
// A trial offer link is opened once by a few admins: its page loads from its own chunk.
const trialOfferPart = lazyPart(() => import('./features/auth/trial-offer-page'));

// pageChunk is the loader of a route whose page is a lazy part. Moving inside the app, it waits for the page's
// chunk, so the router keeps the current page on screen, Settings nav included, until the next one can show:
// the screen never goes blank as a reload would (routes-never-blank.test.tsx holds every route to it). On the
// first load there is no page to keep: it only starts the chunk, and the rail and the page's frame show at once.
let pageOnScreen = false;
const pageChunk = (part: LazyPart<unknown>) => () => (pageOnScreen ? part.ready() : part.preload());

function AppLayout() {
  const path = useRouterState({ select: (state) => state.location.pathname });
  const search = useRouterState({ select: (state) => state.location.search });
  const navigate = useNavigate();
  useEffect(() => { pageOnScreen = true; }, []);
  // A bare link to a page of the app opens without reloading, wherever it is written.
  useInAppLinks();
  // A page opened starts at its top, and Back returns to where it was, in the content pane too (content-scroll.ts).
  const router = useRouter();
  useEffect(() => keepContentScroll(router), [router]);
  // The public page of a results share link has no session and no rail.
  // The consent page of an AI app's OAuth sign-in has no rail either: it checks its own session. So does the
  // page of a trial offer link, which a locked workspace must reach past its locked screen.
  const onSignIn = path === '/sign-in' || path === '/r' || path === '/oauth/authorize' || path === '/trial-offer';
  // The setup flow fills the screen with one step at a time: no rail, its own header names the workspace. Only
  // its own addresses do: a deeper one is not found, and the router matches it to a step page all the same.
  const inSetup = /^\/setup(\/[^/]+)?\/?$/.test(path);
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
  useQuery({ queryKey: ['billing'], queryFn: api.billing, enabled: locked && !paused && can(me.data, 'billing:read') });
  // New, newly assigned and changed conversations are picked up on every signed-in page, not only in the inbox.
  const readsInbox = can(me.data, 'conversations:read');
  useInboxActivity(!onSignIn && readsInbox && !signedOut && !locked, me.data?.agent?.id);
  // The server's config enables it unless telemetry is off; otherwise the library is never loaded.
  // On a sign-in page the analytics library waits for the sign-in form, so a slow connection loads the form first.
  useProductAnalytics(config.data?.product_analytics, me.data, onSignIn ? signInFormReady : undefined);
  // Fixes is in the Autofix chunk, loaded only where the server runs Autofix and this browser turned its preview on.
  const fixes = autofixPreviewOn() && config.data?.autofix?.available === true && Boolean(me.data) && !signedOut && !locked;
  // Cards of chat channels waiting in Review; the link shows only where a chat channel collects messages.
  const intake = useIntakeSummary(!onSignIn && readsInbox && !signedOut && !locked).data;
  // The inbox list on screen (its filters are in the address), so the sidebar marks the view or channel it is.
  const inInbox = path === '/' || path.startsWith('/conversations/');
  const inMentions = path === '/mentions' || path.startsWith('/mentions/');
  const sidebar = useSidebarData(me.data, !onSignIn && !signedOut && !locked, inInbox ? inboxQueryFromSearch(search) : undefined);
  // Quick start in the sidebar head reads where setup stands on every page. A guide the session hid is not asked for;
  // until the session answers it is assumed wanted, so the steps go out with the session, not after it.
  const readsSetup = !onSignIn && !signedOut && !locked && me.data?.setup_guide_hidden !== true;
  const setup = useQuery({ queryKey: ['setup'], queryFn: api.setupProgress, enabled: readsSetup });
  useEffect(() => { if (readsSetup) preloadQuickStart(); }, [readsSetup]);
  // New conversation (the sidebar's pen and the palette's action) opens the inbox where it is not on screen and asks it.
  const askTheInbox = (intent: InboxIntent) => {
    if (!inInbox) void navigate({ to: '/' });
    askInbox(intent);
  };
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
  // Signed out or locked, the setup flow gets the same screens as any page, in the frame of every page.
  if (inSetup && !signedOut && !locked && !notFound) return <Outlet />;
  // Plan and billing is a page inside Settings, so Settings stays the current rail destination there.
  const inSettings = path.startsWith('/settings');
  const inReview = path.startsWith('/review');
  const inKnowledge = path.startsWith('/knowledge');
  const inResults = path.startsWith('/results');
  const inFixes = path.startsWith('/fixes');
  // A screen the session's roles may not read says so; the inbox address takes it to the first area it may open.
  const closed = me.data ? closedScreen(path, me.data) : undefined;
  if (closed && path === '/') return <Navigate to={firstOpenArea(me.data)} replace />;
  const shows = (permission: TokenPermission) => !me.data || can(me.data, permission);
  // Knowledge opens on what the session reads of it: the articles, the media or the help center's settings.
  const knowledge = me.data ? knowledgeHome(me.data) : '/knowledge';
  const inboxOpen = sidebar.inboxOpen ?? 0;
  const mentionsUnread = sidebar.mentionsUnread ?? 0;
  // The page the top bar names: the area the sidebar marks current.
  const page = inSettings ? { title: messages.settings, icon: <SettingsIcon /> } : inReview ? { title: messages.review, icon: <ChatIcon /> }
    : inKnowledge ? { title: messages.knowledge, icon: <BookIcon /> } : inResults ? { title: messages.results, icon: <ChartIcon /> }
    // Fixes keeps its wrench in the Autofix chunk; its name is enough here.
    : inFixes ? { title: messages.autofix, icon: null } : inMentions ? { title: mentionMessages.mentions, icon: <AtIcon /> } : inInbox && !notFound ? { title: messages.inbox, icon: <InboxIcon /> } : undefined;
  // Starting one picks a contact and the channel it leaves through, so it needs those reads as well.
  const mayStart = (['messages:send', 'channels:read', 'contacts:read'] as const).every(shows);
  const onNewConversation = locked || !mayStart || !shows('conversations:read') ? undefined : () => askTheInbox('new-conversation');
  // The top bar's search: the same places and actions as the sidebar, and the inbox's own searches.
  const palette = me.data && !locked ? { ...paletteEntries(me.data, { inboxOpen, mentionsUnread, onNewConversation }), onNavigate: (href: string) => router.history.push(href) } : undefined;
  return <SettingsNavLinkContext.Provider value={settingsRouterLink}><AppShell me={me.data} appVersion={config.data?.version} signedOut={signedOut}
    page={page} views={locked ? undefined : sidebar.views} channels={locked ? undefined : sidebar.channels}
    palette={palette} onNewConversation={onNewConversation}
    quickStart={me.data && !locked ? <QuickStart key={String(me.data.setup_guide_hidden)} progress={setup.data} me={me.data} /> : undefined}
    canInvite={!locked && can(me.data, 'team:invite')}
    notice={me.data && <><PlanNotices me={me.data} onBillingPage={inBilling} />
      {/* The survey only ever asks a cloud trial admin, so no other session loads its chunk. */}
      {config.data?.product_analytics.realm === 'cloud' && can(me.data, 'billing:manage') && me.data.plan?.plan === 'trial'
        && <Part part={trialSurveyPart} render={({ TrialSurvey }) => <TrialSurvey me={me.data} analytics={config.data?.product_analytics} />} />}</>} lockedScreen={locked && me.data ? (paused && me.data.plan ? <SeatPausedScreen plan={messages.plans[me.data.plan.plan]} isAdmin={can(me.data, 'billing:manage')} onSignOut={() => signOut.mutate()} /> : <Part part={lockedRoutePart} failed={<main className="settings-page"><PartUnavailable /></main>} render={({ LockedRoute }) => <LockedRoute me={me.data} />} />) : undefined}
    onSignOut={() => signOut.mutate()} signOutFailed={signOut.isError} signingOut={signOut.isPending}
    brand={<Link to="/" className="rail-brand" aria-label={messages.brand}><LogoMark /></Link>}
    links={locked ? undefined : <>
      {shows('conversations:read') && <Link to="/" className="rail-link" data-tip={messages.inbox} aria-label={inboxOpen > 0 ? messages.inboxOpenCount(inboxOpen) : messages.inbox}
        aria-current={inSettings || inReview || inKnowledge || inResults || inFixes || inMentions || notFound ? undefined : 'page'}><InboxIcon /><span className="rail-label" aria-hidden="true">{messages.inbox}</span>
        {inboxOpen > 0 && <span className="rail-badge" aria-hidden="true">{inboxOpen > 999 ? '999+' : inboxOpen}</span>}</Link>}
      {shows('messages:read') && <Link to="/mentions" className="rail-link" data-tip={mentionMessages.mentions}
        aria-label={mentionsUnread > 0 ? mentionMessages.mentionsUnread(mentionsUnread) : mentionMessages.mentions} aria-current={inMentions ? 'page' : undefined}>
        <AtIcon /><span className="rail-label" aria-hidden="true">{mentionMessages.mentions}</span>
        {mentionsUnread > 0 && <span className="rail-badge" aria-hidden="true">{mentionsUnread > 99 ? '99+' : mentionsUnread}</span>}</Link>}
      {fixes && <Part part={autofixPart} render={({ FixesRailLink }) => <FixesRailLink current={inFixes} />} />}
      {intake?.available && <Link to="/review" className="rail-link" data-tip={messages.review} aria-label={intake.waiting > 0 ? messages.reviewWaiting(intake.waiting) : messages.review}
        aria-current={inReview ? 'page' : undefined} onPointerEnter={reviewRoutePart.preload} onFocus={reviewRoutePart.preload}><ChatIcon /><span className="rail-label" aria-hidden="true">{messages.review}</span>
        {intake.waiting > 0 && <span className="rail-badge" aria-hidden="true">{intake.waiting > 99 ? '99+' : intake.waiting}</span>}</Link>}
      {knowledge && <Link to={knowledge} className="rail-link" data-tip={messages.knowledge} aria-label={messages.knowledge} aria-current={inKnowledge ? 'page' : undefined}
        onPointerEnter={kbPart.preload} onFocus={kbPart.preload}><BookIcon /><span className="rail-label" aria-hidden="true">{messages.knowledge}</span></Link>}
      {shows('results:read') && <Link to="/results" className="rail-link" data-tip={messages.results} aria-label={messages.results} aria-current={inResults ? 'page' : undefined}
        onPointerEnter={resultsRoutePart.preload} onFocus={resultsRoutePart.preload}><ChartIcon /><span className="rail-label" aria-hidden="true">{messages.results}</span></Link>}
    </>}
    // Settings is not in the design's list of places; it sits at the foot, beside the person and Invite teammates.
    footLinks={locked ? undefined : <Link to="/settings" className="rail-link" data-tip={messages.settings} aria-label={messages.settings} aria-current={inSettings ? 'page' : undefined}
      onPointerEnter={settingsRoutePart.preload} onFocus={settingsRoutePart.preload}><SettingsIcon /><span className="rail-label" aria-hidden="true">{messages.settings}</span></Link>}>
    {closed && me.data ? <ClosedScreen label={closed.label} permission={closed.permission} me={me.data} />
      : <PermissionsContext.Provider value={me.data?.permissions}><Outlet /></PermissionsContext.Provider>}
  </AppShell></SettingsNavLinkContext.Provider>;
}

// Settings sections link through the router, so moving between Settings and Plan and billing needs no reload.
// exact + includeHash: otherwise every section link would count as current on any settings page.
function settingsRouterLink(item: NavItem) {
  // /settings/channels, the type homes under it and /settings/ai are routes with a splat, paths the route types do not list.
  return <Link to={item.to as Exclude<NavItem['to'], `/settings/channels${string}` | '/settings/ai'>} hash={item.hash} activeOptions={{ exact: true, includeHash: true }}
    aria-current={item.current ? 'page' : undefined}>{item.label}</Link>;
}

// An unknown address inside the app says so and offers the way back, in the frame of every other page.
function NotFoundPage() {
  return <main className="settings-page">
    <header><h1>{messages.pageNotFound}</h1><p>{messages.pageNotFoundExplanation}</p></header>
    <Link to="/" className="link-action page-back">{messages.backToInbox}</Link>
  </main>;
}

// A route's loader can start the requests its page reads together with its chunk: main.tsx hands the router the
// app's query client. Without one (a test's own router) the page asks once it renders.
type RouterContext = { queryClient?: QueryClient };
const rootRoute = createRootRouteWithContext<RouterContext>()({ component: AppLayout, notFoundComponent: NotFoundPage });
// The inbox filters are in the address, so a link opens the same list (src/lib/inbox-query.ts).
const inboxRoute = createRoute({ getParentRoute: () => rootRoute, path: '/', validateSearch: inboxQueryFromSearch, component: InboxRoute });
const conversationRoute = createRoute({ getParentRoute: () => rootRoute, path: '/conversations/$conversationId', validateSearch: inboxQueryFromSearch, component: InboxRoute });
// Mentions: the notes that asked the person, in the inbox's list column; a row opens its conversation beside it.
const mentionsRoute = createRoute({ getParentRoute: () => rootRoute, path: '/mentions', component: () => <InboxRoute list="mentions" /> });
const mentionRoute = createRoute({ getParentRoute: () => rootRoute, path: '/mentions/$conversationId', component: () => <InboxRoute list="mentions" /> });
// The customer page is its own chunk (contact-part.tsx), so the inbox bundle does not carry it.
const contactRoute = createRoute({ getParentRoute: () => rootRoute, path: '/contacts/$contactId', loader: pageChunk(contactPart),
  component: () => <ContactsPart render={(chunk) => <chunk.ContactRoute />} failed={<main className="settings-page"><PartUnavailable /></main>} /> });
// The knowledge base is its own chunk (features/kb/kb-chunk.ts); its filters are in the address.
const kbFailed = <main className="settings-page"><PartUnavailable /></main>;
const knowledgeRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/knowledge',
  loader: pageChunk(kbPart),
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
  loader: pageChunk(kbPart),
  validateSearch: mediaSearch,
  component: function Media() {
    const search = mediaRoute.useSearch();
    return <Part part={kbPart} failed={kbFailed} render={({ MediaRoute }) => <MediaRoute view="library" search={search} />} />;
  },
});
const unusedMediaRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/knowledge/media/unused',
  loader: pageChunk(kbPart),
  component: () => <Part part={kbPart} failed={kbFailed} render={({ MediaRoute }) => <MediaRoute view="unused" search={{}} />} />,
});
const mediaTrashRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/knowledge/media/trash',
  loader: pageChunk(kbPart),
  component: () => <Part part={kbPart} failed={kbFailed} render={({ MediaRoute }) => <MediaRoute view="trash" search={{}} />} />,
});
const newArticleRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/knowledge/new',
  loader: pageChunk(kbPart),
  validateSearch: (search: Record<string, unknown>): { collection?: string } => (typeof search.collection === 'string' && search.collection !== '' ? { collection: search.collection } : {}),
  component: function NewArticle() {
    const { collection } = newArticleRoute.useSearch();
    return <Part part={kbPart} failed={kbFailed} render={({ ArticleRoute }) => <ArticleRoute collectionId={collection} />} />;
  },
});
const articleRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/knowledge/articles/$articleId',
  loader: pageChunk(kbPart),
  component: function Article() {
    const { articleId } = articleRoute.useParams();
    // key: another article starts with its own form, not the last one's edits.
    return <Part part={kbPart} failed={kbFailed} render={({ ArticleRoute }) => <ArticleRoute key={articleId} articleId={articleId} />} />;
  },
});
// The help center's languages, and the articles missing or outdated in one of them (in the address).
const languagesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/knowledge/languages',
  loader: pageChunk(kbPart),
  validateSearch: (search: Record<string, unknown>): { locale?: string; state?: 'missing' | 'outdated' | 'translated' } => ({
    ...(typeof search.locale === 'string' && /^[A-Za-z]{2,3}(-[A-Za-z0-9]{1,8})*$/.test(search.locale) ? { locale: search.locale } : {}),
    ...(search.state === 'missing' || search.state === 'outdated' || search.state === 'translated' ? { state: search.state } : {}),
  }),
  component: function Languages() {
    const search = languagesRoute.useSearch();
    return <Part part={kbPart} failed={kbFailed} render={({ LanguagesRoute }) => <LanguagesRoute search={search} />} />;
  },
});
// One language of an article, beside the original; it also starts a translation.
// The help center's own settings, beside the articles it publishes; they were a section of Settings.
const helpCenterSettingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/knowledge/settings',
  loader: pageChunk(kbPart),
  component: () => <Part part={kbPart} failed={kbFailed} render={({ HelpCenterSettingsRoute }) => <HelpCenterSettingsRoute />} />,
});
const translationRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/knowledge/articles/$articleId/translations/$locale',
  loader: pageChunk(kbPart),
  component: function Translation() {
    const { articleId, locale } = translationRoute.useParams();
    return <Part part={kbPart} failed={kbFailed} render={({ TranslationRoute }) => <TranslationRoute key={`${articleId}-${locale}`} articleId={articleId} locale={locale} />} />;
  },
});
// Settings is one page per item of its menu (#528), each at its own address under /settings/. While the
// chunk loads a page keeps its frame and title, so a slow network shows the page at once and nothing jumps
// when the sections arrive. A chunk that cannot load says so, with Reload page: a browser keeps a failed
// import failed until the page loads again.
type SettingsChunk = typeof import('./features/settings/settings-route');
function SettingsChunkPage({ title, render }: { title: string; render: (chunk: SettingsChunk) => ReactNode }) {
  const part = settingsRoutePart.use();
  if (part.status === 'ready') return render(part.module);
  // data-chunk-loading: the routes' loaders wait for the chunk, so the router never shows this while it loads.
  return <main className="settings-page" aria-busy={part.status === 'loading'} data-chunk-loading={part.status === 'loading' || undefined}>
    <header><h1>{title}</h1></header>
    {part.status === 'error' ? <PartUnavailable /> : <p role="status" className="muted-copy">{messages.loadingList}</p>}
  </main>;
}
// /settings is the menu on a phone and Account elsewhere. A link to a section of the old one-page Settings
// (/settings#invites-heading, in bookmarks, emails and MCP answers) opens the page that holds it now
// (settings-anchors.ts), and so does a refused export link (/settings?export_error=…).
const settingsRoute = createRoute({ getParentRoute: () => rootRoute, path: '/settings',
  loader: pageChunk(settingsRoutePart),
  beforeLoad: ({ location }) => {
    const address = settingsAddress(location.hash, location.searchStr);
    // On a phone the menu and a settings page take turns on the screen, as the inbox list and a conversation do.
    if (address !== '/settings/account' || !isPhone()) throw redirect({ href: address, replace: true });
  },
  component: () => <SettingsChunkPage title={messages.settings} render={({ SettingsIndexRoute }) => <SettingsIndexRoute />} /> });
const accountRoute = createRoute({ getParentRoute: () => rootRoute, path: '/settings/account', loader: pageChunk(settingsRoutePart),
  component: () => <SettingsChunkPage title={messages.account} render={({ AccountRoute }) => <AccountRoute />} /> });
const generalRoute = createRoute({ getParentRoute: () => rootRoute, path: '/settings/general', loader: pageChunk(settingsRoutePart),
  component: () => <SettingsChunkPage title={messages.general} render={({ GeneralRoute }) => <GeneralRoute />} /> });
const membersRoute = createRoute({ getParentRoute: () => rootRoute, path: '/settings/members', loader: pageChunk(settingsRoutePart),
  component: () => <SettingsChunkPage title={messages.members} render={({ MembersRoute }) => <MembersRoute />} /> });
const savedRepliesRoute = createRoute({ getParentRoute: () => rootRoute, path: '/settings/saved-replies', loader: pageChunk(settingsRoutePart),
  component: () => <SettingsChunkPage title={messages.savedReplies} render={({ SavedRepliesRoute }) => <SavedRepliesRoute />} /> });
// Agent requests were removed: an agent token acts at once when it holds the permission. An old bookmark opens the inbox.
const requestsRoute = createRoute({ getParentRoute: () => rootRoute, path: '/requests', component: () => <Navigate to="/" replace /> });
// The tab and the channel filter of Review are in the address, so a link opens the same list.
type ReviewSearch = { tab?: 'dismissed' | 'decided'; channel?: string; scope?: string };
const reviewRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/review',
  loader: pageChunk(reviewRoutePart),
  validateSearch: (search: Record<string, unknown>): ReviewSearch => ({
    ...(search.tab === 'dismissed' || search.tab === 'decided' ? { tab: search.tab } : {}),
    // A channel alone filters by the channel; its scope narrows it further.
    ...(typeof search.channel === 'string' && search.channel !== '' ? { channel: search.channel } : {}),
    ...(typeof search.channel === 'string' && typeof search.scope === 'string' && search.scope !== '' ? { scope: search.scope } : {}),
  }),
  component: function Review() {
    const { tab, channel, scope } = reviewRoute.useSearch();
    const navigate = useNavigate();
    return <Part part={reviewRoutePart} failed={<main className="settings-page"><PartUnavailable /></main>} render={({ ReviewRoute }) => <ReviewRoute tab={tab ?? 'waiting'}
      filter={channel ? `${channel}:${scope ?? ''}` : ''}
      onChange={(next, filter) => {
        const [nextChannel, nextScope] = filter.split(':');
        void navigate({ to: '/review', search: { ...(next === 'waiting' ? {} : { tab: next }), ...(filter ? { channel: nextChannel, ...(nextScope ? { scope: nextScope } : {}) } : {}) }, replace: true });
      }} />} />;
  },
});
// billingReadable says whether the billing page asks for the plan: the session may read it and billing is on or switched off.
type Me = Awaited<ReturnType<typeof api.me>>;
type PublicConfig = Awaited<ReturnType<typeof api.config>>;
function billingReadable(me: Me | undefined, config: PublicConfig | undefined) {
  return can(me, 'billing:read') && (config?.billing.enabled === true || config?.billing.switched_off === true);
}
const billingRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/settings/billing',
  loader: ({ context: { queryClient } }) => {
    if (queryClient && billingReadable(queryClient.getQueryData<Me>(['me']), queryClient.getQueryData<PublicConfig>(['public-config']))) void queryClient.prefetchQuery({ queryKey: ['billing'], queryFn: api.billing });
    return pageChunk(billingRoutePart)();
  },
  // `buy=<plan>` is the link the website uses to send a visitor straight into that plan's checkout.
  validateSearch: (search: Record<string, unknown>): { checkout: 'success' | ''; buy?: string } => ({
    checkout: search.checkout === 'success' ? 'success' : '',
    ...(typeof search.buy === 'string' && search.buy !== '' ? { buy: search.buy } : {}),
  }),
  // Runs before sign-in is known, so the plan is kept through the sign-in and sign-up round trip.
  // `buy` is then removed from the address: Back or a reload after the checkout must not start another.
  // The ad keys of the link are kept too: the redirect to sign-up carries only the plan.
  // A link loading the page ahead (preload) is not a visit: nothing is remembered and nothing redirects.
  beforeLoad: ({ search, location, preload }) => {
    if (preload || !search.buy) return;
    rememberCheckoutIntent(search.buy);
    rememberSignupAttribution(location.searchStr);
    throw redirect({ to: '/settings/billing', search: { checkout: search.checkout }, replace: true });
  },
  component: function Billing() {
    const checkoutReturned = billingRoute.useSearch().checkout === 'success';
    // Billing is asked for while the page's chunk loads, not after it (billing-route.tsx asks the same).
    const me = useQuery({ queryKey: ['me'], queryFn: api.me });
    const config = useQuery({ queryKey: ['public-config'], queryFn: api.config });
    useQuery({ queryKey: ['billing'], queryFn: api.billing, enabled: billingReadable(me.data, config.data) });
    return <Part part={billingRoutePart} failed={<main className="settings-page"><PartUnavailable /></main>} render={({ BillingRoute }) => <BillingRoute checkoutReturned={checkoutReturned} />} />;
  },
});
// Settings → Data: the Freshdesk import and the whole-workspace export. The import was a page of its own:
// its address (in import emails and older guides) opens Data.
const dataRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/settings/data',
  loader: pageChunk(dataRoutePart),
  component: () => <Part part={dataRoutePart} failed={<main className="settings-page"><PartUnavailable /></main>} render={({ DataRoute }) => <DataRoute />} />,
});
const importRoute = createRoute({ getParentRoute: () => rootRoute, path: '/settings/import-freshdesk',
  beforeLoad: () => { throw redirect({ to: '/settings/data', replace: true }); } });
// Settings → Members → a role: a new role (optionally from another) and one role. The list of roles is a
// section of Members; its old address opens it there.
const rolesFailed = <main className="settings-page"><PartUnavailable /></main>;
const rolesRoute = createRoute({ getParentRoute: () => rootRoute, path: '/settings/roles',
  beforeLoad: () => { throw redirect({ to: '/settings/members', hash: 'roles-heading', replace: true }); } });
const newRoleRoute = createRoute({ getParentRoute: () => rootRoute, path: '/settings/roles/new', loader: pageChunk(rolesRoutePart),
  validateSearch: (search: Record<string, unknown>): { from?: string } => (typeof search.from === 'string' ? { from: search.from } : {}),
  component: function NewRole() {
    const { from } = newRoleRoute.useSearch();
    return <Part part={rolesRoutePart} failed={rolesFailed} render={({ RolesRoute }) => <RolesRoute page={{ kind: 'new', from }} />} />;
  } });
const roleRoute = createRoute({ getParentRoute: () => rootRoute, path: '/settings/roles/$roleId', loader: pageChunk(rolesRoutePart),
  component: function OneRole() {
    const { roleId } = roleRoute.useParams();
    return <Part part={rolesRoutePart} failed={rolesFailed} render={({ RolesRoute }) => <RolesRoute page={{ kind: 'edit', id: roleId }} />} />;
  } });
// Conversation numbers were a page of their own; they are a section of General now.
const numbersRoute = createRoute({ getParentRoute: () => rootRoute, path: '/settings/numbers',
  beforeLoad: () => { throw redirect({ to: '/settings/general', hash: 'numbers-heading', replace: true }); } });
// Settings → Response targets: business hours and the targets of each priority.
const targetsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/settings/targets',
  loader: pageChunk(targetsRoutePart),
  component: function Targets() {
    return <Part part={targetsRoutePart} failed={<main className="settings-page"><PartUnavailable /></main>} render={({ TargetsRoute }) => <TargetsRoute />} />;
  },
});
// Settings → Built-in AI, with a chat's transcript below it (/settings/ai/chats/<id>): the chunk reads its path.
const aiRoute = createRoute({
  getParentRoute: () => rootRoute, path: '/settings/ai/$', loader: pageChunk(aiRoutePart),
  component: function AISettings() {
    // The path is read here, not in render: the part calls render only once its chunk has loaded.
    const path = aiRoute.useParams()._splat ?? '';
    return <Part part={aiRoutePart} failed={<main className="settings-page"><PartUnavailable /></main>} render={({ AIRoute }) => <AIRoute path={path} />} />;
  },
});
// Settings → Channels is its own chunk; one component reads the rest of the address (channels-route.tsx).
type ChannelsSearch = { tab?: string; gmail?: string; gmail_error?: string; microsoft?: string; microsoft_error?: string; step?: string };
// Passed through as it is: the pages read only string values (channels-route.tsx), and the Gmail return
// (gmail, gmail_error) and the Microsoft one (microsoft, microsoft_error) must survive the hop from type/email to the channel.
const channelsSearch = (search: Record<string, unknown>) => search as ChannelsSearch;
function ChannelsSettings({ path, search }: { path: string; search: ChannelsSearch }) {
  const navigate = useNavigate();
  const go = useCallback((href: string, replace?: boolean) => void navigate({ href, replace }), [navigate]);
  return <Part part={channelsRoutePart} failed={<main className="settings-page"><PartUnavailable /></main>} render={({ ChannelsRoute }) => <ChannelsRoute path={path} search={search} navigate={go} />} />;
}
// /settings/channels itself is this route with an empty splat.
const channelRoute = createRoute({
  getParentRoute: () => rootRoute, path: '/settings/channels/$', validateSearch: channelsSearch,
  // The loader also asks for what the page reads, so a hover, a click or a typed address asks for everything at once.
  loader: ({ context: { queryClient }, location }) => { if (queryClient) preloadChannelsPage(queryClient, location.pathname); return pageChunk(channelsRoutePart)(); },
  component: function Channel() { return <ChannelsSettings path={channelRoute.useParams()._splat ?? ''} search={channelRoute.useSearch()} />; },
});
// The setup flow is its own chunk (setup-flow-part.ts); one component reads the step in the address.
function SetupFlow({ step }: { step?: string }) {
  const navigate = useNavigate();
  const go = useCallback((href: string, replace?: boolean) => void navigate({ href, replace }), [navigate]);
  return <Part part={setupFlowPart} failed={<main className="settings-page"><PartUnavailable /></main>} render={({ SetupFlowRoute }) => <SetupFlowRoute step={step} navigate={go} />} />;
}
// The loader asks for what a step reads with its chunk, so a hover, a click, a typed address or the sign-up asks for everything at once.
function setupLoader(queryClient: QueryClient | undefined, step?: string) {
  if (queryClient) preloadSetupStep(queryClient, step);
  return pageChunk(setupFlowPart)();
}
const setupRoute = createRoute({ getParentRoute: () => rootRoute, path: '/setup', loader: ({ context: { queryClient } }) => setupLoader(queryClient), component: () => <SetupFlow /> });
const setupStepRoute = createRoute({
  getParentRoute: () => rootRoute, path: '/setup/$step', loader: ({ context: { queryClient }, params }) => setupLoader(queryClient, params.step),
  component: function SetupStep() { return <SetupFlow step={setupStepRoute.useParams().step} />; },
});
const resultsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/results',
  loader: pageChunk(resultsRoutePart),
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
  loader: () => (autofixPreviewOn() ? pageChunk(autofixPart)() : undefined),
  validateSearch: (search: Record<string, unknown>): { repo?: unknown; agent?: unknown } => search,
  component: () => <AutofixScreen render={({ FixesScreen }) => <FixesScreen />} />,
});
const autofixSettingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/settings/autofix',
  loader: () => (autofixPreviewOn() ? pageChunk(autofixPart)() : undefined),
  component: () => <AutofixScreen render={({ AutofixSettingsRoute }) => <AutofixSettingsRoute />} />,
});
const resultsShareRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/r',
  loader: pageChunk(resultsSharePart),
  component: () => <Part part={resultsSharePart} failed={<main className="settings-page"><PartUnavailable /></main>} render={({ ResultsShareRoute }) => <ResultsShareRoute />} />,
});
const signInRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/sign-in',
  // `signed_out` marks the arrival right after the session ended (see AppLayout). `intent=trial` is the
  // website's "Start free trial" link: the page is titled for a new team. `next` is the page a hand-off
  // from APP_ORIGIN opens once its token (in the fragment, `#handoff=…`) signs in on this workspace host.
  // Without a hand-off, `next` is an AI app's consent page (/oauth/authorize?…) the sign-in returns to.
  // `plan=team25` is the plan the website's trial link came for: the trial starts as usual and the plan
  // picker puts that plan first. Any other plan is dropped here.
  validateSearch: (search: Record<string, unknown>): { token?: string; buy?: string; plan?: PlanIntent; signed_out?: true; intent?: 'trial'; next?: string } => ({
    ...(typeof search.token === 'string' ? { token: search.token } : {}),
    ...(typeof search.buy === 'string' && search.buy !== '' ? { buy: search.buy } : {}),
    ...(parsePlanIntent(search.plan) ? { plan: 'team25' as const } : {}),
    ...(search.signed_out === true ? { signed_out: true as const } : {}),
    ...(search.intent === 'trial' ? { intent: 'trial' as const } : {}),
    ...(typeof search.next === 'string' ? { next: search.next } : {}),
  }),
  // `buy` and `plan` stay in the address through the identity provider's round trip, so the address decides
  // which plan, if any, this sign-in is for (see signInCheckoutIntent, signInPlanIntent). The utm_* keys and
  // ad click IDs of the website's links are kept for the sign-up from the raw address: the parsed search
  // would turn `utm_content=07` into a number (see rememberSignupAttribution).
  beforeLoad: ({ search, location, preload }) => { if (preload) return; signInCheckoutIntent(search); signInPlanIntent(search); rememberSignupAttribution(location.searchStr); },
  component: function SignIn() {
    const { token, buy, signed_out: signedOut, intent, next } = signInRoute.useSearch();
    const handoff = useRouterState({ select: (state) => /^handoff=(.+)$/.exec(state.location.hash)?.[1] });
    if (handoff) return <HandoffSignIn token={handoff} next={next} />;
    return <RedeemRoute token={token ?? ''} buy={buy} trial={intent === 'trial'} signedOut={signedOut === true} next={next} />;
  },
});

// /oauth/authorize is where an AI app (Claude, ChatGPT, Codex, Cursor) asks to connect with OAuth. Its
// parameters are read raw from the address: they are the app's, passed back to the server as they came.
const oauthAuthorizeRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/oauth/authorize',
  component: function OAuthAuthorize() {
    const { search, pathname } = window.location;
    return <OAuthConsent request={oauthRequestFrom(new URLSearchParams(search))} here={pathname + search} />;
  },
});

// /trial-offer?token=… is the link of a trial offer an operator emailed (collie trial offer).
const trialOfferRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/trial-offer',
  loader: pageChunk(trialOfferPart),
  component: function TrialOfferLink() {
    const { search, pathname } = window.location;
    return <Part part={trialOfferPart} failed={<main className="settings-page"><PartUnavailable /></main>}
      render={({ TrialOfferPage }) => <TrialOfferPage token={new URLSearchParams(search).get('token') ?? ''} here={pathname + search} />} />;
  },
});

export const routeTree = rootRoute.addChildren([oauthAuthorizeRoute, trialOfferRoute, inboxRoute, conversationRoute, mentionsRoute, mentionRoute, contactRoute, knowledgeRoute, mediaRoute, unusedMediaRoute, mediaTrashRoute, newArticleRoute, articleRoute, languagesRoute, translationRoute, helpCenterSettingsRoute, settingsRoute, accountRoute, generalRoute, membersRoute, savedRepliesRoute, dataRoute, requestsRoute, reviewRoute, billingRoute, importRoute, rolesRoute, newRoleRoute, roleRoute, numbersRoute, targetsRoute, aiRoute, autofixSettingsRoute, channelRoute, resultsRoute, fixesRoute, resultsShareRoute, signInRoute, setupRoute, setupStepRoute]);
// Every router Link loads its page ahead when the pointer or the focus reaches it (plain links: lib/in-app-links.ts).
export const router = createRouter({ routeTree, context: {}, defaultPreload: 'intent' });

declare module '@tanstack/react-router' {
  interface Register { router: typeof router }
}

// ClosedScreen stands for a page the session's roles may not read: what it needs, who to ask, and where it can go.
function ClosedScreen({ label, permission, me }: { label: string; permission: TokenPermission; me: NonNullable<Parameters<typeof can>[0]> }) {
  const open = railAreasFor(me);
  return <main className="settings-page closed-screen">
    <header><h1>{label}</h1><p role="status">{messages.closedScreen(label)} {messages.closedScreenAsk(permission)}</p></header>
    <p className="muted-copy">{messages.closedScreenOpen}</p>
    <span className="button-row">{open.map((area) => <Link key={area.to} to={area.to} className="button">{area.label}</Link>)}</span>
  </main>;
}
