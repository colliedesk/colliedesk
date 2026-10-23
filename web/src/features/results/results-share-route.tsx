import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { sharedResults } from '../../api/results';
import type { Results, SharedResults, WindowKey } from '../../api/results';
import { resultsMessages as m } from './results-messages';
import { ResultsPage } from './results-page';
import type { NumberLink } from './results-page';
import './results.css';

// Numbers on the share page open nothing: the conversations need a seat.
const plainNumber: NumberLink = ({ className, children }) => <span className={className === 'results-number' ? 'results-number results-number-plain' : undefined}>{children}</span>;

// asResults turns the shared totals into the page's shape. Snapshot ids stay empty: nothing links.
function asResults(shared: SharedResults): Results {
  const side = (value: SharedResults['current'], extra: object) => (value ? { id: '', computed_at: shared.computed_at ?? value.measured_at, measured_at: value.measured_at, windows: value.windows, ...extra } : null);
  return {
    current: side(shared.current, {}),
    baseline: side(shared.baseline, { source: shared.baseline_from === 'helpscout' ? 'helpscout:' : 'freshdesk:', import_status: 'OK', stats_read: 1 }),
    baseline_state: shared.baseline ? 'ready' : 'none',
    hours_saved: shared.hours_saved,
    error: null,
    weekly_email: null,
  };
}

const timezone = (() => {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
  } catch {
    return 'UTC';
  }
})();

// The public page of a results share link: /r#<token>. The token is in the fragment, so it never
// reaches a server log; it is sent in the body of the request.
export function ResultsShareRoute() {
  const token = window.location.hash.replace(/^#/, '');
  const shared = useQuery({ queryKey: ['results-shared', token], queryFn: () => sharedResults(token), enabled: token !== '', retry: false });
  const [period, setPeriod] = useState<WindowKey>('30d');
  if (token === '' || shared.data === null) {
    return <main className="settings-page results-page results-share-off">
      <section className="results-card results-state" role="alert"><h1>{m.share.off}</h1><p>{m.share.offWhy}</p></section>
      <footer className="results-footer"><span className="results-muted">{m.share.madeWith}</span></footer>
    </main>;
  }
  if (shared.isError) {
    return <main className="settings-page results-page"><section className="results-card results-state" role="alert"><h1>{m.share.failed}</h1>
      <button type="button" onClick={() => void shared.refetch()}>{m.tryAgain}</button></section></main>;
  }
  if (!shared.data) return <main className="settings-page results-page" aria-busy="true"><p role="status" className="results-muted">{m.share.loading}</p></main>;
  return <ResultsPage results={asResults(shared.data)} loading={false} failed={false} onRetry={() => void shared.refetch()} window={period} onWindow={setPeriod}
    timezone={timezone} isAdmin={false} now={new Date()} link={plainNumber} importLink={null} shared={{ workspace: shared.data.workspace_name }} />;
}
