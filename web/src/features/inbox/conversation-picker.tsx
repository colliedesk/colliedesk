import { useId } from 'react';
import type { Conversation } from '../../api/schemas';
import { formatShortDate } from '../../lib/format-date';
import { StatusLabel } from './conversation-labels';
import './conversation-picker.css';

export interface PickableConversation { id: string; subject: string; status: Conversation['status']; contactName: string; updatedAt: string; note?: string }

// ConversationPicker is one conversation of a few, as radio rows that read like the inbox's: subject, then
// status, customer and last activity, and a note (what picking it does) when there is one.
export function ConversationPicker({ label, items, value, onChange, timezone }: {
  label: string; items: PickableConversation[]; value: string | undefined; onChange: (id: string) => void; timezone: string }) {
  const name = useId();
  return <fieldset className="candidate-list">
    <legend className="sr-only">{label}</legend>
    {items.map((item) => <label key={item.id} className="candidate-row">
      <input type="radio" name={name} value={item.id} checked={value === item.id} onChange={() => onChange(item.id)} />
      <span className="candidate-subject">{item.subject}</span>
      <span className="candidate-meta">
        <StatusLabel status={item.status} />
        <span>{item.contactName}</span>
        <time dateTime={item.updatedAt}>{formatShortDate(item.updatedAt, timezone)}</time>
        {item.note && <span className="muted-copy">{item.note}</span>}
      </span>
    </label>)}
  </fieldset>;
}
