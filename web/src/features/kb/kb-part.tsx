import { lazyPart } from '../../ui/lazy-part';

// The knowledge base, from its own chunk (kb-chunk.ts): its pages and the Markdown editor. The rail link
// preloads it on hover and focus, so a click finds it there.
export const kbPart = lazyPart(() => import('./kb-chunk'));
