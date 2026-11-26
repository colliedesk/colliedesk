import { useEffect, useRef } from 'react';
import { useQueryClient } from '@tanstack/react-query';

// useSetupFollows reloads the setup progress whenever data a step changes reloads (not on its first load), so
// Continue follows a save at once instead of at the next poll.
export function useSetupFollows(updatedAt: number) {
  const client = useQueryClient();
  const seen = useRef(updatedAt);
  useEffect(() => {
    const before = seen.current;
    seen.current = updatedAt;
    if (before !== 0 && before !== updatedAt) void client.invalidateQueries({ queryKey: ['setup'] });
  }, [client, updatedAt]);
}
