import { lazyPart } from '../../ui/lazy-part';

// Requesting an article from a conversation and making one from a sent reply are a chunk of their own, loaded
// once a conversation is open; the first bundle carries none of it.
export const articleRequestPart = lazyPart(() => import('./article-request'));
