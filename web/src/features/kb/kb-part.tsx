import { lazyPart } from '../../ui/lazy-part';

// The knowledge base, from its own chunk (kb-chunk.ts): its pages and the Markdown editor. The rail link
// preloads it on hover and focus, so a click finds it there.
export const kbPart = lazyPart(() => import('./kb-chunk'));

// The help center card in Settings is a small chunk of its own: Settings does not load the editor.
export const helpCenterPart = lazyPart(() => import('./help-center-card'));

// The articles list of the reply box, to link an article in a reply. It is small and loads on its own.
export const articleLinkPart = lazyPart(() => import('./article-link-picker'));
