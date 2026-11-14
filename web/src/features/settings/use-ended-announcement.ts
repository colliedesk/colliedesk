import { useEffect, useRef, useState } from 'react';

// useEndedAnnouncement says, for a live region, that a row of a list ended while the page was open (an invite or a token
// revoked): the row's own text changes silently. A row that was already ended when it first appeared says nothing.
export function useEndedAnnouncement<T>(items: readonly T[], idOf: (item: T) => string, ended: (item: T) => boolean, said: (item: T) => string): string {
  const [text, setText] = useState('');
  const live = useRef(new Set<string>());
  useEffect(() => {
    for (const item of items) {
      const id = idOf(item);
      if (!ended(item)) live.current.add(id);
      else if (live.current.delete(id)) setText(said(item));
    }
  });
  return text;
}
