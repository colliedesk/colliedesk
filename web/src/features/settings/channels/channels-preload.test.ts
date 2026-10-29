import { QueryClient } from '@tanstack/react-query';
import { expect, test, vi } from 'vitest';
import { preloadChannelsPage } from './channels-preload';

const list = vi.hoisted(() => vi.fn(async (_path: string, _schema: unknown) => ({ items: [], types: [] })));
vi.mock('../../../api/client', () => ({ request: list }));
vi.mock('../../../api/channels', () => ({ channelListSchema: { parse: (value: unknown) => value } }));
const route = vi.hoisted(() => ({ requested: false }));
vi.mock('./channels-route', () => { route.requested = true; return { ChannelsRoute: () => null }; });

test('pointing at the email setup link starts the page chunk and the channel list together, and the list is cached for the page', async () => {
  const client = new QueryClient();
  preloadChannelsPage(client, '/settings/channels/type/email');
  await vi.waitFor(() => expect(list).toHaveBeenCalledTimes(1));
  expect(list.mock.calls[0][0]).toBe('/api/v1/channels');
  await vi.waitFor(() => expect(route.requested).toBe(true));
  await vi.waitFor(() => expect(client.getQueryData(['channels'])).toEqual({ items: [], types: [] }));
});

test('an address outside Channels starts nothing', () => {
  list.mockClear();
  preloadChannelsPage(new QueryClient(), '/settings');
  expect(list).not.toHaveBeenCalled();
});
