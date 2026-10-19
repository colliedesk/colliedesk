import { lazyPart } from '../../ui/lazy-part';

// The customer page, from its own chunk. The inbox preloads it when idle, so a click on a customer finds it there.
export const contactPart = lazyPart(() => import('./contact-route'));
