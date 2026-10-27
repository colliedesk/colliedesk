import { lazyPart } from '../../ui/lazy-part';

// The Autofix screens load from their own chunk, and only where /api/v1/config says Autofix is there.
export const autofixPart = lazyPart(() => import('./autofix-chunk'));
