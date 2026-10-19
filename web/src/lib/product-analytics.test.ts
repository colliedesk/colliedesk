import { afterEach, describe, expect, it, vi } from 'vitest';
import { resetProductAnalytics, resetProductAnalyticsForTest, scrubEvent, startProductAnalytics, track } from './product-analytics';

// The loader stands in for posthog-js, the external analytics service.
function fakePostHog() {
  return { init: vi.fn(), identify: vi.fn(), register: vi.fn(), capture: vi.fn(), reset: vi.fn() };
}

const me = {
  agent: { id: '0b8f7a52-1d2c-4a8e-9b1a-5f2c1e7d9a10', email: 'ada@acme.example.com', display_name: 'Ada', role: 'admin' as const, timezone_override: null },
  workspace: { id: '5c1d2e3f-4a5b-4c6d-8e7f-9a0b1c2d3e4f', slug: 'acme', name: 'Acme Support' },
  plan: { plan: 'trial' as const },
};

afterEach(() => resetProductAnalyticsForTest());

describe('startProductAnalytics', () => {
  it('never loads the analytics library when the server does not enable it', async () => {
    const loader = vi.fn();
    expect(await startProductAnalytics({ enabled: false, key: null, host: null, realm: null }, me, loader)).toBeNull();
    expect(await startProductAnalytics(undefined, me, loader)).toBeNull();
    expect(loader).not.toHaveBeenCalled();
  });

  it('starts PostHog without storage, replay or visible text, and names the agent by id only', async () => {
    const posthog = fakePostHog();
    await startProductAnalytics({ enabled: true, key: 'phc_cloudkey', host: 'https://us.i.posthog.com', realm: 'self-hosted' }, me, async () => posthog);

    expect(posthog.init).toHaveBeenCalledTimes(1);
    const [key, options] = posthog.init.mock.calls[0];
    expect(key).toBe('phc_cloudkey');
    expect(options).toMatchObject({
      api_host: 'https://us.i.posthog.com',
      persistence: 'memory',
      disable_session_recording: true,
      mask_all_text: true,
      mask_all_element_attributes: true,
      // A switch flipped in PostHog's settings must not widen what the app sends.
      disable_capture_url_hashes: true,
      capture_heatmaps: false,
      capture_dead_clicks: false,
      capture_performance: false,
      disable_external_dependency_loading: true,
    });
    expect(posthog.identify).toHaveBeenCalledWith('0b8f7a52-1d2c-4a8e-9b1a-5f2c1e7d9a10', { role: 'admin' });
    expect(posthog.register).toHaveBeenCalledWith({ realm: 'self-hosted', workspace_id: '5c1d2e3f-4a5b-4c6d-8e7f-9a0b1c2d3e4f', plan: 'trial' });
    expect(JSON.stringify([posthog.identify.mock.calls, posthog.register.mock.calls])).not.toMatch(/ada@|Acme Support|Ada/);
  });

  it('on Colliedesk Cloud names the agent and the workspace, so the team knows which customer it is', async () => {
    const posthog = fakePostHog();
    await startProductAnalytics({ enabled: true, key: 'phc_cloudkey', host: 'https://us.i.posthog.com', realm: 'cloud' }, me, async () => posthog);
    expect(posthog.identify).toHaveBeenCalledWith('0b8f7a52-1d2c-4a8e-9b1a-5f2c1e7d9a10', { role: 'admin', email: 'ada@acme.example.com', name: 'Ada' });
    expect(posthog.register).toHaveBeenLastCalledWith({
      realm: 'cloud', workspace_id: '5c1d2e3f-4a5b-4c6d-8e7f-9a0b1c2d3e4f', workspace_name: 'Acme Support', workspace_slug: 'acme', plan: 'trial',
    });
  });

  it('loads PostHog once however often the shell renders', async () => {
    const posthog = fakePostHog();
    const loader = vi.fn(async () => posthog);
    const settings = { enabled: true, key: 'phc_cloudkey', host: 'https://us.i.posthog.com', realm: 'cloud' };
    await startProductAnalytics(settings, me, loader);
    await startProductAnalytics(settings, me, loader);
    expect(loader).toHaveBeenCalledTimes(1);
    expect(posthog.init).toHaveBeenCalledTimes(1);
  });
});

const cloud = { enabled: true, key: 'phc_cloudkey', host: 'https://us.i.posthog.com', realm: 'cloud' };

describe('realm', () => {
  it('is registered at start, before anyone signs in, so signed-out pageviews carry it', async () => {
    const posthog = fakePostHog();
    await startProductAnalytics(cloud, undefined, async () => posthog);
    expect(posthog.register).toHaveBeenCalledTimes(1);
    expect(posthog.register).toHaveBeenCalledWith({ realm: 'cloud' });
    expect(posthog.identify).not.toHaveBeenCalled();
  });
});

describe('track', () => {
  it('does nothing before PostHog started, and does not queue the event for later', async () => {
    const posthog = fakePostHog();
    expect(track('saved_reply_inserted')).toBe(false);
    await startProductAnalytics(cloud, me, async () => posthog);
    expect(posthog.capture).not.toHaveBeenCalled();
  });

  it('forwards the event name and its properties once PostHog started', async () => {
    const posthog = fakePostHog();
    await startProductAnalytics(cloud, me, async () => posthog);
    expect(track('bulk_action_applied', { action: 'status', count: 3 })).toBe(true);
    track('saved_reply_inserted');
    expect(posthog.capture).toHaveBeenNthCalledWith(1, 'bulk_action_applied', { action: 'status', count: 3 });
    expect(posthog.capture).toHaveBeenNthCalledWith(2, 'saved_reply_inserted', undefined);
  });

  it('never throws into the app when the library fails', async () => {
    const posthog = fakePostHog();
    posthog.capture.mockImplementation(() => { throw new Error('boom'); });
    await startProductAnalytics(cloud, me, async () => posthog);
    expect(track('saved_reply_inserted')).toBe(false);
  });
});

describe('resetProductAnalytics', () => {
  it('forgets the signed-out agent but keeps the realm on later events', async () => {
    const posthog = fakePostHog();
    await startProductAnalytics(cloud, me, async () => posthog);
    resetProductAnalytics();
    expect(posthog.reset).toHaveBeenCalledTimes(1);
    expect(posthog.register).toHaveBeenLastCalledWith({ realm: 'cloud' });
    expect(posthog.reset.mock.invocationCallOrder[0]).toBeLessThan(posthog.register.mock.invocationCallOrder.at(-1)!);
  });

  it('does nothing when analytics never started', () => {
    expect(() => resetProductAnalytics()).not.toThrow();
  });
});

describe('scrubEvent', () => {
  it('drops search terms, fragments and page titles, which can hold a customer address', () => {
    const event = scrubEvent({
      uuid: '01a1524f-3bd0-7b2b-b2fd-edb4cf9852dc',
      event: '$pageview',
      properties: {
        $current_url: 'https://app.colliedesk.com/?q=ada%40customer.example.com#c1',
        $referrer: 'https://app.colliedesk.com/settings?tab=team',
        title: '(2) Colliedesk',
        $pathname: '/',
      },
    });
    expect(event?.properties).toEqual({ $current_url: 'https://app.colliedesk.com/', $referrer: 'https://app.colliedesk.com/settings', $pathname: '/' });
  });

  // posthog-js puts $set and $set_once on the event itself, next to properties: a sign-in link's
  // one-time token reached the person this way before the fix.
  it('cleans the person URLs posthog-js sends beside the properties, such as a sign-in token', () => {
    const event = scrubEvent({
      uuid: '01a1524f-3bd0-7b2b-b2fd-edb4cf9852de',
      event: '$identify',
      properties: { $current_url: 'https://app.colliedesk.com/' },
      $set: { role: 'admin', $current_url: 'https://app.colliedesk.com/sign-in?token=abc123#x' },
      $set_once: { $initial_current_url: 'https://app.colliedesk.com/sign-in?token=abc123', $initial_referrer: '$direct' },
    } as never);
    expect(JSON.stringify(event)).not.toContain('abc123');
    expect((event as unknown as { $set_once: Record<string, unknown> }).$set_once).toEqual({
      $initial_current_url: 'https://app.colliedesk.com/sign-in', $initial_referrer: '$direct',
    });
    expect((event as unknown as { $set: Record<string, unknown> }).$set).toEqual({ role: 'admin', $current_url: 'https://app.colliedesk.com/sign-in' });
  });

  it('cleans the URLs PostHog keeps on the person and the session too', () => {
    const event = scrubEvent({
      uuid: '01a1524f-3bd0-7b2b-b2fd-edb4cf9852dd',
      event: '$identify',
      properties: {
        $session_entry_url: 'https://app.colliedesk.com/?q=ada%40customer.example.com',
        $session_entry_referrer: 'https://colliedesk.com/pricing?utm_source=hn#plans',
        $set_once: { $initial_current_url: 'https://app.colliedesk.com/?q=ada%40customer.example.com', $initial_referrer: 'https://x.example/?ref=1' },
        $set: { $current_url: 'https://app.colliedesk.com/#c1', role: 'admin' },
      },
    });
    expect(event?.properties).toEqual({
      $session_entry_url: 'https://app.colliedesk.com/',
      $session_entry_referrer: 'https://colliedesk.com/pricing',
      $set_once: { $initial_current_url: 'https://app.colliedesk.com/', $initial_referrer: 'https://x.example/' },
      $set: { $current_url: 'https://app.colliedesk.com/', role: 'admin' },
    });
  });
});
