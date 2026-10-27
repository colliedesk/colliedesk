import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { Link } from '@tanstack/react-router';
import { api, ChangedElsewhereError, InvalidRequestError } from '../../api/client';
import { formatShortDate } from '../../lib/format-date';
import { aiDraftApi, writing } from '../inbox/ai-draft-api';
import { autofixApi, keys } from './autofix-api';
import type { AutofixSettings, Fix, FixDetail, FixSent } from './autofix-api';
import { AutofixBlockView } from './autofix-block';
import type { SendTarget } from './autofix-block';
import { CancelDialog, LinkDialog } from './autofix-dialogs';
import { autofixMessages as m } from './autofix-messages';
import { fixView, safeHref, stripText } from './fix-view';
import type { FixAction, Tone } from './fix-view';
import { SendDialog } from './send-dialog';

// A fix the vendor is moving is read again often; one that waits for a person rarely. Sending is a matter of seconds.
const pollMs = (fix: Fix | null | undefined) => (!fix ? 60_000 : fix.state === 'sending' ? 3_000
  : fix.state === 'sent' || fix.state === 'working' || fix.state === 'pr_open' ? 15_000 : 60_000);

// useConversationFix reads the conversation's latest fix and, once it changed, its timeline.
export function useConversationFix(conversationId: string) {
  const latest = useQuery({
    queryKey: keys.conversation(conversationId),
    queryFn: () => autofixApi.ofConversation(conversationId).then((page) => page.items[0] ?? null),
    refetchInterval: (query) => pollMs(query.state.data),
  });
  const fix = latest.data;
  const detail = useQuery({
    queryKey: [...keys.fix(fix?.id ?? ''), fix?.row_version, fix?.stale],
    queryFn: () => autofixApi.get(fix!.id),
    enabled: Boolean(fix),
    placeholderData: (previous) => (previous?.id === fix?.id ? previous : undefined),
  });
  return { latest, detail };
}

// useAutofixSettings is shared by every Autofix screen; it changes only in Settings → Autofix.
export function useAutofixSettings() {
  return useQuery({ queryKey: keys.settings, queryFn: autofixApi.settings, staleTime: 60_000 });
}

// targetOf is where a new fix goes by default: the first repository that takes fixes, with its default agent when
// that one is on, or the first agent that is.
export function targetOf(settings: AutofixSettings | undefined): SendTarget & { repos: AutofixSettings['repos'] } {
  const repos = (settings?.repos ?? []).filter((repo) => repo.paused_reason === null);
  const on = (settings?.connectors ?? []).filter((item) => item.enabled && (item.kind !== 'cursor_api' || item.credential !== null)).map((item) => item.kind);
  const repo = repos.find((item) => item.default_connector !== null && on.includes(item.default_connector)) ?? repos[0];
  const connector = repo?.default_connector && on.includes(repo.default_connector) ? repo.default_connector : on[0];
  return { repo, connector, repos };
}

// fixReturn reads what GitHub's redirect left in the address (`?fix=<id>` or `?fix_error=<reason>`) and cleans it,
// so a reload does not repeat it.
export function fixReturn(): { error?: string } | undefined {
  const params = new URLSearchParams(window.location.search);
  const done = params.get('fix');
  const error = params.get('fix_error');
  if (done === null && error === null) return undefined;
  params.delete('fix');
  params.delete('fix_error');
  const rest = params.toString();
  window.history.replaceState(window.history.state, '', `${window.location.pathname}${rest ? `?${rest}` : ''}${window.location.hash}`);
  return error === null ? {} : { error: m.returnErrors[error] ?? m.returnUnknown };
}

// goTo leaves for GitHub in this window (never a popup): the person confirms there and comes back to the conversation.
export function goTo(url: string | null | undefined) {
  const safe = safeHref(url);
  if (safe) window.location.assign(safe);
}

const settingsLink = (label: string, primary = false) => <Link to="/settings/autofix" className={primary ? 'af-btn primary-action' : 'af-btn'}>{label}</Link>;
const problemOf = (error: unknown) => (error instanceof ChangedElsewhereError ? m.changedElsewhere : error instanceof InvalidRequestError ? error.message : m.actionFailed);

type Open = { dialog: 'send'; retry?: Fix } | { dialog: 'cancel' | 'link' } | undefined;

interface AutofixConversationProps {
  conversationId: string;
  customerName?: string;
  timezone: string;
}

// AutofixConversation is the Autofix block at the top of a conversation's details, with its dialogs.
export function AutofixConversation({ conversationId, customerName, timezone }: AutofixConversationProps) {
  const client = useQueryClient();
  const { latest, detail } = useConversationFix(conversationId);
  const settings = useAutofixSettings();
  const me = useQuery({ queryKey: ['me'], queryFn: api.me, staleTime: Infinity });
  const [open, setOpen] = useState<Open>();
  const [problem, setProblem] = useState<string>();
  const [notice, setNotice] = useState<string>();
  // Back from GitHub: show the fix as it is now ("Sending" at once), and why it did not go if it did not.
  useEffect(() => {
    const back = fixReturn();
    if (!back) return;
    if (back.error) setProblem(back.error);
    void client.invalidateQueries({ queryKey: keys.conversation(conversationId) });
  }, [client, conversationId]);
  const refresh = () => Promise.all([
    client.invalidateQueries({ queryKey: keys.conversation(conversationId) }),
    client.invalidateQueries({ queryKey: keys.status }),
    client.invalidateQueries({ queryKey: keys.list }),
  ]);
  const fix: FixDetail | null = latest.data === null ? null : detail.data ?? null;
  const act = useMutation({
    mutationFn: async (run: () => Promise<unknown>) => run(),
    onMutate: () => { setProblem(undefined); setNotice(undefined); },
    onSuccess: () => { setOpen(undefined); return refresh(); },
    onError: (error) => { setNotice(undefined); setProblem(problemOf(error)); return refresh(); },
  });
  const draftReply = async (current: Fix) => {
    const released = formatShortDate(current.deployed_at ?? current.state_changed_at, timezone);
    const put = (text: string, aiDraftId?: string) =>
      window.dispatchEvent(new CustomEvent('colliedesk:insert-reply', { detail: { conversationId, text, ...(aiDraftId ? { aiDraftId } : {}) } }));
    if (!me.data?.ai_features.draft) {
      put(m.draftPlain(customerName));
      setNotice(m.draftInserted);
      return;
    }
    setNotice(m.draftStarted);
    let draft = await aiDraftApi.create(conversationId, m.draftPrompt(released));
    for (let tries = 0; writing(draft) && tries < 6; tries++) draft = await aiDraftApi.get(conversationId, draft.id, 20);
    if (draft.status !== 'ready' || !draft.text) throw new Error('no draft');
    put(draft.text, draft.id);
    setNotice(m.draftReady);
    void client.invalidateQueries({ queryKey: ['ai-drafts', conversationId] });
  };
  const onAction = (action: FixAction) => {
    if (!fix) return;
    switch (action) {
      case 'cancel': setProblem(undefined); return setOpen({ dialog: 'cancel' });
      case 'link_pr': setProblem(undefined); return setOpen({ dialog: 'link' });
      case 'retry': case 'send_again': setProblem(undefined); return setOpen({ dialog: 'send', retry: fix });
      case 'unlink_pr': return act.mutate(() => autofixApi.unlinkPR(fix));
      case 'mark_released': return act.mutate(() => autofixApi.markDeployed(fix));
      case 'unmark_released': return act.mutate(() => autofixApi.unmarkDeployed(fix));
      case 'draft_reply': return act.mutate(() => draftReply(fix), { onSuccess: () => undefined });
      default: return undefined;
    }
  };
  const onSent = (sent: FixSent) => {
    if (sent.authorize_url) return goTo(sent.authorize_url);
    setOpen(undefined);
    void refresh();
  };
  const target = targetOf(settings.data);
  const load = latest.isPending || (latest.data && detail.isPending) || settings.isPending ? 'loading'
    : latest.isError || detail.isError || settings.isError ? 'error' : 'ready';
  return <>
    <AutofixBlockView load={load} fix={fix} target={target} customerName={customerName} timezone={timezone} now={Date.now()}
      busy={act.isPending} problem={open ? undefined : problem} notice={notice} settingsLink={settingsLink}
      onSend={() => { setProblem(undefined); setOpen({ dialog: 'send' }); }} onAction={onAction}
      onRetryLoad={() => { void latest.refetch(); void detail.refetch(); void settings.refetch(); }} />
    {open?.dialog === 'send' && settings.data && <SendDialog conversationId={conversationId} repos={target.repos} connectors={settings.data.connectors}
      initialRepo={open.retry?.repo_id ?? target.repo?.id} initialConnector={open.retry?.connector ?? target.connector} retryOf={open.retry} timezone={timezone}
      onClose={() => setOpen(undefined)} onSent={onSent} />}
    {open?.dialog === 'cancel' && fix && <CancelDialog fix={fix} busy={act.isPending} problem={problem} onClose={() => setOpen(undefined)}
      onCancel={() => act.mutate(() => autofixApi.cancel(fix))} />}
    {open?.dialog === 'link' && fix && <LinkDialog fix={fix} busy={act.isPending} problem={problem} onClose={() => setOpen(undefined)}
      onLink={(url) => act.mutate(() => autofixApi.linkPR(fix, url))} />}
  </>;
}

const stripTone: Record<Tone, string> = { off: 'af-st-off', running: 'af-st-open', attention: 'af-st-warn', done: 'af-st-ok', stopped: 'af-st-err' };

// AutofixStrip is the one line under the thread header on narrow screens, where the details hide behind a toggle.
export function AutofixStrip({ conversationId, timezone, customerName, onOpenDetails }: { conversationId: string; timezone: string; customerName?: string; onOpenDetails: () => void }) {
  const { latest } = useConversationFix(conversationId);
  const settings = useAutofixSettings();
  if (latest.data === undefined || (latest.data === null && !targetOf(settings.data).repo)) return null;
  const fix = latest.data;
  const now = Date.now();
  const view = fix && fixView(fix, now);
  const tone: Tone = view?.tone ?? 'off';
  const text = fix ? stripText(fix, now, (value) => formatShortDate(value, timezone, new Date(now)), customerName) : m.strip.none(targetOf(settings.data).repo?.full_name);
  return <button type="button" className="af-strip" onClick={onOpenDetails} aria-controls="conversation-details" aria-label={`${m.openDetails}: ${text}`}>
    <b>{m.heading}</b><span className="af-strip-text">{text}</span><span className={`status ${stripTone[tone]}`}>{m.stripTone[tone]}</span>
  </button>;
}
