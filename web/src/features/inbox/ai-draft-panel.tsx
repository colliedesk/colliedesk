import { useEffect, useRef, useState } from 'react';
import type { KeyboardEvent as ReactKeyboardEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { PlanLimitError } from '../../api/client';
import { aiDraftApi, writing } from './ai-draft-api';
import type { AIDraft } from './ai-draft-api';
import { aiDraftMessages as text } from './ai-draft-messages';
import { BookIcon, InboxIcon, SparklesIcon, XIcon } from '../../ui/icons';
import './ai-draft.css';

export type DraftUse = 'replace' | 'below';

interface AIDraftPanelProps {
  conversationId: string;
  // The person's own drafts are theirs to pick up again; a teammate's are not shown.
  agentId?: string;
  // Whether the reply box holds text: then the draft can replace it or go below it.
  hasText: boolean;
  // An admin is offered Settings → AI when a draft fails for a setting; others are told to ask one.
  canManage?: boolean;
  // Told whether a draft is being written, so the button that opens the panel can say so.
  onWriting?: (writing: boolean) => void;
  onUse: (draftText: string, use: DraftUse, draftId: string, warnings: string[]) => void;
  onClose: () => void;
}

const longPoll = 20;

// The newest draft of this person that is still being written or waits for a decision.
function openDraft(drafts: AIDraft[] | undefined, agentId?: string) {
  return drafts?.find((draft) => draft.requested_by_agent_id === agentId && (writing(draft) || (draft.status === 'ready' && draft.outcome === null)));
}

// DraftStatus says on the closed panel's button that the person's draft is being written or is ready,
// after the conversation is on screen (the chunk loads after it; nothing waits for this read).
export function DraftStatus({ conversationId, agentId }: { conversationId: string; agentId?: string }) {
  const list = useQuery({ queryKey: ['ai-drafts', conversationId], queryFn: () => aiDraftApi.list(conversationId),
    refetchInterval: (query) => (query.state.status !== 'error' && openDraft(query.state.data?.items, agentId) && writing(openDraft(query.state.data?.items, agentId)) ? 5000 : false) });
  const found = openDraft(list.data?.items, agentId);
  if (!found) return null;
  return <span className="ai-draft-badge" role="status"><span className="sr-only">, </span>{writing(found) ? text.badgeWriting : text.badgeReady}</span>;
}

// AIDraftPanel asks for an AI draft of a reply and shows it when ready: the text, what it is missing,
// what to check before sending, and its sources. It never sends; the person puts the text in the box.
export function AIDraftPanel({ conversationId, agentId, hasText, canManage = false, onUse, onClose, onWriting }: AIDraftPanelProps) {
  const client = useQueryClient();
  const list = useQuery({ queryKey: ['ai-drafts', conversationId], queryFn: () => aiDraftApi.list(conversationId) });
  const [activeId, setActiveId] = useState<string>();
  const [asking, setAsking] = useState(false);
  const [prompt, setPrompt] = useState('');
  const found = openDraft(list.data?.items, agentId);
  const id = asking ? undefined : activeId ?? found?.id;
  const initial = list.data?.items.find((draft) => draft.id === id);
  // While a draft is written, one request waits for it (up to 20 s) and the next starts when it returns.
  const current = useQuery({
    queryKey: ['ai-draft', conversationId, id], enabled: Boolean(id),
    queryFn: () => aiDraftApi.get(conversationId, id!, writing(client.getQueryData<AIDraft>(['ai-draft', conversationId, id]) ?? initial) ? longPoll : 0),
    initialData: initial, // A failed read stops the loop: Retry is the query's own, and a refused read must not repeat at once.
    refetchInterval: (query) => (query.state.status === 'error' ? false : writing(query.state.data) ? 1 : false), refetchOnWindowFocus: false,
  });
  const draft = id ? current.data : undefined;
  // The list (which the button's badge reads) follows the draft as it changes.
  useEffect(() => {
    if (!current.data) return;
    const changed = current.data;
    client.setQueryData<{ items: AIDraft[] }>(['ai-drafts', conversationId], (list) =>
      list && { items: list.items.map((item) => (item.id === changed.id ? changed : item)) });
  }, [client, conversationId, current.data]);
  const busyWriting = writing(draft);
  useEffect(() => { onWriting?.(busyWriting); }, [busyWriting, onWriting]);
  useEffect(() => () => onWriting?.(false), [onWriting]);
  const ask = useMutation({
    mutationFn: (request: string) => aiDraftApi.create(conversationId, request.trim()),
    onSuccess: (created) => {
      client.setQueryData(['ai-draft', conversationId, created.id], created);
      setActiveId(created.id);
      setAsking(false);
      void client.invalidateQueries({ queryKey: ['ai-drafts', conversationId] });
    },
  });
  const discard = useMutation({
    mutationFn: (target: AIDraft) => aiDraftApi.discard(conversationId, target.id),
    onSuccess: (changed) => {
      client.setQueryData(['ai-draft', conversationId, changed.id], changed);
      void client.invalidateQueries({ queryKey: ['ai-drafts', conversationId] });
      setActiveId(undefined);
      setAsking(true);
    },
  });
  const root = useRef<HTMLElement>(null);
  const field = useRef<HTMLTextAreaElement>(null);
  const showForm = !id && !list.isPending;
  useEffect(() => { if (showForm) field.current?.focus(); }, [showForm]);
  const onKey = (event: ReactKeyboardEvent) => {
    if (event.key === 'Escape') { event.preventDefault(); onClose(); }
  };
  const onPromptKey = (event: ReactKeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) { event.preventDefault(); if (!ask.isPending) ask.mutate(prompt); }
  };
  const again = () => {
    setPrompt(draft?.prompt ?? prompt);
    setActiveId(undefined);
    setAsking(true);
  };
  const use = (how: DraftUse) => {
    if (draft?.text) onUse(draft.text, how, draft.id, draft.warnings.map((warning) => warning.value));
  };
  return (
    <section className="ai-draft" aria-label={text.heading} ref={root} onKeyDown={onKey}>
      <header className="ai-draft-head">
        <SparklesIcon /><h3>{text.heading}</h3>
        <button type="button" className="icon-button" aria-label={text.close} title={text.close} onClick={onClose}><XIcon /></button>
      </header>
      {list.isError && !id && <p role="alert" className="ai-draft-problem">{text.loadFailed}</p>}
      {current.isError && id && <p role="alert" className="ai-draft-problem">{text.loadFailed}</p>}
      {showForm && (
        <form className="ai-draft-form" onSubmit={(event) => { event.preventDefault(); ask.mutate(prompt); }}>
          <label htmlFor={`ai-draft-prompt-${conversationId}`}>{text.promptLabel}</label>
          <textarea id={`ai-draft-prompt-${conversationId}`} ref={field} rows={2} maxLength={2000} value={prompt} placeholder={text.promptPlaceholder}
            onChange={(event) => setPrompt(event.target.value)} onKeyDown={onPromptKey} />
          <div className="ai-draft-row">
            <span className="ai-draft-count" aria-hidden="true">{text.promptCount(prompt.length)}</span>
            <button type="submit" className="primary-action" disabled={ask.isPending}>{text.write}</button>
          </div>
          {ask.isError && <p role="alert" className="ai-draft-problem">{ask.error instanceof Error && ask.error.message ? ask.error.message : text.failed(null)}
            {' '}<SettingsWay code={refusalCode(ask.error)} canManage={canManage} /></p>}
        </form>
      )}
      {draft && writing(draft) && (
        <div className="ai-draft-row ai-draft-writing">
          <span role="status"><span className="ai-draft-pulse" aria-hidden="true" />{text.writing}</span>
          <button type="button" disabled={discard.isPending} onClick={() => discard.mutate(draft)}>{text.stop}</button>
        </div>
      )}
      {draft && (draft.status === 'failed' || draft.status === 'cancelled' || (draft.status === 'ready' && draft.outcome !== null)) && (
        <div className="ai-draft-row">
          <p role="alert" className="ai-draft-problem">{text.failed(draft.status === 'cancelled' ? draft.error_code ?? 'cancelled' : draft.error_code)}
            {' '}<SettingsWay code={draft.error_code} canManage={canManage} /></p>
          {/* Try again asks at once with the same prompt; New draft lets the person change it first. */}
          <button type="button" disabled={ask.isPending} onClick={() => { setPrompt(draft.prompt); ask.mutate(draft.prompt); }}>{text.tryAgain}</button>
          <button type="button" onClick={again}>{text.again}</button>
        </div>
      )}
      {draft?.status === 'ready' && draft.outcome === null && draft.text && <ReadyDraft draft={draft} hasText={hasText} onUse={use} onDiscard={() => discard.mutate(draft)} onAgain={again} busy={discard.isPending} />}
    </section>
  );
}

function ReadyDraft({ draft, hasText, onUse, onDiscard, onAgain, busy }: { draft: AIDraft; hasText: boolean; onUse: (how: DraftUse) => void; onDiscard: () => void; onAgain: () => void; busy: boolean }) {
  return (
    <div className="ai-draft-ready">
      {draft.warnings.length > 0 && (
        <div className="ai-draft-check" role="alert">
          <strong>{text.checkHeading}</strong>
          <span>{text.checkBody}</span>
          <ul>{draft.warnings.map((warning) => <li key={warning.kind + warning.value}><code>{warning.value}</code> <span>({text.warningKind[warning.kind] ?? warning.kind})</span></li>)}</ul>
        </div>
      )}
      <p className="ai-draft-text" tabIndex={0}>{draft.text}</p>
      {draft.missing_info && <p className="ai-draft-missing"><strong>{text.missingInfo}:</strong> {draft.missing_info}</p>}
      <div className="ai-draft-sources">
        <span>{text.sources}</span>
        {draft.sources.length === 0 ? <span className="ai-draft-quiet">{text.noSources}</span> : (
          <ul>{draft.sources.map((source) => (
            <li key={source.kind + source.id}>
              <a href={source.kind === 'kb_article' ? `/knowledge/articles/${source.id}` : `/conversations/${source.id}`} target="_blank" rel="noopener">
                {source.kind === 'kb_article' ? <BookIcon /> : <InboxIcon />}
                <span className="sr-only">{source.kind === 'kb_article' ? text.article : text.ticket}: </span>{source.title}
              </a>
            </li>
          ))}</ul>
        )}
      </div>
      <p className="ai-draft-quiet">{text.editable}</p>
      <div className="ai-draft-actions">
        <button type="button" className="primary-action" title={hasText ? text.replaceTitle : undefined} onClick={() => onUse('replace')}>{hasText ? text.replace : text.useDraft}</button>
        {hasText && <button type="button" onClick={() => onUse('below')}>{text.insertBelow}</button>}
        <button type="button" disabled={busy} onClick={onDiscard}>{text.discard}</button>
        <button type="button" className="ai-draft-again" onClick={onAgain}>{text.again}</button>
      </div>
    </div>
  );
}

// Failures a setting causes: the provider's key or credit, the included AI, the feature turned off.
const settingsCodes = ['auth', 'permission', 'quota_exhausted', 'model_not_found', 'ai_allowance_reached', 'feature_off', 'ai_feature_off'];

function refusalCode(error: unknown): string | null {
  const detail = (error as { detail?: { code?: unknown } } | null)?.detail;
  if (typeof detail?.code === 'string') return detail.code;
  // A 402 carries no detail: the included AI is used up.
  return error instanceof PlanLimitError ? 'ai_allowance_reached' : null;
}

// SettingsWay is the next step for a failure a setting causes: Settings → AI for an admin, ask an admin otherwise.
function SettingsWay({ code, canManage }: { code: string | null; canManage: boolean }) {
  if (!code || !settingsCodes.includes(code)) return null;
  return canManage ? <a href="/settings/ai" target="_blank" rel="noopener">{text.openSettings}</a> : <span>{text.askAdmin}</span>;
}
