import { useSyncExternalStore } from 'react';

// The width under which the app takes its phone layout (layout.css changes the shell there).
export const phoneQuery = '(max-width: 44rem)';

export const isPhone = () => window.matchMedia?.(phoneQuery).matches ?? false;

const subscribePhone = (change: () => void) => {
  const query = window.matchMedia?.(phoneQuery);
  query?.addEventListener('change', change);
  return () => query?.removeEventListener('change', change);
};

// usePhone follows the width as it changes, for example when a phone is turned on its side.
export const usePhone = () => useSyncExternalStore(subscribePhone, isPhone);
