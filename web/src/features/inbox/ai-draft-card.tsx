import { useEffect, useRef, useState } from 'react';
import type { AIDraft } from './ai-draft-api';
import { aiMessages } from './ai-draft-messages';

interface AIDraftCardProps {
  draft: AIDraft;
  timezone: string;
  // onSend sends the text shown, edited or not, as a reply that names the draft.
  onSend: (body: string) => Promise<unknown>;
  onDiscard: () => Promise<unknown>;
  onApplyTriage: () => Promise<unknown>;
  onCancelAutoAnswer: () => Promise<unknown>;
  onRegenerate: () => Promise<unknown>;
}

const confidenceLabel = { high: aiMessages.trusted, medium: aiMessages.check, low: aiMessages.guess } as const;

function time(value: string, timeZone: string): string {
  try {
    return new Intl.DateTimeFormat('en', { hour: '2-digit', minute: '2-digit', hourCycle: 'h23', timeZone }).format(new Date(value));
  } catch {
    return new Intl.DateTimeFormat('en', { hour: '2-digit', minute: '2-digit', hourCycle: 'h23', timeZone: 'UTC' }).format(new Date(value));
  }
}

// AIDraftCard is the reply and triage suggestion Colliedesk wrote from the knowledge base for the
// newest customer message. Nothing reaches the customer until a person presses Send, unless the
// workspace let a proven topic answer on its own; then the card says when, and Stop holds it back.
export function AIDraftCard({ draft, timezone, onSend, onDiscard, onApplyTriage, onCancelAutoAnswer, onRegenerate }: AIDraftCardProps) {
  const original = draft.body ?? '';
  const [editing, setEditing] = useState(false);
  const [text, setText] = useState(original);
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const field = useRef<HTMLTextAreaElement>(null);
  useEffect(() => { setText(original); setEditing(false); }, [draft.id, original]);
  useEffect(() => { if (editing) field.current?.focus(); }, [editing]);
  const run = async (action: () => Promise<unknown>) => {
    setBusy(true);
    setFailed(false);
    try {
      await action();
    } catch {
      setFailed(true);
    } finally {
      setBusy(false);
    }
  };
  const triage = draft.triage;
  const canSend = (draft.status === 'ready' || draft.status === 'scheduled') && draft.action !== 'handoff' && original !== '';
  return (
    <section className="reply-draft ai-draft" aria-label={aiMessages.title} data-confidence={draft.confidence ?? undefined}>
      <p className="reply-draft-by">{aiMessages.title}
        {draft.confidence && draft.status !== 'queued' && <span className="ai-draft-confidence">{confidenceLabel[draft.confidence]}</span>}</p>
      {draft.status === 'queued' && <p role="status" className="ai-draft-note">{aiMessages.writing}</p>}
      {draft.status === 'failed' && <p className="ai-draft-note">{aiMessages.failed(draft.error_code)}</p>}
      {draft.status === 'scheduled' && draft.auto_send_at && <p className="ai-draft-note">{aiMessages.scheduled(time(draft.auto_send_at, timezone))}</p>}
      {draft.action === 'handoff' && <p className="ai-draft-note"><strong>{aiMessages.handoff}.</strong> {draft.handoff_reason}</p>}
      {canSend && (editing
        ? <textarea ref={field} className="reply-draft-edit" aria-label={aiMessages.text} rows={5} value={text} maxLength={20000} onChange={(event) => setText(event.target.value)} />
        : <p className="reply-draft-text">{original}</p>)}
      {draft.confidence !== 'high' && draft.confidence_reasons.length > 0 && draft.action !== 'handoff' && draft.status !== 'queued' &&
        <p className="ai-draft-note">{aiMessages.why(draft.confidence_reasons)}{draft.handoff_reason ? ` ${draft.handoff_reason}` : ''}</p>}
      {draft.sources.length > 0 && <p className="ai-draft-sources">{aiMessages.sources}: {draft.sources.map((source, index) => (
        <span key={source.id}>{index > 0 && ', '}<a href={source.kind === 'kb_article' ? `/knowledge/articles/${source.id}` : `/conversations/${source.id}`}>
          {source.number ? `#${source.number} ` : ''}{source.title}</a></span>))}</p>}
      {triage && (draft.triage_applied_at
        ? <p className="ai-draft-note">{aiMessages.triageApplied}</p>
        : <p className="ai-draft-triage">{aiMessages.triage}: {[...triage.tags, triage.priority ? `priority ${triage.priority}` : ''].filter(Boolean).join(' · ')}
          {' '}<button type="button" className="link-action" disabled={busy} onClick={() => void run(onApplyTriage)}>{aiMessages.applyTriage(triage.tags, triage.priority)}</button></p>)}
      {failed && <p role="alert" className="error-copy">{aiMessages.actionFailed}</p>}
      <div className="button-row">
        {canSend && <button type="button" className="primary-action" disabled={busy || !text.trim()} onClick={() => void run(() => onSend(editing ? text : original))}>{aiMessages.send}</button>}
        {canSend && !editing && <button type="button" disabled={busy} onClick={() => setEditing(true)}>{aiMessages.edit}</button>}
        {draft.status === 'scheduled' && <button type="button" disabled={busy} onClick={() => void run(onCancelAutoAnswer)}>{aiMessages.stop}</button>}
        {(draft.status === 'ready' || draft.status === 'scheduled') && <button type="button" className="danger-action" disabled={busy} onClick={() => void run(onDiscard)}>{aiMessages.discard}</button>}
        {(draft.status === 'failed' || draft.status === 'discarded' || draft.action === 'handoff') &&
          <button type="button" disabled={busy} onClick={() => void run(onRegenerate)}>{aiMessages.regenerate}</button>}
      </div>
    </section>
  );
}
