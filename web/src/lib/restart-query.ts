import type { QueryClient, QueryKey } from '@tanstack/react-query';

// Sends a query's request again, even while the first one is still waiting. A plain refetch() on a query with no
// data yet joins the request in flight, so a stalled connection would keep the person waiting on it. Cancelling
// first drops that request: aborted when the queryFn passes its signal to fetch, otherwise its answer is ignored.
export async function restartQuery(client: QueryClient, queryKey: QueryKey) {
  await client.cancelQueries({ queryKey, exact: true });
  await client.refetchQueries({ queryKey, exact: true });
}
