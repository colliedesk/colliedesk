import { useEffect } from 'react';
import type { CaptureResult, PostHog, PostHogConfig } from 'posthog-js';
import { recordProductEvent } from './diagnostics';

// Product analytics. The server's public config says whether to report (DISABLE_TELEMETRY or
// DO_NOT_TRACK turn it off, on the cloud and self-hosted alike), and only then is posthog-js
// loaded, as its own chunk.
// PostHog keeps nothing in the browser, records no session, and sees no text: every element's
// text and attributes are masked, and search terms, fragments and titles are dropped from URLs.
// On Colliedesk Cloud an agent is named by email and display name and the workspace by name and slug,
// so the team knows which customer is which. A self-hosted install sends the random ids only.

export type ProductAnalyticsSettings = { enabled: boolean; key: string | null; host: string | null; realm: string | null };
type Identity = {
  agent: { id: string; role: string; email: string; display_name: string };
  workspace: { id: string; name: string; slug: string };
  plan: { plan: string } | null;
};
type Client = Pick<PostHog, 'init' | 'identify' | 'register' | 'capture' | 'reset'>;
export type PostHogLoader = () => Promise<Client>;

// The no-external build never loads a script from PostHog's CDN into a page that shows customers'
// conversations. Its extensions (error capture, replay) are left out, which keeps the chunk small.
const loadPostHog: PostHogLoader = async () => (await import('posthog-js/no-external')).default;

const URL_PROPERTIES = ['$current_url', '$referrer', '$initial_current_url', '$initial_referrer',
  '$session_entry_url', '$session_entry_referrer'];

// cleanURLs drops the query and fragment of every URL property; a non-URL value is dropped if it has either.
function cleanURLs(properties: Record<string, unknown>): Record<string, unknown> {
  const cleaned = { ...properties };
  for (const key of URL_PROPERTIES) {
    const value = cleaned[key];
    if (typeof value !== 'string') continue;
    try {
      const url = new URL(value);
      url.search = '';
      url.hash = '';
      cleaned[key] = url.toString();
    } catch {
      // Not a URL: PostHog's own markers such as "$direct" stay; anything that could carry a query goes.
      if (/[?#]/.test(value)) delete cleaned[key];
    }
  }
  return cleaned;
}

// scrubEvent removes what could carry a customer's words: a search query, a fragment, a page title,
// on the event and on the person properties PostHog keeps ($set, $set_once).
export function scrubEvent(event: CaptureResult | null): CaptureResult | null {
  if (!event) return event;
  const properties = cleanURLs(event.properties ?? {});
  cleanPersonProperties(properties);
  delete properties.title;
  delete properties.$title;
  // posthog-js sends $set and $set_once beside the properties too, and the person keeps them for good.
  const scrubbed: Record<string, unknown> = { ...event, properties };
  cleanPersonProperties(scrubbed);
  return scrubbed as unknown as CaptureResult;
}

function cleanPersonProperties(holder: Record<string, unknown>) {
  for (const key of ['$set', '$set_once']) {
    const nested = holder[key];
    if (nested && typeof nested === 'object') holder[key] = cleanURLs(nested as Record<string, unknown>);
  }
}

function options(host: string): Partial<PostHogConfig> {
  return {
    api_host: host,
    defaults: '2026-05-30',
    persistence: 'memory',
    person_profiles: 'identified_only',
    disable_session_recording: true,
    disable_surveys: true,
    advanced_disable_flags: true,
    mask_all_text: true,
    mask_all_element_attributes: true,
    capture_pageview: 'history_change',
    // Fixed here so a switch flipped in PostHog's project settings cannot widen what is sent.
    disable_capture_url_hashes: true,
    capture_heatmaps: false,
    capture_dead_clicks: false,
    capture_performance: false,
    disable_external_dependency_loading: true,
    before_send: scrubEvent,
  };
}

// The events the web app reports. A property is a boolean, a number or a member of a small union:
// never a contact, a message, an id or anything a person typed. The one exception is the answer to
// the trial survey, which the admin sends by pressing Send.
export type ConversationStatus = 'open' | 'pending' | 'resolved' | 'closed';
export type SettingsSection = 'profile' | 'preferences' | 'general' | 'team' | 'invites' | 'sessions' | 'email_channel'
  | 'web_form' | 'saved_replies' | 'help_center' | 'export' | 'billing';
export type SurveyId = 'switch_blocker_v1';
export type InboxFilterKind = 'status' | 'priority' | 'assignee' | 'tag' | 'contact' | 'organization' | 'awaiting' | 'draft' | 'delivery'
  | 'source' | 'channel' | 'created' | 'updated' | 'sort';

export interface EventProperties {
  conversation_opened: { source: 'list' | 'link' };
  // filter names the kind of filter only, never a value: tag also covers tag_match, created and updated cover both ends of their range.
  inbox_filtered: { filter: InboxFilterKind };
  inbox_searched: { has_results: boolean };
  saved_reply_inserted: undefined;
  conversation_status_changed: { to: ConversationStatus };
  conversation_assigned: { to_self: boolean };
  bulk_action_applied: { action: 'status' | 'assign' | 'add_tag' | 'remove_tag'; count: number };
  agent_request_decided: { decision: 'approved' | 'rejected' };
  settings_section_opened: { section: SettingsSection };
  workspace_export_requested: undefined;
  problem_reported: undefined;
  survey_answered: { survey: SurveyId; answer: string };
  survey_dismissed: { survey: SurveyId };
}
export type EventName = keyof EventProperties;
type PropertiesArg<E extends EventName> = EventProperties[E] extends undefined ? [properties?: undefined] : [properties: EventProperties[E]];

let client: Promise<Client> | null = null;
// started is set once PostHog is initialised; until then track() drops events instead of queueing them.
let started: Client | null = null;
let realm: string | null = null;

// track reports a named event. It does nothing until PostHog started (analytics off, still loading,
// or blocked), and a failure of the library never reaches the app. It answers whether the event was
// handed to PostHog, which only the survey needs: it must not say "thank you" for an answer that was dropped.
export function track<E extends EventName>(event: E, ...[properties]: PropertiesArg<E>): boolean {
  // The name alone is kept for a problem report, whether or not analytics runs.
  recordProductEvent(event);
  if (!started) return false;
  try {
    started.capture(event, properties);
    return true;
  } catch {
    return false;
  }
}

// resetProductAnalytics forgets the signed-out agent so later events do not carry their id.
// reset() also clears registered properties, so the realm is put back.
export function resetProductAnalytics(): void {
  if (!started) return;
  try {
    started.reset();
    started.register({ realm });
  } catch {
    // Analytics never affects the app.
  }
}

// startProductAnalytics loads and starts PostHog once, then names the signed-in agent.
export async function startProductAnalytics(settings: ProductAnalyticsSettings | undefined, me: Identity | undefined,
  loader: PostHogLoader = loadPostHog): Promise<Client | null> {
  if (!settings?.enabled || !settings.key || !settings.host) return null;
  const { key, host } = settings;
  client ??= loader().then((posthog) => {
    posthog.init(key, options(host));
    // Only the realm, so a pageview before sign-in says which deployment it came from and nothing more.
    realm = settings.realm;
    posthog.register({ realm });
    started = posthog;
    return posthog;
  });
  const posthog = await client;
  if (me && settings.realm === 'cloud') {
    // Our own customers: who they are is what the team needs to help them.
    posthog.identify(me.agent.id, { role: me.agent.role, email: me.agent.email, name: me.agent.display_name || undefined });
    posthog.register({ realm: settings.realm, workspace_id: me.workspace.id, workspace_name: me.workspace.name,
      workspace_slug: me.workspace.slug, plan: me.plan?.plan ?? null });
  } else if (me) {
    // A self-hosted install's agents are someone else's staff: only random ids leave it.
    posthog.identify(me.agent.id, { role: me.agent.role });
    posthog.register({ realm: settings.realm, workspace_id: me.workspace.id, plan: me.plan?.plan ?? null });
  }
  return posthog;
}

export function useProductAnalytics(settings: ProductAnalyticsSettings | undefined, me: Identity | undefined) {
  const enabled = settings?.enabled === true;
  const agentId = me?.agent.id;
  const plan = me?.plan?.plan;
  useEffect(() => {
    if (!enabled) return;
    // A failure to load analytics never affects the app.
    startProductAnalytics(settings, me).catch(() => undefined);
    // Re-run only when what is reported changes: the agent, or the workspace's plan.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled, agentId, plan]);
}

export function resetProductAnalyticsForTest() {
  client = null;
  started = null;
  realm = null;
}
