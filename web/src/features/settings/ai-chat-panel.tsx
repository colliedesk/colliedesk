import { useId, useState } from 'react';
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { aiApi } from '../../api/ai';
import type { AIChatSession, AIChatStatus, AIChatSummary, AIChatTurn, AIChatUsage, AISlot } from '../../api/ai';
import { formatDate, formatDay } from '../../lib/format-date';
import { Dialog } from '../../ui/dialog';
import { panelID, tabID, Tabs } from '../../ui/tabs';
import { aiChatMessages as m } from './ai-chat-messages';
import './ai-settings.css';
import './ai-chat.css';

// The team's side of the help center's AI chat: the month's use (admins), the chats and a chat's
// transcript. Every member reads chats; only an admin deletes one.

const money = (micro: number) => `$${(micro / 1e6).toFixed(2)}`;
const statusClass: Record<AIChatStatus, string> = { open: 'status-open', handed_off: 'status-pending', ended: 'status-closed' };
// A 404 fails a request as Error('API 404') (api/client.ts).
const missing = (error: unknown) => error instanceof Error && error.message === 'API 404';

// A turn's status: answered is resolved; the others are not waiting on anyone, so they stay neutral.
const turnClass = (status: AIChatTurn['status']) => (status === 'answered' ? 'status-resolved' : 'status-closed');

function Bar({ used, limit, label, text }: { used: number; limit: number; label: string; text: string }) {
  const percent = limit > 0 ? Math.min(100, Math.round((used / limit) * 100)) : 100;
  return <div className={`ai-bar${percent >= 100 ? ' ai-bar-full' : ''}`} role="meter" aria-label={label} aria-valuemin={0} aria-valuemax={limit}
    aria-valuenow={Math.min(used, limit)} aria-valuetext={text}><i style={{ width: `${percent}%` }} /></div>;
}

// Notices say what stops the chat from answering, first the one a person can fix at once.
function Notices({ usage }: { usage: AIChatUsage }) {
  const resets = formatDay(usage.resets_at, 'UTC');
  const full = (usage.answer_limit !== null && usage.answers >= usage.answer_limit) || (usage.spent_percent !== null && usage.spent_percent >= 100);
  const near = !full && ((usage.answer_limit !== null && usage.answers >= usage.answer_limit * 0.8) || (usage.spent_percent !== null && usage.spent_percent >= 80));
  const planned = usage.answer_limit !== null;
  return <>
    {usage.articles === 0 && <p className="notice notice-plain" role="status"><span>{m.noArticles}</span> <a href="/knowledge">{m.openKnowledge}</a></p>}
    {full && <p className="notice notice-danger" role="alert"><span>{m.paused(resets)}</span>{planned && <> <a href="/settings/billing">{m.changePlan}</a></>}</p>}
    {near && <p className="notice notice-plain" role="status"><span>{m.nearLimit}</span>{planned && <> <a href="/settings/billing">{m.changePlan}</a></>}</p>}
  </>;
}

// CapForm sets the AI chat feature's own monthly spend limit, on a provider the workspace pays for.
function CapForm({ slot, onSave }: { slot: AISlot; onSave: (cap: number | null) => Promise<unknown> }) {
  const [value, setValue] = useState(slot.monthly_cap_micro_usd === null ? '' : (slot.monthly_cap_micro_usd / 1e6).toFixed(2));
  const [state, setState] = useState<'idle' | 'saving' | 'saved' | 'invalid' | 'failed'>('idle');
  const id = useId();
  const save = () => {
    const text = value.trim();
    const dollars = Number(text);
    if (text !== '' && !(Number.isFinite(dollars) && dollars > 0)) { setState('invalid'); return; }
    setState('saving');
    onSave(text === '' ? null : Math.round(dollars * 1e6)).then(() => setState('saved'), () => setState('failed'));
  };
  return <form className="aic-cap" onSubmit={(event) => { event.preventDefault(); if (state !== 'saving') save(); }}>
    <label className="field" htmlFor={id}><span>{m.capLabel}</span>
      <input id={id} inputMode="decimal" value={value} aria-invalid={state === 'invalid' ? true : undefined} aria-describedby={`${id}-hint`}
        onChange={(event) => { setValue(event.target.value); setState('idle'); }} /></label>
    <small id={`${id}-hint`} className="muted-copy">{state === 'invalid' ? m.capInvalid : m.capHint}</small>
    <span className="button-row"><button type="submit" disabled={state === 'saving'}>{state === 'saving' ? m.capSaving : m.capSave}</button>
      {state === 'saved' && <span role="status" className="muted-copy">{m.capSaved}</span>}
      {state === 'failed' && <span role="alert" className="error-copy">{m.capFailed}</span>}</span>
  </form>;
}

function Month({ usage, slot, onSetCap }: { usage: AIChatUsage; slot?: AISlot; onSetCap?: (cap: number | null) => Promise<unknown> }) {
  const resets = formatDay(usage.resets_at, 'UTC');
  const answers = usage.answer_limit === null ? usage.answers.toLocaleString() : m.answersOf(usage.answers, usage.answer_limit);
  const spent = usage.spent_micro_usd === null ? undefined : m.spent(money(usage.spent_micro_usd), usage.limit_micro_usd === null ? undefined : money(usage.limit_micro_usd));
  return <section className="panel aic-month" aria-label={m.month}>
    <div className="ai-meter">
      <div className="ai-meter-top"><strong>{m.answers}</strong><span>{m.today(usage.answers_today, usage.day_answer_limit)}</span></div>
      <span className="aic-figure">{answers}</span>
      {usage.answer_limit !== null && <Bar used={usage.answers} limit={usage.answer_limit} label={m.answers} text={answers} />}
    </div>
    <div className="ai-meter">
      <div className="ai-meter-top"><strong>{m.budget}</strong><span>{m.resets(resets)}</span></div>
      <span className="aic-figure">{usage.spent_micro_usd === null ? (usage.spent_percent === null ? '—' : m.budgetShare(usage.spent_percent)) : spent}</span>
      {usage.spent_percent !== null && <Bar used={usage.spent_percent} limit={100} label={m.budget} text={m.budgetShare(usage.spent_percent)} />}
      {usage.spent_percent === null && <span className="ai-meter-left">{m.noLimit}</span>}
      {usage.answer_limit === null && slot && onSetCap && <CapForm key={slot.monthly_cap_micro_usd ?? 'none'} slot={slot} onSave={onSetCap} />}
    </div>
    <div className="ai-meter">
      <div className="ai-meter-top"><strong>{m.chats}</strong></div>
      <span className="aic-figure">{usage.chats.toLocaleString()}</span>
      <span className="ai-meter-left">{m.chatsLine(usage.handed_off, usage.rated_helpful, usage.rated)}</span>
    </div>
  </section>;
}

type ChatTab = 'all' | AIChatStatus;

// ChatList is the chats, newest activity first, as a table on a wide panel and cards on a narrow one.
export function ChatList({ timezone, onOpen }: { timezone: string; onOpen: (id: string) => void }) {
  const [tab, setTab] = useState<ChatTab>('all');
  const [notHelpful, setNotHelpful] = useState(false);
  const prefix = useId();
  const status = tab === 'all' ? undefined : tab;
  const chats = useInfiniteQuery({
    queryKey: ['ai-chats', status ?? 'all', notHelpful],
    queryFn: ({ pageParam }) => aiApi.chatSessions({ status, notHelpful, cursor: pageParam }),
    initialPageParam: '',
    getNextPageParam: (page) => page.next_cursor ?? undefined,
  });
  const items = chats.data?.pages.flatMap((page) => page.items) ?? [];
  const filtered = tab !== 'all' || notHelpful;
  const tabs: { id: ChatTab; label: string }[] = [{ id: 'all', label: m.all }, { id: 'open', label: m.status.open },
    { id: 'handed_off', label: m.status.handed_off }, { id: 'ended', label: m.status.ended }];
  return <section className="panel aic-chats" aria-labelledby={`${prefix}-heading`}>
    <div className="aic-chats-head">
      <h2 id={`${prefix}-heading`}>{m.list}</h2>
      <Tabs label={m.filters} tabs={tabs} value={tab} onChange={setTab} prefix={prefix} />
      <label className="field-inline"><input type="checkbox" checked={notHelpful} onChange={(event) => setNotHelpful(event.target.checked)} /><span>{m.onlyNotHelpful}</span></label>
    </div>
    <div role="tabpanel" id={panelID(prefix, tab)} aria-labelledby={tabID(prefix, tab)} aria-busy={chats.isPending}>
      {chats.isError && <p role="alert" className="error-copy">{m.listFailed}</p>}
      {chats.isSuccess && items.length === 0 && <p className="muted-copy aic-empty">{filtered ? m.noMatch : m.noChats}</p>}
      {items.length > 0 && <table className="aic-table">
        <thead><tr><th scope="col">{m.columnLast}</th><th scope="col">{m.columnQuestion}</th><th scope="col">{m.columnLength}</th>
          <th scope="col">{m.columnStatus}</th><th scope="col">{m.columnStarted}</th><th scope="col">{m.columnFeedback}</th></tr></thead>
        <tbody>{items.map((chat) => <ChatRow key={chat.id} chat={chat} timezone={timezone} onOpen={onOpen} />)}</tbody>
      </table>}
      {chats.hasNextPage && <span className="button-row aic-more"><button type="button" disabled={chats.isFetchingNextPage} onClick={() => void chats.fetchNextPage()}>{m.older}</button></span>}
    </div>
  </section>;
}

function ChatRow({ chat, timezone, onOpen }: { chat: AIChatSummary; timezone: string; onOpen: (id: string) => void }) {
  const href = `#chat-${chat.id}`;
  return <tr>
    <td data-label={m.columnLast}>{formatDate(chat.last_turn_at, timezone)}</td>
    <td data-label={m.columnQuestion} className="aic-question">
      <a href={href} onClick={(event) => { event.preventDefault(); onOpen(chat.id); }}>{chat.first_question ?? m.expired}</a></td>
    <td data-label={m.columnLength}>{m.questions(chat.turns)}</td>
    <td data-label={m.columnStatus}><span className={`status ${statusClass[chat.status]}`}>{m.status[chat.status]}</span></td>
    <td data-label={m.columnStarted}>{m.surface[chat.surface]}<small className="muted-copy"> · {chat.channel_name}</small></td>
    <td data-label={m.columnFeedback}>{chat.not_helpful ? m.notHelpful : '—'}</td>
  </tr>;
}

// AIChatPanel is the AI chat tab of Settings → Built-in AI. slot is the AI chat feature, whose own monthly
// limit onSetCap changes where the plan sets none.
export function AIChatPanel({ timezone, onOpen, slot, onSetCap }: { timezone: string; onOpen: (id: string) => void; slot?: AISlot; onSetCap?: (cap: number | null) => Promise<unknown> }) {
  const client = useQueryClient();
  const usage = useQuery({ queryKey: ['ai-chat-usage'], queryFn: aiApi.chatUsage });
  const setCap = onSetCap && ((cap: number | null) => onSetCap(cap).then(() => client.invalidateQueries({ queryKey: ['ai-chat-usage'] })));
  return <>
    {usage.data && <Notices usage={usage.data} />}
    {usage.isError && <p role="alert" className="error-copy">{m.usageFailed}</p>}
    {usage.data ? <Month usage={usage.data} slot={slot} onSetCap={setCap} /> : !usage.isError && <section className="panel aic-month" aria-busy="true" aria-label={m.month} />}
    <ChatList timezone={timezone} onOpen={onOpen} />
  </>;
}

function Turn({ turn, timezone }: { turn: AIChatTurn; timezone: string }) {
  return <li className="aic-turn">
    <div className="aic-said"><b>{m.customer}</b><p>{turn.question ?? m.textGone}</p></div>
    {turn.answer !== null && <div className="aic-said"><b>{m.ai}</b><p>{turn.answer}</p></div>}
    {turn.sources.length > 0 && <ul className="aic-sources" aria-label={m.sources}>{turn.sources.map((source) =>
      <li key={source.ordinal}>{source.folder && <small>{source.folder}</small>}{source.title ?? `#${source.number}`}</li>)}</ul>}
    <div className="aic-turn-foot"><span className={`status ${turnClass(turn.status)}`}>{m.turnStatus[turn.status]}</span>
      {turn.feedback && <span>{m.feedback[turn.feedback]}</span>}<span>{formatDate(turn.created_at, timezone)}</span></div>
  </li>;
}

// ChatTranscript is one chat; canDelete adds Delete chat (admins).
export function ChatTranscript({ id, timezone, canDelete, back, backHref, onBack }: {
  id: string; timezone: string; canDelete: boolean; back: string; backHref: string; onBack: () => void;
}) {
  const client = useQueryClient();
  const chat = useQuery({ queryKey: ['ai-chat', id], queryFn: () => aiApi.chatSession(id), retry: (count, error) => !missing(error) && count < 2 });
  const [confirm, setConfirm] = useState(false);
  const remove = useMutation({
    mutationFn: () => aiApi.deleteChatSession(id),
    onSuccess: () => { void client.invalidateQueries({ queryKey: ['ai-chats'] }); void client.invalidateQueries({ queryKey: ['ai-chat-usage'] }); onBack(); },
  });
  const backLink = <a className="ch-back" href={backHref} onClick={(event) => { event.preventDefault(); onBack(); }}>← {back}</a>;
  if (chat.isError) return <div className="ai-panel-stack">{backLink}<p role="alert" className="error-copy">{missing(chat.error) ? m.notFound : m.loadFailed}</p></div>;
  if (!chat.data) return <div className="ai-panel-stack" aria-busy="true">{backLink}</div>;
  const data: AIChatSession = chat.data;
  return <div className="ai-panel-stack">
    {backLink}
    <section className="settings-card ch-detail aic-transcript" aria-labelledby="aic-title">
      <header className="aic-transcript-head">
        <div><h2 id="aic-title">{m.title(formatDate(data.created_at, timezone))}</h2>
          <p className="muted-copy"><span className={`status ${statusClass[data.status]}`}>{m.status[data.status]}</span> · {m.surface[data.surface]} · {data.channel_name} · {m.questions(data.turns)} · {m.keptUntil(formatDay(data.kept_until, timezone))}</p></div>
        <span className="button-row">
          {data.conversation_id && <a className="button" href={`/conversations/${data.conversation_id}`}>{m.openConversation}</a>}
          {canDelete && <button type="button" className="danger-action" onClick={() => setConfirm(true)}>{m.delete}</button>}
        </span>
      </header>
      {data.history_cut_at && <p className="muted-copy">{m.historyCut(formatDate(data.history_cut_at, timezone))}</p>}
      <ol className="aic-turns">{data.transcript.map((turn) => <Turn key={turn.id} turn={turn} timezone={timezone} />)}</ol>
      {data.status === 'handed_off' && <p className="muted-copy">{m.handedOff}</p>}
    </section>
    {confirm && <Dialog title={m.deleteTitle} help={m.deleteBody(data.conversation_id !== null)} closeLabel={m.cancel} alert onClose={() => setConfirm(false)}
      onSubmit={() => { if (!remove.isPending) remove.mutate(); }}
      footer={<><button type="button" onClick={() => setConfirm(false)} autoFocus>{m.cancel}</button>
        <button type="submit" className="danger-action" disabled={remove.isPending}>{remove.isPending ? m.deleting : m.delete}</button></>}>
      {remove.isError && <p role="alert" className="error-copy">{m.deleteFailed}</p>}
    </Dialog>}
  </div>;
}
