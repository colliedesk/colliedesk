import { lazyPart } from '../../ui/lazy-part';

// "Send from Gmail" loads in its own chunk: Settings is rarely opened and the main bundle has a fixed budget
// (docs/internals/performance.md). It is a module of its own so that a page in the first bundle (an agent
// request card) can show the block without carrying the whole email channel section.
export const gmailPart = lazyPart(() => import('./gmail-connection'));
