import { useQueryClient } from '@tanstack/react-query';

// Every write can change the counts in the tree, the list and the trash, so all three are read again.
export function useRefreshKB() {
  const client = useQueryClient();
  return () => Promise.all([client.invalidateQueries({ queryKey: ['kb-collections'] }), client.invalidateQueries({ queryKey: ['kb-articles'] }),
    client.invalidateQueries({ queryKey: ['kb-trash'] })]);
}
