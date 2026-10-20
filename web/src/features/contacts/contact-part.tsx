import type { ReactNode } from 'react';
import { lazyPart, PartUnavailable } from '../../ui/lazy-part';

// The customers feature, from its own chunk (contacts-chunk.ts). The inbox preloads it when idle, so a
// click on a customer or on New conversation finds it there.
export const contactPart = lazyPart(() => import('./contacts-chunk'));

type ContactsChunk = typeof import('./contacts-chunk');

// ContactsPart renders a piece of the chunk once it is there; until then nothing, and if it cannot load,
// failed (by default the part's own "could not be loaded" line), so only that piece says so.
export function ContactsPart({ render, failed = <PartUnavailable /> }: { render: (chunk: ContactsChunk) => ReactNode; failed?: ReactNode }) {
  const part = contactPart.use();
  return part.status === 'ready' ? render(part.module) : part.status === 'error' ? failed : null;
}
