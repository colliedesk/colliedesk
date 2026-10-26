import { useId } from 'react';
import type { IntakeCandidate } from '../../api/intake';
import { formatShortDate } from '../../lib/format-date';
import { StatusLabel } from '../inbox/conversation-labels';
import { reviewMessages as m } from './review-messages';

// CandidatePicker is the conversations a card may join: the server lists only the safe ones (the same
// group, or the same customer on the same channel one-to-one), the rules' target first. A row reads like
// the inbox's: subject, customer, status, last activity. A resolved one reopens when the card joins it.
export function CandidatePicker({ candidates, value, onChange, timezone }: {
  candidates: IntakeCandidate[]; value: string | undefined; onChange: (id: string) => void; timezone: string }) {
  const name = useId();
  if (candidates.length === 0) return <p className="muted-copy">{m.noCandidates}</p>;
  return <fieldset className="candidate-list">
    <legend className="sr-only">{m.pickConversation}</legend>
    {candidates.map((candidate) => <label key={candidate.conversation_id} className="candidate-row">
      <input type="radio" name={name} value={candidate.conversation_id} checked={value === candidate.conversation_id}
        onChange={() => onChange(candidate.conversation_id)} />
      <span className="candidate-subject">{candidate.subject}</span>
      <span className="candidate-meta">
        <StatusLabel status={candidate.status} />
        <span>{candidate.contact_name}</span>
        <time dateTime={candidate.updated_at}>{formatShortDate(candidate.updated_at, timezone)}</time>
        {candidate.resolved && <span className="muted-copy">{m.resolvedCandidate}</span>}
      </span>
    </label>)}
  </fieldset>;
}
