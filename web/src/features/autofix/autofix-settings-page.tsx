import { useState } from 'react';
import type { ReactNode } from 'react';
import { formatDay, formatShortDate } from '../../lib/format-date';
import { Dialog } from '../../ui/dialog';
import type { AutofixSettings, ConnectorState, FixConnector, GitHubRepo, Installation, Repo } from './autofix-api';
import { StateLabel, OutLink } from './autofix-block';
import { ExternalIcon, KeyIcon } from './autofix-icons';
import { autofixMessages } from './autofix-messages';

const m = autofixMessages.set;
const vendor = autofixMessages.vendor;
const kinds: FixConnector[] = ['claude_issue', 'copilot_issue', 'cursor_api'];

// Every change Settings → Autofix makes. Each answers whether it went through (the caller says why not), so a change
// made of two steps stops at the first that failed.
export interface SettingsActions {
  connectGitHub: () => Promise<boolean>;
  disconnectGitHub: (installation: Installation) => Promise<boolean>;
  connectRepo: (installation: Installation, repo: GitHubRepo) => Promise<boolean>;
  disconnectRepo: (repo: Repo) => Promise<boolean>;
  setDefault: (repo: Repo, connector: FixConnector | null) => Promise<boolean>;
  setNotes: (repo: Repo, on: boolean) => Promise<boolean>;
  enable: (kind: FixConnector) => Promise<boolean>;
  disable: (kind: FixConnector) => Promise<boolean>;
  check: (kind: FixConnector) => Promise<boolean>;
  setKey: (key: string) => Promise<boolean>;
  removeKey: () => Promise<boolean>;
}

export interface AutofixSettingsPageProps {
  load: 'loading' | 'error' | 'ready';
  // The server's GitHub App is set up (GET /api/v1/config); the names it still needs otherwise.
  available: boolean;
  missingSettings: string[];
  isAdmin: boolean;
  settings?: AutofixSettings;
  // The repositories the installation reaches, read from GitHub now.
  githubRepos: { load: 'loading' | 'error' | 'ready'; items: GitHubRepo[]; onRetry: () => void };
  timezone: string;
  now: number;
  actions: SettingsActions;
  // What GitHub's redirect said, or a change that failed.
  notice?: string;
  problem?: string;
  onRetry: () => void;
  // The vendor-side checklist, kept in this browser (checklist-store.ts).
  checklist: (kind: FixConnector) => boolean[];
  onChecklist: (kind: FixConnector, ticks: boolean[]) => void;
}

type Busy = (run: () => Promise<unknown>) => void;

function Confirm({ title, help, children, confirm, danger, onConfirm, onClose, busy }: { title: string; help: string; children?: ReactNode; confirm: string; danger?: boolean; onConfirm: () => void; onClose: () => void; busy: boolean }) {
  return <Dialog alert className="af-dialog" title={title} help={help} closeLabel={autofixMessages.close} onClose={onClose} onSubmit={onConfirm}
    footer={<><button type="button" className="af-ghost" onClick={onClose}>{autofixMessages.cancel}</button>
      <button type="submit" className={danger ? 'af-danger-solid' : 'primary-action'} disabled={busy}>{confirm}</button></>}>{children}</Dialog>;
}

function Perms() {
  return <ul className="af-perms">{m.perms.map(([name, why]) => <li key={name}><code>{name}</code><span>{why}</span></li>)}
    <li><code className="af-muted">{m.notAsked[0]}</code><span>{m.notAsked[1]}</span></li></ul>;
}

function GitHubStep({ installation, isAdmin, at, busy, run, actions, repos }: { installation?: Installation; isAdmin: boolean; at: (value: string) => string; busy: boolean; run: Busy; actions: SettingsActions; repos: number }) {
  const [dialog, setDialog] = useState<'leaving' | 'disconnect'>();
  const leaving = dialog === 'leaving' && <Dialog className="af-dialog" title={m.leavingTitle} help={m.leavingHelp} closeLabel={autofixMessages.close} onClose={() => setDialog(undefined)}
    onSubmit={() => run(actions.connectGitHub)}
    footer={<><button type="button" className="af-ghost" onClick={() => setDialog(undefined)}>{autofixMessages.cancel}</button>
      <button type="submit" className="primary-action" disabled={busy}><ExternalIcon />{m.continueToGitHub}</button></>}>
    <div className="af-cards">
      <div className="af-cardopt af-static"><span className="af-ok-mark" aria-hidden="true">✓</span><b>{m.onlySelect}</b><span>{m.onlySelectHelp}</span></div>
      <div className="af-cardopt af-static af-off"><span className="af-no-mark" aria-hidden="true">✕</span><b>{m.allRepos}</b><span>{m.allReposHelp}</span></div>
    </div>
    <p className="af-help">{m.leavingNote}</p>
  </Dialog>;
  if (!installation) {
    return <section className="af-stepcard panel" aria-labelledby="af-s1"><header><h2 id="af-s1">{m.connectGitHub}</h2><StateLabel tone="off">{m.notConnected}</StateLabel></header>
      <p>{m.connectGitHubHelp}</p><Perms />
      {isAdmin && <div><button type="button" className="primary-action" disabled={busy} onClick={() => setDialog('leaving')}><ExternalIcon />{m.connectGitHub}</button></div>}
      {leaving}
    </section>;
  }
  const blocked = installation.status === 'blocked_all_repositories';
  const suspended = installation.status === 'suspended';
  return <section className="af-stepcard panel" aria-labelledby="af-s1"><header><h2 id="af-s1">{m.github}</h2>
    {blocked ? <StateLabel tone="stopped">{m.blockedAll}</StateLabel> : suspended ? <StateLabel tone="attention">{m.suspended}</StateLabel> : <StateLabel tone="done">{m.connected}</StateLabel>}
    {isAdmin && <div className="af-acts af-head-acts"><OutLink href={installation.settings_url} className="af-btn"><ExternalIcon />{m.changeOnGitHub}</OutLink>
      <button type="button" className="danger-action" disabled={busy} onClick={() => setDialog('disconnect')}>{m.disconnect}</button></div>}</header>
    <div className="af-acct"><span className="af-av" aria-hidden="true">{installation.account_login.slice(0, 2).toLowerCase()}</span>
      <span className="af-who"><b>{installation.account_login}</b><span>{m.account(installation.account_type === 'User' ? 'Personal account' : 'Organization',
        m.selection[installation.repository_selection], installation.connected_by_name, at(installation.connected_at))}</span></span></div>
    {blocked && <div className="af-notice af-danger" role="alert"><span><strong>{m.blockedAllTitle(installation.account_login)}</strong> {m.blockedAllHelp}</span>
      <OutLink href={installation.settings_url} className="af-btn">{m.changeShort}</OutLink></div>}
    {suspended && <div className="af-notice" role="alert"><span><strong>{m.suspendedTitle(installation.account_login)}</strong> {m.suspendedHelp}</span></div>}
    {!blocked && !suspended && <details><summary className="af-help">{m.permsSummary}</summary><Perms /></details>}
    {dialog === 'disconnect' && <Confirm danger title={m.disconnectTitle} help={m.disconnectHelp(installation.account_login, repos)} confirm={m.disconnect} busy={busy}
      onClose={() => setDialog(undefined)} onConfirm={() => run(() => actions.disconnectGitHub(installation))}>
      <div className="af-oc af-note"><span>{m.disconnectNote}</span></div><p className="af-help">{m.disconnectAgain}</p></Confirm>}
  </section>;
}

function RepoRow({ repo, isAdmin, at, busy, run, actions, enabled }: { repo: Repo; isAdmin: boolean; at: (value: string) => string; busy: boolean; run: Busy; actions: SettingsActions; enabled: FixConnector[] }) {
  const [dialog, setDialog] = useState<'notes' | 'disconnect'>();
  const paused = repo.paused_reason !== null;
  const choices = [...new Set([...enabled, ...(repo.default_connector ? [repo.default_connector] : [])])];
  return <li className={paused ? 'af-paused' : undefined}>
    <div className="af-nm"><b>{repo.full_name} <span className="af-badge">{m.visibility[repo.visibility]}</span>
      {paused ? <StateLabel tone="attention">{m.paused}</StateLabel> : <StateLabel tone="done">{m.connected}</StateLabel>}</b>
      <span>{paused ? m.pausedWhy[repo.paused_reason!] : m.connectedBy(repo.connected_by_name, at(repo.connected_at))}</span></div>
    <div className="af-opts">{!paused && <>
      <label className="af-field-sm"><span>{m.defaultAgent}</span>{isAdmin
        ? <select aria-label={m.defaultAgentFor(repo.full_name)} value={repo.default_connector ?? ''} disabled={busy}
          onChange={(event) => run(() => actions.setDefault(repo, (event.target.value || null) as FixConnector | null))}>
          <option value="">{m.noDefault}</option>{choices.map((kind) => <option key={kind} value={kind}>{vendor(kind)}</option>)}</select>
        : <span>{repo.default_connector ? vendor(repo.default_connector) : m.noDefault}</span>}</label>
      <label className="switch af-switch"><input type="checkbox" role="switch" checked={repo.include_notes} disabled={!isAdmin || busy}
        onChange={(event) => (event.target.checked ? setDialog('notes') : run(() => actions.setNotes(repo, false)))} /><span>{m.notesSwitch}</span></label>
    </>}</div>
    <div className="af-end">{isAdmin && (repo.paused_reason === 'removed_from_app' ? null
      : <button type="button" className="af-ghost" disabled={busy} onClick={() => setDialog('disconnect')}>{m.disconnect}</button>)}</div>
    {dialog === 'notes' && <Confirm title={m.notesTitle(repo.full_name)} help={m.notesHelp} confirm={m.allowNotes} busy={busy} onClose={() => setDialog(undefined)}
      onConfirm={() => { setDialog(undefined); run(() => actions.setNotes(repo, true)); }}><p className="af-help">{m.notesNote}</p></Confirm>}
    {dialog === 'disconnect' && <Confirm danger title={m.disconnectRepoTitle(repo.full_name)} help={m.disconnectRepoHelp} confirm={m.disconnect} busy={busy}
      onClose={() => setDialog(undefined)} onConfirm={() => { setDialog(undefined); run(() => actions.disconnectRepo(repo)); }} />}
  </li>;
}

function ReposStep({ settings, installation, githubRepos, isAdmin, at, busy, run, actions }: { settings: AutofixSettings; installation?: Installation; githubRepos: AutofixSettingsPageProps['githubRepos']; isAdmin: boolean; at: (value: string) => string; busy: boolean; run: Busy; actions: SettingsActions }) {
  if (!installation) {
    return <section className="af-stepcard panel af-locked" aria-labelledby="af-s2"><header><h2 id="af-s2">{m.repos}</h2><StateLabel tone="off">{m.afterGitHub}</StateLabel></header><p>{m.reposAfter}</p></section>;
  }
  const blocked = installation.status === 'blocked_all_repositories';
  const suspended = installation.status === 'suspended';
  const connected = settings.repos;
  const live = connected.filter((repo) => repo.paused_reason === null).length;
  const enabled = settings.connectors.filter((item) => item.enabled).map((item) => item.kind);
  const known = new Set(connected.map((repo) => repo.github_repo_id));
  const others = githubRepos.items.filter((repo) => !known.has(repo.github_repo_id) && repo.connected_repo_id === null);
  return <section className="af-stepcard panel" aria-labelledby="af-s2"><header><h2 id="af-s2">{m.repos}</h2>
    {blocked ? <StateLabel tone="off">{m.locked}</StateLabel> : suspended ? <StateLabel tone="attention">{m.allPaused}</StateLabel> : <StateLabel tone={live > 0 ? 'done' : 'off'}>{m.reposConnected(live)}</StateLabel>}</header>
    <p>{blocked ? m.reposBlocked : m.reposHelp}</p>
    {!blocked && <ul className="af-repos">
      {connected.map((repo) => <RepoRow key={repo.id} repo={repo} isAdmin={isAdmin} at={at} busy={busy} run={run} actions={actions} enabled={enabled} />)}
      {others.map((repo) => {
        const grey = repo.visibility !== 'private';
        return <li key={repo.github_repo_id} className={grey ? 'af-grey' : undefined}>
          <div className="af-nm"><b>{repo.full_name} <span className="af-badge">{m.visibility[repo.visibility]}</span></b>
            <span>{repo.visibility === 'public' ? m.cantPublic : repo.visibility === 'internal' ? m.cantInternal : m.canSee}</span></div>
          <div className="af-opts" />
          <div className="af-end">{isAdmin && !grey && <button type="button" disabled={busy} onClick={() => run(() => actions.connectRepo(installation, repo))}>{m.connect}</button>}</div>
        </li>;
      })}
      {githubRepos.load === 'loading' && <li role="status" className="af-repos-note">{m.reposLoading}</li>}
      {githubRepos.load === 'error' && <li role="alert" className="af-repos-note"><span className="error-copy">{m.reposFailed}</span> <button type="button" onClick={githubRepos.onRetry}>{autofixMessages.tryAgain}</button></li>}
      {githubRepos.load === 'ready' && connected.length === 0 && others.length === 0 && <li className="af-repos-note">{m.noRepos}</li>}
    </ul>}
  </section>;
}

function Checklist({ kind, ticks, onTicks, disabled }: { kind: FixConnector; ticks: boolean[]; onTicks: (ticks: boolean[]) => void; disabled: boolean }) {
  const items = m.checklist[kind];
  if (items.length === 0) return null;
  return <div className="af-col"><h4>{m.firstOnGitHub}</h4>
    <ul className="af-checklist">{items.map(([what, help], index) => <li key={what}><input type="checkbox" checked={ticks[index] ?? false} disabled={disabled}
      aria-label={what} onChange={(event) => onTicks(items.map((_, i) => (i === index ? event.target.checked : ticks[i] ?? false)))} />
      <span>{what}{help && <span className="af-help">{help}</span>}</span></li>)}</ul>
    <span className="af-help">{m.checklistLocal}</span></div>;
}

function KeyField({ label, placeholder, onSave, busy, saveLabel }: { label: string; placeholder: string; onSave?: (key: string) => void; busy: boolean; saveLabel?: string }) {
  const [key, setKey] = useState('');
  const [error, setError] = useState<string>();
  return <form className="af-keyrow" onSubmit={(event) => { event.preventDefault(); if (key.trim().length < 8) return setError(m.keyTooShort); setError(undefined); onSave?.(key.trim()); setKey(''); }}>
    <label><span className="sr-only">{label}</span><input type="password" value={key} placeholder={placeholder} autoComplete="off" spellCheck={false}
      aria-invalid={error ? true : undefined} onChange={(event) => setKey(event.target.value)} /></label>
    {onSave && <button type="submit" className="primary-action" disabled={busy || !key}>{saveLabel ?? m.saveKey}</button>}
    {error && <span className="error-copy">{error}</span>}
  </form>;
}

function VendorCard({ state, kind, isAdmin, at, busy, run, actions, ticks, onTicks }: { state?: ConnectorState; kind: FixConnector; isAdmin: boolean; at: (value: string) => string; busy: boolean; run: Busy; actions: SettingsActions; ticks: boolean[]; onTicks: (ticks: boolean[]) => void }) {
  const [setup, setSetup] = useState(false);
  const [agreed, setAgreed] = useState(false);
  const [key, setKey] = useState('');
  const [replacing, setReplacing] = useState(false);
  const enabled = state?.enabled === true;
  const cursor = kind === 'cursor_api';
  const refused = cursor && state?.last_check?.result === 'key_invalid';
  const heading = `af-v-${kind}`;
  if (!enabled) {
    const keyless = cursor && !state?.credential;
    const turnOn = () => run(async () => { if (cursor && key && !(await actions.setKey(key))) return; await actions.enable(kind); });
    return <article className="af-vendor af-open" aria-labelledby={heading}><header><h3 id={heading}>{vendor(kind)}</h3><StateLabel tone="off">{m.off}</StateLabel></header>
      <span className="af-how">{m.how[kind]}</span>
      {isAdmin && <div className="af-cols">
        {cursor ? <div className="af-col"><h4>{m.cursorKey}</h4>
          {state?.credential && !keyless ? <div className="af-keyset"><KeyIcon /><span><b>{m.keySet}</b>{m.keySetBy(at(state.credential.set_at), state.credential.set_by_name)}</span>
            <div className="af-acts"><button type="button" disabled={busy} onClick={() => setReplacing(!replacing)} aria-expanded={replacing}>{m.replace}</button>
              <button type="button" className="af-ghost" disabled={busy} onClick={() => run(actions.removeKey)}>{m.remove}</button></div></div>
            : <label className="af-keyrow"><span className="sr-only">{m.cursorKey}</span><input type="password" value={key} placeholder={m.pasteKey} autoComplete="off" spellCheck={false} onChange={(event) => setKey(event.target.value)} /></label>}
          {state?.credential && replacing && <KeyField label={m.newKey} placeholder={m.pasteNewKey} busy={busy}
            onSave={(value) => { setReplacing(false); run(() => actions.setKey(value)); }} />}
          <span className="af-help">{m.keyAdvice}</span></div>
          : <Checklist kind={kind} ticks={ticks} onTicks={onTicks} disabled={false} />}
        <div className="af-col"><h4>{m.whatLeaves}</h4>
          <label className="af-consent"><input type="checkbox" checked={agreed} onChange={(event) => setAgreed(event.target.checked)} /><span>{m.consent[kind]}</span></label>
          <div className="af-acts"><button type="button" className="primary-action" disabled={busy || !agreed || (keyless && key.trim().length < 8)} onClick={turnOn}>{cursor ? m.saveAndTurnOn : m.turnOn(vendor(kind))}</button></div>
          {!agreed && <span className="af-help">{m.turnOnHelp}</span>}</div>
      </div>}
    </article>;
  }
  const check = state?.last_check;
  return <article className="af-vendor" aria-labelledby={heading}><header><h3 id={heading}>{vendor(kind)}</h3>{refused ? <StateLabel tone="stopped">{m.keyRefused}</StateLabel> : <StateLabel tone="done">{m.on}</StateLabel>}</header>
    <span className="af-how">{m.how[kind]}</span>
    {kind === 'copilot_issue' && state?.mode && <span className="af-help">{m.copilotMode[state.mode]}</span>}
    {check && !refused && <p className={`af-checkres ${check.result === 'ok' ? 'af-good' : 'af-bad'}`}><span>{m.checked(at(check.at))} {check.result === 'ok'
      ? <>{m.checkOk}{!cursor && ` ${m.checkOkIssue}`}</> : check.message ?? m.checkResults[check.result] ?? check.result}</span></p>}
    {cursor && !refused && (state?.credential ? <div className="af-keyset"><KeyIcon /><span><b>{m.keySet}</b>{m.keySetBy(at(state.credential.set_at), state.credential.set_by_name)}</span>
      {isAdmin && <div className="af-acts"><button type="button" disabled={busy} onClick={() => setReplacing(!replacing)} aria-expanded={replacing}>{m.replace}</button>
        <button type="button" className="af-ghost" disabled={busy} onClick={() => run(actions.removeKey)}>{m.remove}</button></div>}</div>
      : <div className="af-oc af-note"><span>{m.keyMissing}</span></div>)}
    {cursor && refused && <div className="af-oc af-danger"><span><b>{m.keyRefusedAt(at(check!.at))}</b></span></div>}
    {cursor && isAdmin && (refused || replacing || !state?.credential) && <KeyField label={m.newKey} placeholder={m.pasteNewKey} busy={busy}
      onSave={(value) => { setReplacing(false); run(() => actions.setKey(value)); }} />}
    {state?.consent && <span className="af-help">{m.agreed(state.consent.name, at(state.consent.at), m.retention[kind])}</span>}
    {setup && <Checklist kind={kind} ticks={ticks} onTicks={onTicks} disabled={!isAdmin} />}
    {isAdmin && <div className="af-acts"><button type="button" disabled={busy} onClick={() => run(() => actions.check(kind))}>{m.check}</button>
      {m.checklist[kind].length > 0 && <button type="button" className="af-ghost" aria-expanded={setup} onClick={() => setSetup(!setup)}>{m.setupSteps}</button>}
      <button type="button" className="af-ghost" disabled={busy} onClick={() => run(() => actions.disable(kind))}>{m.turnOff}</button></div>}
  </article>;
}

// AutofixSettingsPage is Settings → Autofix: one checklist of three steps (GitHub, repositories, agents), each with its
// state and its way back. Admins change it; everyone else reads it.
export function AutofixSettingsPage({ load, available, missingSettings, isAdmin, settings, githubRepos, timezone, now, actions, notice, problem, onRetry, checklist, onChecklist }: AutofixSettingsPageProps) {
  const [busy, setBusy] = useState(false);
  const run: Busy = (task) => { setBusy(true); void task().finally(() => setBusy(false)); };
  const at = (value: string) => (Date.parse(value) > now - 86_400_000 ? formatShortDate(value, timezone, new Date(now)) : formatDay(value, timezone));
  const installation = settings?.installations[0];
  const on = settings?.connectors.filter((item) => item.enabled).length ?? 0;
  return <div className="af-settings">
    <header className="af-page-head"><div><h1>{m.heading}</h1><p>{m.description}</p></div></header>
    {notice && <p role="status" className="af-notice af-ok">{notice}</p>}
    {problem && <p role="alert" className="error-copy">{problem}</p>}
    {!available ? <div className="af-notice" role="note"><span><strong>{m.unavailable}</strong> {m.unavailableHelp(missingSettings.map((name) => name))}</span></div>
      : load === 'loading' ? <p role="status" className="empty-copy">{m.loading}</p>
        : load === 'error' || !settings ? <div className="af-empty panel"><p role="alert" className="error-copy">{m.failed}</p><button type="button" onClick={onRetry}>{autofixMessages.tryAgain}</button></div>
          : <>
            {!isAdmin && <div className="af-notice af-plain" role="note"><span>{m.member}</span></div>}
            <ol className="af-steps3">
              <li><GitHubStep installation={installation} isAdmin={isAdmin} at={at} busy={busy} run={run} actions={actions} repos={settings.repos.length} /></li>
              <li><ReposStep settings={settings} installation={installation} githubRepos={githubRepos} isAdmin={isAdmin} at={at} busy={busy} run={run} actions={actions} /></li>
              <li><section className="af-stepcard panel" aria-labelledby="af-s3"><header><h2 id="af-s3">{m.agents}</h2>
                {installation ? <StateLabel tone={on > 0 ? 'done' : 'off'}>{m.agentsOn(on)}</StateLabel> : <StateLabel tone="off">{m.afterGitHub}</StateLabel>}</header>
                <p>{m.agentsHelp}</p>
                {installation && <div className="af-vendors">{kinds.map((kind) => <VendorCard key={kind} kind={kind} state={settings.connectors.find((item) => item.kind === kind)}
                  isAdmin={isAdmin} at={at} busy={busy} run={run} actions={actions} ticks={checklist(kind)} onTicks={(ticks) => onChecklist(kind, ticks)} />)}</div>}
              </section></li>
            </ol>
          </>}
  </div>;
}
