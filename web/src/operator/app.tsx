import { lazy, Suspense, useCallback, useEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { api, onSessionEnded } from './api';
import type { Config, Me, Status } from './api';
import { CommandLine } from './copy';
import { groupOrder } from './fields';
import { AlertIcon, HistoryIcon, HomeIcon, ListIcon, UsersIcon } from './icons';
import { ConsoleLogo } from './logo';
import { RequestsIcon, SettingsIcon } from '../ui/icons';
import { m } from './messages';
import { href, navigate, usePath } from './router';
import { SignIn } from './sign-in';
import { setTheme, storedTheme } from './theme';
import { Overview } from './overview';
import { ConfigPage } from './config-page';
import type { Draft } from './guards';
import { storeDraft, storedDraft } from './guards';

const Requests = lazy(() => import('./requests').then((module) => ({ default: module.Requests })));
const History = lazy(() => import('./history').then((module) => ({ default: module.History })));
const Audit = lazy(() => import('./audit').then((module) => ({ default: module.Audit })));
const Operators = lazy(() => import('./operators').then((module) => ({ default: module.Operators })));

// The console's data, read once for every screen: switching pages reads nothing again (design §10.5).
export interface ConsoleData {
  me: Me;
  status: Status;
  config: Config;
  reload: () => Promise<void>;
}

export function App() {
  const path = usePath();
  const [data, setData] = useState<{ me: Me; status: Status; config: Config } | null>(null);
  const [failed, setFailed] = useState(false);
  const [ended, setEnded] = useState(false);
  // The unsaved change of a configuration page lives here, above the pages, so a session that ends keeps it
  // in this tab until the operator signs in again (design §7.3 session-expired).
  const [draft, setDraft] = useState<Draft>(storedDraft);
  useEffect(() => { storeDraft(draft); }, [draft]);
  const load = useCallback(async () => {
    try {
      const [me, status, config] = await Promise.all([api.me(), api.status(), api.config()]);
      setData({ me, status, config });
      setFailed(false);
      setEnded(false);
    } catch {
      setFailed(true);
    }
  }, []);
  useEffect(() => {
    onSessionEnded(() => setEnded(true));
    if (!path.startsWith('/sign-in')) void load();
  }, [load, path]);
  // The band follows what the servers run: every 5 seconds while a process lags the version to run (a save
  // applying), every 30 seconds otherwise.
  const applying = Boolean(data && data.status.processes.some((process) => process.running_version !== data.status.desired_version));
  useEffect(() => {
    if (!data || ended) return undefined;
    const timer = window.setInterval(() => {
      void api.status().then((status) => setData((current) => (current ? { ...current, status } : current)), () => {});
    }, applying ? 5_000 : 30_000);
    return () => window.clearInterval(timer);
  }, [applying, ended, data]);
  if (path.startsWith('/sign-in')) return <SignIn onSignedIn={() => void load()} />;
  if (ended || (failed && !data)) {
    return <Shell data={data} ended={ended} path={path}><Ended /></Shell>;
  }
  if (!data) return <Shell data={null} ended={false} path={path}><div className="ops-page-inner" aria-busy="true"><div className="skeleton" /></div></Shell>;
  const shared: ConsoleData = { ...data, reload: load };
  let page: ReactNode;
  const group = path.startsWith('/config/') ? path.slice('/config/'.length) : '';
  if (group && groupOrder.includes(group)) page = <ConfigPage key={group} group={group} data={shared} draft={draft} setDraft={setDraft} />;
  else if (path === '/requests' || path.startsWith('/requests/')) page = <Requests data={shared} />;
  else if (path === '/history') page = <History data={shared} />;
  else if (path === '/audit') page = <Audit />;
  else if (path === '/operators') page = <Operators data={shared} />;
  else page = <Overview data={shared} />;
  return <Shell data={data} ended={false} path={path}><Suspense fallback={<div className="ops-page-inner" aria-busy="true"><div className="skeleton" /></div>}>{page}</Suspense></Shell>;
}

function Ended() {
  return <div className="ops-page-inner">
    <header className="page-head"><div><h1 id="page-title">{m.signedOutTitle}</h1><p>{m.signedOutLead}</p></div></header>
    <CommandLine command="docker compose exec server /app/collie operator pair --email <your email>" />
  </div>;
}

function NavLink({ to, path, icon, children, count }: { to: string; path: string; icon: ReactNode; children: string; count?: number }) {
  const current = to === '/' ? path === '/' : path === to || path.startsWith(`${to}/`);
  return <li><a href={href(to)} aria-current={current ? 'page' : undefined}
    onClick={(event) => { if (event.metaKey || event.ctrlKey || event.shiftKey) return; event.preventDefault(); navigate(to); }}>
    {icon}<span>{children}</span>{count ? <span className="seg-count">{count}</span> : null}</a></li>;
}

// ledgerState is what the band says about the version every process should run.
function ledgerState(status: Status) {
  const desired = status.desired_version;
  const total = status.processes.length;
  const on = status.processes.filter((process) => process.running_version === desired).length;
  const refused = status.warnings.find((warning) => warning.code === 'version_refused');
  if (refused) return { tone: 'danger', text: m.refusedVersion(refused.version ?? desired) };
  if (total > 0 && on < total) return { tone: 'busy', text: m.applyingVersion(desired) };
  return { tone: 'ok', text: desired ? m.runningVersion(desired, on, total || 1) : m.noVersion };
}

function Ledger({ status }: { status: Status | null }) {
  if (!status) return <span className="ops-ledger"><span className="ops-dot" />{m.sessionEnded}</span>;
  const { tone, text } = ledgerState(status);
  return <a className="ops-ledger" href={href('/history')} onClick={(event) => { event.preventDefault(); navigate('/history'); }}>
    <span className="ops-dot" data-tone={tone} /><span role="status">{text}</span></a>;
}

// MiniLedger is the band's version on a phone, beside the host, where the full ledger does not fit.
function MiniLedger({ status }: { status: Status }) {
  const { tone, text } = ledgerState(status);
  return <span className="ops-mini" title={text}><span className="ops-dot" data-tone={tone} />{status.desired_version ? m.runningShort(status.desired_version) : m.noVersion}</span>;
}

function Shell({ data, ended, path, children }: { data: { me: Me; status: Status } | null; ended: boolean; path: string; children: ReactNode }) {
  const [menu, setMenu] = useState(false);
  const [theme, setThemeState] = useState(storedTheme);
  const main = useRef<HTMLElement>(null);
  const menuButton = useRef<HTMLButtonElement>(null);
  useEffect(() => { main.current?.focus(); }, [path]);
  const waiting = data?.status.requests_waiting ?? 0;
  return (
    <div className="ops-shell">
      <a className="skip-link" href="#main">{m.skipToContent}</a>
      <header className="ops-band">
        <ConsoleLogo />
        <div className="ops-id"><strong>{m.consoleName}</strong><span className="ops-where"><span className="ops-host">{window.location.host}</span>
          {data && !ended && <MiniLedger status={data.status} />}</span></div>
        <Ledger status={ended ? null : data?.status ?? null} />
        {waiting > 0 && !ended && <a className="ops-requests" href={href('/requests')} onClick={(event) => { event.preventDefault(); navigate('/requests'); }}>
          <RequestsIcon /><span>{m.requestsWaiting(waiting)}</span></a>}
        {data && !ended && <div className="ops-account">
          <button type="button" ref={menuButton} aria-expanded={menu} aria-controls="ops-account-menu" aria-label={`${m.account}: ${data.me.operator.email}`} onClick={() => setMenu(!menu)}>
            <UsersIcon /><span className="acct-label">{data.me.operator.email}</span></button>
          {menu && <div id="ops-account-menu" className="ops-account-menu" onKeyDown={(event) => { if (event.key === 'Escape') { setMenu(false); menuButton.current?.focus(); } }}>
            <p>{m.account}<span>{data.me.operator.email}</span></p>
            <button type="button" onClick={() => { const next = theme === 'dark' ? 'light' : 'dark'; setTheme(next); setThemeState(next); }}>{theme === 'dark' ? m.lightTheme : m.darkTheme}</button>
            <button type="button" onClick={() => { void api.logout().finally(() => navigate('/sign-in', true)); }}>{m.signOut}</button>
          </div>}
        </div>}
      </header>
      <div className="ops-body">
        <nav className="ops-nav" aria-label={m.consoleName}>
          <div><p className="settings-group-label">{m.navServer}</p><ul><NavLink to="/" path={path} icon={<HomeIcon />}>{m.overview}</NavLink></ul></div>
          <div><p className="settings-group-label">{m.navConfiguration}</p><ul>
            {groupOrder.map((group) => <NavLink key={group} to={`/config/${group}`} path={path} icon={<SettingsIcon />}>{m.groups[group]}</NavLink>)}
          </ul></div>
          <div><p className="settings-group-label">{m.navControl}</p><ul>
            <NavLink to="/requests" path={path} icon={<AlertIcon />} count={waiting}>{m.requests}</NavLink>
            <NavLink to="/history" path={path} icon={<HistoryIcon />}>{m.history}</NavLink>
            <NavLink to="/audit" path={path} icon={<ListIcon />}>{m.audit}</NavLink>
            <NavLink to="/operators" path={path} icon={<UsersIcon />}>{m.operators}</NavLink>
          </ul></div>
        </nav>
        <main id="main" className="ops-main" tabIndex={-1} ref={main} aria-labelledby="page-title"><div className="ops-page">{children}</div></main>
      </div>
    </div>
  );
}
