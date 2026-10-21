import type { ReactNode } from 'react';
import { lazyPart, Part, PartUnavailable } from '../../ui/lazy-part';

// The customers feature, from its own chunk (contacts-chunk.ts). The inbox preloads it when idle, so a
// click on a customer or on New conversation finds it there.
export const contactPart = lazyPart(() => import('./contacts-chunk'));

type ContactsChunk = typeof import('./contacts-chunk');

// ContactsPart renders a piece of the chunk once it is there; until then nothing, and if it cannot load,
// failed (by default the part's own "could not be loaded" line), so only that piece says so.
export function ContactsPart({ render, failed = <PartUnavailable /> }: { render: (chunk: ContactsChunk) => ReactNode; failed?: ReactNode }) {
  return <Part part={contactPart} render={render} failed={failed} />;
}
