import type { IntakeDecideInput, IntakeSuggestion } from '../../api/intake';
import { messages } from '../../messages';
import { SparklesIcon } from '../../ui/icons';
import { reviewMessages as m } from './review-messages';

// followInput is the decision that follows a suggestion as it stands; its reply kind comes with it.
export function followInput(suggestion: IntakeSuggestion): IntakeDecideInput | undefined {
  const follows = { follows_decision_id: suggestion.decision_id };
  if (suggestion.action === 'attach') return suggestion.target_conversation_id ? { action: 'attach', conversation_id: suggestion.target_conversation_id, ...follows } : undefined;
  if (suggestion.action === 'ignore') return { action: 'ignore', ...(suggestion.ignore_reason ? { ignore_reason: suggestion.ignore_reason } : {}), ...follows };
  return { action: 'create', ...(suggestion.subject ? { subject: suggestion.subject } : {}), ...(suggestion.command ? { command: suggestion.command } : {}), ...follows };
}

// AISuggestion is what AI suggested for a card a person decides: the decision, how sure it was, the
// runner-up, why, and why it did not do it alone; Follow suggestion decides the card that way.
export function AISuggestion({ suggestion, busy, onFollow }: { suggestion: IntakeSuggestion; busy: boolean; onFollow?: (input: IntakeDecideInput) => void }) {
  const what = suggestion.action === 'attach' ? m.suggestAttach(suggestion.target_subject)
    : suggestion.action === 'ignore' ? m.suggestIgnore(suggestion.ignore_reason && m.ignoreReasons[suggestion.ignore_reason])
      : [m.suggestCreate(suggestion.subject), suggestion.command].filter(Boolean).join(' · ');
  const closed = suggestion.action === 'attach' && (suggestion.target_status === 'resolved' || suggestion.target_status === 'closed');
  // A thank-you keeps a resolved conversation resolved; a closed one reopens all the same.
  const stays = closed && suggestion.target_status === 'resolved' && suggestion.reply_kind === 'acknowledgement';
  const why = m.notApplied[suggestion.not_applied_reason];
  const input = followInput(suggestion);
  return <div className="intake-suggestion" role="group" aria-label={m.suggested}>
    <p className="intake-suggestion-head"><span className="intake-ai-chip" aria-hidden="true"><SparklesIcon />AI</span>
      <span><strong>{what}</strong>{suggestion.confidence !== null && <> · {m.sure(suggestion.confidence)}</>}</span></p>
    {closed && suggestion.target_status && <p className="muted-copy">{stays ? m.staysResolved : m.reopens(messages.statuses[suggestion.target_status])}</p>}
    {suggestion.action === 'attach' && suggestion.runner_up_conversation_id && <p className="muted-copy">{m.runnerUp(suggestion.runner_up_subject, suggestion.runner_up_confidence)}</p>}
    {suggestion.reason && <p className="intake-suggestion-reason">{suggestion.reason}</p>}
    {why && <p className="muted-copy">{why}</p>}
    {input && onFollow && <div className="request-actions"><button type="button" disabled={busy} onClick={() => onFollow(input)}>{m.follow}</button></div>}
  </div>;
}
