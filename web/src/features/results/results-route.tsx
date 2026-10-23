import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { api } from '../../api/client';
import { resultsApi } from '../../api/results';
import type { WindowKey } from '../../api/results';
import { resultsMessages as m } from './results-messages';
import { ResultsAdmin } from './results-admin';
import { ResultsPage } from './results-page';
import type { NumberLink } from './results-page';
import './results.css';

// A number opens the inbox on the conversations it counts, in any status.
const numberLink: NumberLink = ({ result, className, children }) => <Link to="/" search={{ status: 'all', result }} className={className}>{children}</Link>;

// The period is in the address (?window=7d), so a number opened from it comes back to the same period.
export function ResultsRoute({ window = '30d', onWindow }: { window?: WindowKey; onWindow: (window: WindowKey) => void }) {
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  // The numbers are computed in the background (the worker looks every 15 seconds); while the first ones
  // or the baseline are being worked out, the page asks again every 3 seconds so it updates by itself.
  // The read is one stored row (20 ms budget), so this costs nothing that matters.
  const results = useQuery({
    queryKey: ['results'], queryFn: resultsApi.get, enabled: Boolean(me.data),
    refetchInterval: (query) => (query.state.data && (query.state.data.current === null || query.state.data.baseline_state === 'computing') ? 3_000 : false),
  });
  // "Updated 40 min ago" moves with the clock.
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const timer = setInterval(() => setNow(new Date()), 60_000);
    return () => clearInterval(timer);
  }, []);
  const isAdmin = me.data?.agent.role === 'admin';
  const client = useQueryClient();
  const shareLink = useQuery({ queryKey: ['results-share-link'], queryFn: resultsApi.shareLink, enabled: isAdmin });
  // The address of a link made now: it is shown once and never stored.
  const [created, setCreated] = useState<string>();
  const [replacedOld, setReplacedOld] = useState(false);
  const share = useMutation({
    mutationFn: async (action: 'create' | 'off') => {
      if (action === 'off') return { ...(await resultsApi.revokeShareLink()), url: undefined };
      return resultsApi.createShareLink();
    },
    onMutate: (action) => { setReplacedOld(action === 'create' && Boolean(shareLink.data?.link)); setCreated(undefined); },
    onSuccess: (answer) => { setCreated(answer.url); client.setQueryData(['results-share-link'], { link: answer.link }); },
  });
  const weekly = useMutation({
    mutationFn: resultsApi.setWeekly,
    onSuccess: () => client.invalidateQueries({ queryKey: ['results'] }),
  });
  const weeklyState = results.data?.weekly_email && weekly.isPending && weekly.variables !== undefined
    ? { ...results.data.weekly_email, subscribed: weekly.variables } : results.data?.weekly_email;
  const timezone = me.data?.timezone ?? 'UTC';
  return <ResultsPage results={results.data} loading={results.isPending || me.isPending} failed={results.isError || me.isError}
    onRetry={() => void results.refetch()} window={window} onWindow={onWindow}
    timezone={timezone} isAdmin={isAdmin} now={now} link={numberLink}
    importLink={<Link to="/settings/import-freshdesk" className="button">{m.openImport}</Link>}
    admin={isAdmin ? <ResultsAdmin timezone={timezone} link={shareLink.data?.link} linkFailed={shareLink.isError} created={created} replacedOld={replacedOld}
      busy={share.isPending} shareFailed={share.isError} onCreate={() => share.mutate('create')} onTurnOff={() => share.mutate('off')}
      weekly={weeklyState} weeklyBusy={weekly.isPending} weeklyFailed={weekly.isError} onWeekly={(value) => weekly.mutate(value)} /> : undefined} />;
}
