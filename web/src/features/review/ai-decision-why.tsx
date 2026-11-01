import type { IntakeAIDecision } from '../../api/intake';
import { formatDate } from '../../lib/format-date';
import './ai-decision.css';

// Why AI decided a card by itself: how sure it was, the model it asked (and the fallback model that answered
// instead, when the server says one did), when, and the reason it gave (erased after 90 days). Shown where its decision
// landed: on a message AI added to a conversation, and on the card in Review.

const text = {
  sure: (confidence: number) => `${Math.round(confidence * 100)}% sure`,
  model: (model: string | null, fallback: string | null) => (fallback ? `${fallback} (fallback for ${model ?? 'the channel’s model'})` : model ?? ''),
};

type Decision = Pick<IntakeAIDecision, 'confidence' | 'reason' | 'model' | 'fallback_model' | 'created_at'>;

export function AIDecisionWhy({ decision, timezone, label }: { decision: Decision; timezone: string; label: string }) {
  const model = text.model(decision.model, decision.fallback_model);
  return <span className="ai-decision-why" role="note" aria-label={label}>
    <span className="ai-decision-how">
      {[decision.confidence !== null && text.sure(decision.confidence), model].filter(Boolean).join(' · ')}
      {' · '}<time dateTime={decision.created_at}>{formatDate(decision.created_at, timezone)}</time>
    </span>
    {decision.reason && <span className="ai-decision-reason">{decision.reason}</span>}
  </span>;
}
