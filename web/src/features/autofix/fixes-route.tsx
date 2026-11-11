import { useInfiniteQuery, useQuery } from '@tanstack/react-query';
import { Link, useNavigate, useSearch } from '@tanstack/react-router';
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
  const agents = settings.data?.connectors.filter((item) => item.enabled).map((item) => item.kind) ?? [];
  return <FixesPage groups={groups} counts={status.data} repos={settings.data?.repos ?? []} agents={agents} repoId={repoId} connector={connector} onFilter={onFilter}
    timezone={me.data?.timezone ?? 'UTC'} now={Date.now()} conversationLink={conversationLink} settingsLink={settingsLink} inboxLink={inboxLink}
    onRetry={() => { for (const rows of Object.values(groups)) rows.refetch(); }} />;
}

const fixAgents: readonly FixConnector[] = ['claude_issue', 'copilot_issue', 'cursor_api'];

// FixesScreen is the /fixes page: its filters are in the address, and one that is not a repository id or a
// coding agent is left out.
export function FixesScreen() {
  const search = useSearch({ from: '/fixes' });
  const navigate = useNavigate();
  const repoId = typeof search.repo === 'string' && /^[0-9a-f-]{36}$/.test(search.repo) ? search.repo : undefined;
  const connector = fixAgents.find((agent) => agent === search.agent);
  return <FixesRoute repoId={repoId} connector={connector}
    onFilter={(repo, agent) => void navigate({ to: '/fixes', search: { ...(repo ? { repo } : {}), ...(agent ? { agent } : {}) }, replace: true })} />;
}
