import { useInfiniteQuery, useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { api } from '../../api/client';
import { autofixApi, keys } from './autofix-api';
import type { FixConnector, FixGroup } from './autofix-api';
import { useAutofixSettings } from './autofix-conversation';
import { FixesPage } from './fixes-page';
import type { FixGroupRows } from './fixes-page';

interface FixesRouteProps {
  repoId?: string;
  connector?: FixConnector;
  onFilter: (repoId: string | undefined, connector: FixConnector | undefined) => void;
}

const conversationLink = (id: string, label: string) => <Link to="/conversations/$conversationId" params={{ conversationId: id }}>{label}</Link>;
const settingsLink = (label: string) => <Link to="/settings/autofix" className="af-btn">{label}</Link>;
const inboxLink = (label: string) => <Link to="/" className="af-btn">{label}</Link>;

// useGroup reads one group of the Fixes page, a page at a time, filtered on the server.
function useGroup(group: FixGroup, repoId: string | undefined, connector: FixConnector | undefined): FixGroupRows & { refetch: () => void } {
  const list = useInfiniteQuery({
    queryKey: [...keys.list, group, repoId, connector],
    queryFn: ({ pageParam }) => autofixApi.list({ group, repo_id: repoId, connector, cursor: pageParam || undefined }),
    initialPageParam: '',
    getNextPageParam: (page) => page.next_cursor ?? undefined,
    refetchInterval: group === 'done' ? false : 30_000,
  });
  return {
    items: list.data?.pages.flatMap((page) => page.items) ?? [],
    load: list.isPending ? 'loading' : list.isError ? 'error' : 'ready',
    more: list.hasNextPage, loadingMore: list.isFetchingNextPage, onMore: () => void list.fetchNextPage(), refetch: () => void list.refetch(),
  };
}

export function FixesRoute({ repoId, connector, onFilter }: FixesRouteProps) {
  const settings = useAutofixSettings();
  const me = useQuery({ queryKey: ['me'], queryFn: api.me, staleTime: Infinity });
  const status = useQuery({ queryKey: keys.status, queryFn: () => autofixApi.status() });
  const groups = { needs_person: useGroup('needs_person', repoId, connector), running: useGroup('running', repoId, connector), done: useGroup('done', repoId, connector) };
  return <FixesPage groups={groups} counts={status.data} repos={settings.data?.repos ?? []} repoId={repoId} connector={connector} onFilter={onFilter}
    timezone={me.data?.timezone ?? 'UTC'} now={Date.now()} conversationLink={conversationLink} settingsLink={settingsLink} inboxLink={inboxLink}
    onRetry={() => { for (const rows of Object.values(groups)) rows.refetch(); }} />;
}
