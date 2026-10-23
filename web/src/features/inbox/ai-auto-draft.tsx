import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { formatDate, formatShortDate } from '../../lib/format-date';
import { aiDraftApi, writing } from './ai-draft-api';
import type { AIDraft } from './ai-draft-api';
import type { DraftUse } from './ai-draft-panel';
import { SettingsWay } from './ai-failure';
import { AISources } from './ai-sources';
import './ai-draft.css';

// Strings of the automatic draft card; it is in the AI draft chunk, so they live there.
const text = {
  label: 'Automatic AI draft',
  drafting: 'AI is drafting a reply',
  draftingTo: (who: string, at: string) => ` to ${who}’s message of ${at}.`,
  draftingRest: ' Reply yourself if you like; the draft waits here.',
  wroteAgain: (who: string, at: string) => `${who} wrote again at ${at}.`,
  wroteAgainRest: ' A new draft that answers both messages is coming.',
  draftFor: (who: string, at: string) => `Draft for ${who}’s message of ${at}`,
  team: 'Written automatically · your team sees it',
  overText: 'Your reply box has text, so nothing was replaced.',
  neverSends: 'AI never sends. Check it, then send it yourself.',
  discard: 'Discard',
  useDraft: 'Use draft',
  insertBelow: 'Insert below',
  replace: 'Replace my text',
  noReply: 'No reply needed',
  noReplyRest: ', AI thinks. The draft is here if you want it.',
  showDraft: 'Show draft',
  pausedDay: (until: string) => `Automatic drafts are paused until ${until}:`,
  pausedDayRest: ' today’s are used. Draft with AI below still works.',
  pausedMonth: (until: string) => `Automatic drafts are paused until ${until}:`,
  pausedMonthRest: ' they used their half of this month’s included AI. Draft with AI below still works.',
  capped: 'No automatic draft for this message:',
  cappedRest: ' this conversation had 5 in 24 hours. Draft with AI below still works.',
  failed: 'The automatic draft failed:',
  failedWhy: (code: string | null) => failures[code ?? ''] ?? 'the AI provider did not answer.',
  failedNext: ' Reply yourself, or use Draft with AI.',
  customer: 'the customer',
};

const failures: Record<string, string> = {
  auth: 'the AI provider refused its key.',
  quota_exhausted: 'the AI provider account is out of credit.',
  ai_allowance_reached: 'this month’s included AI is used up.',
  refused: 'the model declined to write this reply.',
  too_long: 'the draft ran past its length.',
  empty_answer: 'the model returned no reply.',
};

// Stopped because it was no longer wanted: nothing to show.
const quiet = ['answered', 'cancelled', 'automation_off', 'conversation_closed', 'feature_off', 'restored'];

interface AutoDraftCardProps {
  conversationId: string;
  customerName?: string;
  timezone: string;
  // Whether the reply box holds text: then the draft goes below it or replaces it, never by itself.
  hasText: boolean;
  canManage?: boolean;
  // Changes when a new customer message arrives, so the card asks again.
  latestCustomerMessage?: string;
  onUse: (draftText: string, use: DraftUse, draftId: string, warnings: string[]) => void;
}

function nextUTCMidnight(at: string) {
  const day = new Date(at);
  return new Date(Date.UTC(day.getUTCFullYear(), day.getUTCMonth(), day.getUTCDate() + 1)).toISOString();
}

function nextUTCMonth(at: string) {
  const day = new Date(at);
  return new Date(Date.UTC(day.getUTCFullYear(), day.getUTCMonth() + 1, 1)).toISOString();
}

// AutoDraftCard is the draft AI wrote by itself for the customer's newest message, above the reply box
// for everyone on the team. It never goes into the box by itself and is never sent: a person uses it,
// below their text or in its place, or discards it.
export function AutoDraftCard({ conversationId, customerName, timezone, hasText, canManage = false, latestCustomerMessage, onUse }: AutoDraftCardProps) {
  const client = useQueryClient();
  const key = ['ai-drafts', conversationId];
  const list = useQuery({ queryKey: key, queryFn: () => aiDraftApi.list(conversationId),
    refetchInterval: (query) => (query.state.status !== 'error' && query.state.data?.items.some((item) => item.automatic && writing(item)) ? 10_000 : false) });
  // A new customer message may have queued a new draft, or replaced the one shown.
  useEffect(() => {
    if (latestCustomerMessage) void client.invalidateQueries({ queryKey: ['ai-drafts', conversationId] });
  }, [latestCustomerMessage, conversationId, client]);
  const discard = useMutation({ mutationFn: (draft: AIDraft) => aiDraftApi.discard(conversationId, draft.id),
    onSuccess: () => void client.invalidateQueries({ queryKey: key }) });
  const automatic = (list.data?.items ?? []).filter((item) => item.automatic);
  const draft = automatic[0];
  if (!draft) return null;
  const who = customerName || text.customer;
  const at = formatShortDate(draft.created_at, timezone);
  if (writing(draft)) {
    const replaced = automatic.find((item) => item.outcome === 'superseded');
    return (
      <div className="ai-auto-draft ai-auto-draft-strip" role="status">
        <span className="ai-draft-pulse" aria-hidden="true" />
        {replaced ? <span><b>{text.wroteAgain(who, at)}</b>{text.wroteAgainRest}</span>
          : <span><b>{text.drafting}</b>{text.draftingTo(who, at)}<span className="ai-long">{text.draftingRest}</span></span>}
      </div>
    );
  }
  if (draft.status === 'failed' || draft.status === 'cancelled') {
    const code = draft.error_code;
    if (code === null || quiet.includes(code)) return null;
    if (code === 'draft_limit' || code === 'automatic_limit') {
      return <div className="ai-auto-draft ai-auto-draft-strip note" role="status"><span><b>{text.pausedDay(formatDate(nextUTCMidnight(draft.created_at), timezone))}</b>{text.pausedDayRest}</span></div>;
    }
    if (code === 'allowance_reserved') {
      return <div className="ai-auto-draft ai-auto-draft-strip note" role="status"><span><b>{text.pausedMonth(formatDate(nextUTCMonth(draft.created_at), timezone))}</b>{text.pausedMonthRest}</span></div>;
    }
    if (code === 'conversation_cap') {
      return <div className="ai-auto-draft ai-auto-draft-strip note" role="status"><span><b>{text.capped}</b>{text.cappedRest}</span></div>;
    }
    return (
      <div className="ai-auto-draft ai-auto-draft-strip danger" role="alert">
        <span><b>{text.failed}</b> {text.failedWhy(code)}{text.failedNext}</span>
        <span className="ai-auto-draft-acts"><SettingsWay code={code} canManage={canManage} /></span>
      </div>
    );
  }
  if (draft.status !== 'ready' || draft.outcome !== null || !draft.text) return null;
  return <ReadyAutoDraft key={draft.id} draft={draft} title={text.draftFor(who, at)} hasText={hasText} busy={discard.isPending}
    onDiscard={() => discard.mutate(draft)} onUse={(how) => onUse(draft.text ?? '', how, draft.id, draft.warnings.map((warning) => warning.value))} />;
}

function ReadyAutoDraft({ draft, title, hasText, busy, onDiscard, onUse }: { draft: AIDraft; title: string; hasText: boolean; busy: boolean;
  onDiscard: () => void; onUse: (how: DraftUse) => void }) {
  // A draft the model thinks needs no reply starts folded; Show draft opens it.
  const [shown, setShown] = useState(draft.reply_needed !== false);
  if (!shown) {
    return (
      <section className="ai-auto-draft ai-auto-draft-strip" aria-label={text.label}>
        <span className="ai-chip" aria-hidden="true">AI</span><span><b>{text.noReply}</b>{text.noReplyRest}</span>
        <span className="ai-auto-draft-acts"><button type="button" className="ghost" disabled={busy} onClick={onDiscard}>{text.discard}</button>
          <button type="button" onClick={() => setShown(true)}>{text.showDraft}</button></span>
      </section>
    );
  }
  return (
    <section className="ai-auto-draft" aria-label={text.label}>
      <div className="ai-auto-draft-head"><span className="ai-chip" aria-hidden="true">AI</span><b>{title}</b>
        <span className="muted-copy">{hasText ? text.overText : text.team}</span></div>
      {draft.warnings.length > 0 && <p className="ai-draft-check" role="alert">{draft.warnings.map((warning) => warning.value).join(', ')}</p>}
      <p className="ai-auto-draft-text" tabIndex={0}>{draft.text}</p>
      <AISources sources={draft.sources} />
      <div className="ai-auto-draft-row">
        <span className="muted-copy ai-long">{text.neverSends}</span>
        <button type="button" className="ghost" disabled={busy} onClick={onDiscard}>{text.discard}</button>
        {hasText ? <><button type="button" onClick={() => onUse('below')}>{text.insertBelow}</button>
          <button type="button" className="primary-action" onClick={() => onUse('replace')}>{text.replace}</button></>
          : <button type="button" className="primary-action" onClick={() => onUse('replace')}>{text.useDraft}</button>}
      </div>
    </section>
  );
}

