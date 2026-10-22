import { lazyPart } from '../../ui/lazy-part';

// Presence (who else has a conversation open, and this tab's announces) comes from its own chunk, loaded
// the first time a conversation is open or somebody else is there.
export const presenceViewPart = lazyPart(() => import('./presence-view'));
