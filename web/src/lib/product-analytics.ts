import { useEffect } from 'react';
import type { CaptureResult, PostHog, PostHogConfig } from 'posthog-js';

// Product analytics for the hosted cloud only. The server's public config says whether to report
// (a self-hosted server never enables it), and only then is posthog-js loaded, as its own chunk.
// PostHog keeps nothing in the browser, records no session, and sees no text: every element's
// text and attributes are masked, and search terms, fragments and titles are dropped from URLs.
// Agents are named by their random id; no email address or name leaves the app.

export type ProductAnalyticsSettings = { enabled: boolean; key: string | null; host: string | null };
type Identity = {
  agent: { id: string; role: string };
  workspace: { id: string };
  plan: { plan: string } | null;
};
type Client = Pick<PostHog, 'init' | 'identify' | 'register'>;
export type PostHogLoader = () => Promise<Client>;

const loadPostHog: PostHogLoader = async () => (await import('posthog-js')).default;

const URL_PROPERTIES = ['$current_url', '$referrer', '$initial_current_url', '$initial_referrer'];

// scrubEvent removes what could carry a customer's words: a search query, a fragment, a page title.
export function scrubEvent(event: CaptureResult | null): CaptureResult | null {
  if (!event) return event;
  const properties = { ...event.properties };
  for (const key of URL_PROPERTIES) {
    const value = properties[key];
    if (typeof value !== 'string') continue;
    try {
      const url = new URL(value);
      url.search = '';
      url.hash = '';
      properties[key] = url.toString();
    } catch {
      delete properties[key];
    }
  }
  delete properties.title;
  delete properties.$title;
  return { ...event, properties };
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
    capture_exceptions: true,
    before_send: scrubEvent,
  };
}

let client: Promise<Client> | null = null;

// startProductAnalytics loads and starts PostHog once, then names the signed-in agent.
export async function startProductAnalytics(settings: ProductAnalyticsSettings | undefined, me: Identity | undefined,
  loader: PostHogLoader = loadPostHog): Promise<Client | null> {
  if (!settings?.enabled || !settings.key || !settings.host) return null;
  const { key, host } = settings;
  client ??= loader().then((posthog) => {
    posthog.init(key, options(host));
    return posthog;
  });
  const posthog = await client;
  if (me) {
    posthog.identify(me.agent.id, { role: me.agent.role });
    posthog.register({ workspace_id: me.workspace.id, plan: me.plan?.plan ?? null });
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
}
