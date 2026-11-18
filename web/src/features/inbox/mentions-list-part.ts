import { lazyPart } from '../../ui/lazy-part';

// The Mentions list loads from its own chunk: only the Mentions page shows it. Its routes wait for it (app.tsx), so
// the page that was on screen stays until the list can show.
export const mentionsListPart = lazyPart(() => import('./mentions-list'));
