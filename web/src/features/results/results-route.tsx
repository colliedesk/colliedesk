import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { api } from '../../api/client';
import { resultsApi } from '../../api/results';
import type { WindowKey } from '../../api/results';
import { resultsMessages as m } from './results-messages';
import { ResultsPage } from './results-page';
import type { NumberLink } from './results-page';
import './results.css';

// A number opens the inbox on the conversations it counts, in any status.
const numberLink: NumberLink = ({ result, className, children }) => <Link to="/" search={{ status: 'all', result }} className={className}>{children}</Link>;

export function ResultsRoute({ window: initial = '30d', onWindow }: { window?: WindowKey; onWindow?: (window: WindowKey) => void }) {
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  // The numbers are computed in the background; while the first ones or the baseline are being worked
  // out, the page asks again every 30 seconds so it updates by itself.
  const results = useQuery({
    queryKey: ['results'], queryFn: resultsApi.get, enabled: Boolean(me.data),
    refetchInterval: (query) => (query.state.data && (query.state.data.current === null || query.state.data.baseline_state === 'computing') ? 30_000 : false),
  });
  const [window, setWindow] = useState<WindowKey>(initial);
  // "Updated 40 min ago" moves with the clock.
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const timer = setInterval(() => setNow(new Date()), 60_000);
    return () => clearInterval(timer);
  }, []);
  const isAdmin = me.data?.agent.role === 'admin';
  return <ResultsPage results={results.data} loading={results.isPending || me.isPending} failed={results.isError || me.isError}
    onRetry={() => void results.refetch()} window={window} onWindow={(value) => { setWindow(value); onWindow?.(value); }}
    timezone={me.data?.timezone ?? 'UTC'} isAdmin={isAdmin} now={now} link={numberLink}
    importLink={<Link to="/settings/import-freshdesk" className="button secondary">{m.openImport}</Link>} />;
}
