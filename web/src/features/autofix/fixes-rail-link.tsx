import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { autofixApi, keys } from './autofix-api';
import { WrenchIcon } from './autofix-icons';
import { autofixMessages as m } from './autofix-messages';

// FixesRailLink is the rail's Fixes link: it shows once a repository is connected, and its badge counts the
// fixes that need a person. It loads with the Autofix chunk, so a browser without Autofix carries none of it.
export function FixesRailLink({ current }: { current: boolean }) {
  const fixes = useQuery({ queryKey: keys.status, queryFn: () => autofixApi.status(), refetchInterval: 60_000 }).data;
  if (!fixes || fixes.connected_repos === 0) return null;
  const waiting = fixes.needs_person;
  return (
    <Link to="/fixes" className="rail-link" data-tip={m.railFixes} aria-label={waiting > 0 ? m.railFixesWaiting(waiting) : m.railFixes} aria-current={current ? 'page' : undefined}>
      <WrenchIcon /><span className="rail-label" aria-hidden="true">{m.railFixes}</span>
      {waiting > 0 && <span className="rail-badge" aria-hidden="true">{waiting > 99 ? '99+' : waiting}</span>}
    </Link>
  );
}
