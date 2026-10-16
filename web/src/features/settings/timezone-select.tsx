import { useId } from 'react';
import { messages } from '../../messages';
import { useSaveFeedback } from './save-feedback';
import type { SaveState } from './save-feedback';

interface TimezoneSelectProps {
  label: string;
  value: string;
  // emptyOption, when given, is the first choice and stands for "no value" (an empty string).
  emptyOption?: string;
  save?: SaveState;
  onChange: (value: string) => void;
}

// Every IANA zone this browser knows. UTC and the stored value are always offered, so a zone the
// browser does not list is still shown as the current choice instead of silently changing.
function zones(current: string): string[] {
  const known = typeof Intl.supportedValuesOf === 'function' ? Intl.supportedValuesOf('timeZone') : [];
  return [...new Set(['UTC', ...known, ...(current ? [current] : [])])].sort((a, b) => (a === 'UTC' ? -1 : b === 'UTC' ? 1 : a.localeCompare(b)));
}

// TimezoneSelect saves as soon as a zone is chosen; the result shows next to it.
export function TimezoneSelect({ label, value, emptyOption, save, onChange }: TimezoneSelectProps) {
  const feedback = useSaveFeedback(save);
  const id = useId();
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      <span className="input-with-actions">
        <select id={id} value={value} disabled={feedback.pending} onChange={(event) => { feedback.touch(); onChange(event.target.value); }}>
          {emptyOption && <option value="">{emptyOption}</option>}
          {zones(value).map((zone) => <option key={zone} value={zone}>{zone}</option>)}
        </select>
        {feedback.pending ? <span className="save-status">{messages.saving}</span> : feedback.status}
      </span>
    </div>
  );
}
