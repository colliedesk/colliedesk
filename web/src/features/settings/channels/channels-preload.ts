import type { QueryClient } from '@tanstack/react-query';
import { z } from 'zod';
import { request } from '../../../api/client';
import type { ChannelList } from '../../../api/channels';
import { lazyPart } from '../../../ui/lazy-part';

// Settings → Channels is its own chunk. Everything that page needs goes out together, from the moment a
// person points at a link to it or follows one, not one download after another: the page's chunk, the
// panels of the channel type in the address, and the channel list.
export const channelsRoutePart = lazyPart(() => import('./channels-route'));

const base = '/settings/channels';
// The panels chunk of each type that has a home page; the type's own lazy part loads the same module later.
const typePanels: Record<string, () => Promise<unknown>> = {
  email: () => import('../../../channels/email/panels'),
  web_form: () => import('../../../channels/web_form/panels'),
};

// preloadChannelsPage takes the address of a Channels page, with or without a query. Calling it again for
// the same address costs nothing: parts load once and the list is fresh for the query client's stale time.
export function preloadChannelsPage(client: QueryClient, href: string) {
  if (!href.startsWith(base)) return;
  channelsRoutePart.preload();
  const [first, second] = href.slice(base.length).split(/[?#]/, 1)[0].split('/').filter(Boolean);
  // A type's page also needs that type's panels.
  if (first === 'type' || first === 'new') void typePanels[second ?? '']?.().catch(() => undefined);
  // The list is what a type's home and a channel's page both read.
  if (first) void client.prefetchQuery({ queryKey: ['channels'], queryFn: listChannels });
}

// The request leaves now; the schema that checks the answer comes with the API chunk, which loads beside it.
// (The same call as channelsApi.list, whose schemas are not in the first bundle.)
async function listChannels(): Promise<ChannelList> {
  const [answer, { channelListSchema }] = await Promise.all([request('/api/v1/channels', z.unknown()), import('../../../api/channels')]);
  return channelListSchema.parse(answer);
}
