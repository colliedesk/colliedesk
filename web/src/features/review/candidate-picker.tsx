import type { IntakeCandidate } from '../../api/intake';
import { ConversationPicker } from '../inbox/conversation-picker';
import { reviewMessages as m } from './review-messages';

// CandidatePicker is the conversations a card may join: the server lists only the safe ones (the same
// group, or the same customer on the same channel one-to-one), the rules' target first. A resolved one
// reopens when the card joins it.
export function CandidatePicker({ candidates, value, onChange, timezone }: {
  candidates: IntakeCandidate[]; value: string | undefined; onChange: (id: string) => void; timezone: string }) {
  if (candidates.length === 0) return <p className="muted-copy">{m.noCandidates}</p>;
  return <ConversationPicker label={m.pickConversation} value={value} onChange={onChange} timezone={timezone}
    items={candidates.map((candidate) => ({ id: candidate.conversation_id, subject: candidate.subject, status: candidate.status, contactName: candidate.contact_name,
      updatedAt: candidate.updated_at, note: candidate.resolved ? m.resolvedCandidate : undefined }))} />;
}
