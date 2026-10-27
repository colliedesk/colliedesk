import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { api, InvalidRequestError } from '../../api/client';
import { SettingsNav } from '../settings/settings-nav';
import { autofixApi, keys } from './autofix-api';
import type { FixConnector } from './autofix-api';
import { goTo, useAutofixSettings } from './autofix-conversation';
import { autofixMessages } from './autofix-messages';
import { AutofixSettingsPage } from './autofix-settings-page';
import type { SettingsActions } from './autofix-settings-page';

// The vendor-side checklist (Claude workflow, Copilot turned on) is the admin's own ticks: Colliedesk can't see those
// from its app, so they stay in this browser, per workspace (decided for the first release).
const checklistKey = (workspace: string, kind: FixConnector) => `colliedesk:autofix-checklist:${workspace}:${kind}`;
export function readChecklist(workspace: string, kind: FixConnector): boolean[] {
  try {
    const value: unknown = JSON.parse(window.localStorage.getItem(checklistKey(workspace, kind)) ?? '[]');
    return Array.isArray(value) ? value.map((item) => item === true) : [];
  } catch {
    return [];
  }
}
function writeChecklist(workspace: string, kind: FixConnector, ticks: boolean[]) {
  try {
    window.localStorage.setItem(checklistKey(workspace, kind), JSON.stringify(ticks));
  } catch {
    // A browser without storage keeps the ticks for this page only.
  }
}

// githubReturn reads the outcome of installing the App (`?github=connected` or `?github_error=<reason>`) and cleans
// the address, so a reload does not repeat it.
function githubReturn(): { notice?: string; problem?: string } | undefined {
  const params = new URLSearchParams(window.location.search);
  const done = params.get('github');
  const error = params.get('github_error');
  if (done === null && error === null) return undefined;
  params.delete('github');
  params.delete('github_error');
  const rest = params.toString();
  window.history.replaceState(window.history.state, '', `${window.location.pathname}${rest ? `?${rest}` : ''}${window.location.hash}`);
  return error === null ? { notice: autofixMessages.set.githubReturn.connected } : { problem: autofixMessages.returnErrors[error] ?? autofixMessages.returnUnknown };
}

export function AutofixSettingsRoute() {
  const client = useQueryClient();
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const config = useQuery({ queryKey: ['public-config'], queryFn: api.config });
  const available = config.data?.autofix.available ?? true;
  const settings = useAutofixSettings();
  const installation = settings.data?.installations[0];
  const github = useQuery({
    queryKey: ['autofix-github-repos', installation?.id],
    queryFn: () => autofixApi.githubRepos(installation!.id),
    enabled: Boolean(installation && installation.status === 'active'),
    staleTime: 30_000,
  });
  const [back] = useState(githubReturn);
  const [problem, setProblem] = useState(back?.problem);
  const [ticks, setTicks] = useState<Partial<Record<FixConnector, boolean[]>>>({});
  const workspace = me.data?.workspace.id ?? '';
  useEffect(() => { if (back) void client.invalidateQueries({ queryKey: keys.settings }); }, [back, client]);
  // Every change refreshes what it touches; a refusal is said in words and nothing else moves.
  const change = (task: () => Promise<unknown>) => async () => {
    setProblem(undefined);
    let done = true;
    try {
      await task();
    } catch (error) {
      done = false;
      setProblem(error instanceof InvalidRequestError ? error.message : autofixMessages.actionFailed);
    }
    await Promise.all([client.invalidateQueries({ queryKey: keys.settings }), client.invalidateQueries({ queryKey: ['autofix-github-repos'] }),
      client.invalidateQueries({ queryKey: keys.status })]);
    return done;
  };
  const actions: SettingsActions = {
    connectGitHub: () => change(async () => goTo((await autofixApi.connectGitHub()).authorize_url))(),
    disconnectGitHub: (item) => change(() => autofixApi.disconnectGitHub(item.id))(),
    connectRepo: (item, repo) => change(() => autofixApi.connectRepo(item.id, repo.github_repo_id))(),
    disconnectRepo: (repo) => change(() => autofixApi.disconnectRepo(repo.id))(),
    setDefault: (repo, connector) => change(() => autofixApi.setDefaultConnector(repo.id, connector))(),
    setNotes: (repo, on) => change(() => autofixApi.includeNotes(repo.id, on))(),
    enable: (kind) => change(() => autofixApi.enableConnector(kind, settings.data?.consent_version ?? ''))(),
    disable: (kind) => change(() => autofixApi.disableConnector(kind))(),
    check: (kind) => change(() => autofixApi.checkConnector(kind))(),
    setKey: (key) => change(() => autofixApi.setCursorKey(key))(),
    removeKey: () => change(() => autofixApi.removeCursorKey())(),
  };
  if (!me.data) return <main className="settings-page" aria-busy="true" />;
  const isAdmin = me.data.agent.role === 'admin';
  return <main className="settings-page panel">
    <div className="settings-layout">
      <SettingsNav page="/settings/autofix" shown={{ general: isAdmin, team: isAdmin, channels: true, savedReplies: true, exportData: isAdmin,
        billing: isAdmin && config.data?.billing.enabled === true, importData: isAdmin, ai: true, autofix: true }} />
      <AutofixSettingsPage load={settings.isPending ? 'loading' : settings.isError ? 'error' : 'ready'} available={available}
        missingSettings={config.data?.autofix.missing_settings ?? []} isAdmin={isAdmin} settings={settings.data}
        githubRepos={{ load: installation?.status !== 'active' ? 'ready' : github.isPending ? 'loading' : github.isError ? 'error' : 'ready', items: github.data?.items ?? [], onRetry: () => void github.refetch() }}
        timezone={me.data.timezone ?? 'UTC'} now={Date.now()} actions={actions} notice={back?.notice} problem={problem} onRetry={() => void settings.refetch()}
        checklist={(kind) => ticks[kind] ?? readChecklist(workspace, kind)}
        onChecklist={(kind, next) => { setTicks({ ...ticks, [kind]: next }); writeChecklist(workspace, kind, next); }} />
    </div>
  </main>;
}
