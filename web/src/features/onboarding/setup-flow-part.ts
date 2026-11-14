import type { QueryClient } from '@tanstack/react-query';
import { api } from '../../api/client';
import { lazyPart } from '../../ui/lazy-part';
import { preloadChannelType } from '../settings/channels/channels-preload';

// The setup flow (/setup, one step per screen) is its own chunk: the first bundle has only its routes.
export const setupFlowPart = lazyPart(() => import('./setup-flow'));

// preloadSetupStep starts everything a step of the flow needs at once, not one download after another: the
// flow's chunk, the setup data, and what the step embeds (the email setup's panels and channel list, the
// import page). The guide calls it on hover and focus, the route when it opens. Repeats cost nothing.
export function preloadSetupStep(client: QueryClient, step?: string) {
  setupFlowPart.preload();
  void client.prefetchQuery({ queryKey: ['setup'], queryFn: api.setupProgress });
  if (step === 'email') preloadChannelType(client, 'email');
  if (step === 'import') void import('../settings/import-route').catch(() => undefined);
}
