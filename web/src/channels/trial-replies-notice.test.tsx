import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import type { ReactElement } from 'react';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import type { ChannelDetail } from '../api/channels';

// The help chat and web form tabs say, on the trial, that replies to their conversations need a verified
// sending domain or a plan (docs/user/help-chat.md, docs/user/web-form.md).
const mocks = vi.hoisted(() => ({ api: { me: vi.fn(), emailChannel: vi.fn() } }));
vi.mock('../api/client', async (importOriginal) => ({ ...(await importOriginal<typeof import('../api/client')>()), api: mocks.api }));
const helpChat = await import('./help_chat/panels');
const webForm = await import('./web_form/panels');

const ID = '44444444-4444-4444-8444-444444444444';
const channel = (type: 'help_chat' | 'web_form', config: Record<string, unknown>): ChannelDetail => ({
  id: ID, type, name: 'Channel', enabled: true, status: { state: 'working', reason_code: null, message: null }, summary: '', capabilities: [], held_count: 0, unsent_count: 0,
  last_inbound_at: null, created_at: '2026-10-30T00:00:00Z', updated_at: '2026-10-30T00:00:00Z', config, settings: {}, effective_settings: {},
} as ChannelDetail);
const chat = channel('help_chat', { greeting: 'Hi', suggestions: [], instructions: '', widget_origins: [] });
const form = channel('web_form', { enabled: true, allowed_origins: [], submit_path: '/api/v1/public/web-forms/x', legacy_slug: null, channel_id: ID });

const tabs: [string, (props: { channel: ChannelDetail }) => ReactElement][] = [
  ['help chat setup', ({ channel }) => <helpChat.Tabs.setup channel={channel} canManage timezone="UTC" />],
  ['web form embed', ({ channel }) => <webForm.Tabs.embed channel={channel} canManage timezone="UTC" />],
];
const email = (verified: boolean) => ({ channel: { support_address: 'support@desk.example', domain_verified_at: verified ? '2026-10-01T00:00:00Z' : null } });
const show = (tab: (typeof tabs)[number][1], opened: ChannelDetail) =>
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{tab({ channel: opened })}</QueryClientProvider>);

beforeEach(() => vi.clearAllMocks());

describe.each(tabs)('%s', (_name, tab) => {
  const opened = _name.startsWith('help') ? chat : form;

  test('on the trial with an unverified sending domain it says replies need a verified domain or a plan, and links both', async () => {
    mocks.api.me.mockResolvedValue({ plan: { plan: 'trial' } });
    mocks.api.emailChannel.mockResolvedValue(email(false));
    show(tab, opened);
    const notice = (await screen.findByText(/During the trial, replies to these conversations need a verified sending domain or a plan/)).closest('[role="status"]') as HTMLElement;
    expect(notice.querySelector('a[href="/settings/channels/type/email?tab=replies"]')).toHaveTextContent('Verify your sending domain in Settings');
    expect(notice.querySelector('a[href="/settings/billing"]')).toHaveTextContent('Choose a plan');
  });

  test.each([
    ['a verified sending domain', { plan: 'trial' }, true],
    ['a paid plan', { plan: 'team' }, false],
  ])('with %s it says nothing', async (_case, plan, verified) => {
    mocks.api.me.mockResolvedValue({ plan });
    mocks.api.emailChannel.mockResolvedValue(email(verified));
    show(tab, opened);
    await waitFor(() => expect(mocks.api.emailChannel).toHaveBeenCalled());
    await waitFor(() => expect(mocks.api.me).toHaveBeenCalled());
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(screen.queryByText(/replies to these conversations need/)).toBeNull();
  });
});
