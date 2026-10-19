import { afterEach, describe, expect, it, vi } from 'vitest';
import { resetProductAnalyticsForTest, scrubEvent, startProductAnalytics } from './product-analytics';

// The loader stands in for posthog-js, the external analytics service.
function fakePostHog() {
  return { init: vi.fn(), identify: vi.fn(), register: vi.fn() };
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
    expect(await startProductAnalytics({ enabled: false, key: null, host: null }, me, loader)).toBeNull();
    expect(await startProductAnalytics(undefined, me, loader)).toBeNull();
    expect(loader).not.toHaveBeenCalled();
  });

  it('starts PostHog without storage, replay or visible text, and names the agent by id only', async () => {
    const posthog = fakePostHog();
    await startProductAnalytics({ enabled: true, key: 'phc_cloudkey', host: 'https://us.i.posthog.com' }, me, async () => posthog);

    expect(posthog.init).toHaveBeenCalledTimes(1);
    const [key, options] = posthog.init.mock.calls[0];
    expect(key).toBe('phc_cloudkey');
    expect(options).toMatchObject({
      api_host: 'https://us.i.posthog.com',
      persistence: 'memory',
      disable_session_recording: true,
      mask_all_text: true,
      mask_all_element_attributes: true,
    });
    expect(posthog.identify).toHaveBeenCalledWith('0b8f7a52-1d2c-4a8e-9b1a-5f2c1e7d9a10', { role: 'admin' });
    expect(posthog.register).toHaveBeenCalledWith({ workspace_id: '5c1d2e3f-4a5b-4c6d-8e7f-9a0b1c2d3e4f', plan: 'trial' });
    expect(JSON.stringify([posthog.identify.mock.calls, posthog.register.mock.calls])).not.toMatch(/ada@|Acme Support|Ada/);
  });

  it('loads PostHog once however often the shell renders', async () => {
    const posthog = fakePostHog();
    const loader = vi.fn(async () => posthog);
    const settings = { enabled: true, key: 'phc_cloudkey', host: 'https://us.i.posthog.com' };
    await startProductAnalytics(settings, me, loader);
    await startProductAnalytics(settings, me, loader);
    expect(loader).toHaveBeenCalledTimes(1);
    expect(posthog.init).toHaveBeenCalledTimes(1);
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
});
