import { useEffect, useId, useRef, useState } from 'react';
import { fallbackTimezones } from '../../lib/fallback-timezones';
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

// Browsers fire change on every arrow key in a closed select, so a choice is saved only where the
// person stops: on leaving the list, on Enter, or after this pause.
const settleMs = 1500;

function canonical(zone: string): string {
  try { return new Intl.DateTimeFormat('en', { timeZone: zone }).resolvedOptions().timeZone; } catch { return zone; }
}

// Every IANA zone this browser knows, plus UTC. Browsers list some zones under an older name
// (Asia/Saigon for Asia/Ho_Chi_Minh): the stored name replaces its alias instead of appearing twice,
// and a stored zone the browser does not know at all is still offered as the current choice.
function zones(current: string): string[] {
  const known = typeof Intl.supportedValuesOf === 'function' ? Intl.supportedValuesOf('timeZone') : fallbackTimezones;
  const alias = current ? canonical(current) : '';
  const listed = known.map((zone) => (zone === alias ? current : zone));
  return [...new Set(['UTC', ...listed, ...(current ? [current] : [])])].sort((a, b) => (a === 'UTC' ? -1 : b === 'UTC' ? 1 : a.localeCompare(b)));
}

// TimezoneSelect saves without a button and shows the result next to it. It is never disabled while
// saving: a disabled control would throw keyboard focus out to the page.
export function TimezoneSelect({ label, value, emptyOption, save, onChange }: TimezoneSelectProps) {
  const feedback = useSaveFeedback(save);
  const id = useId();
  const [draft, setDraft] = useState(value);
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  // sent is the last zone handed to onChange; it stops a blur from saving the same zone again
  // while the first save is still on its way back.
  const sent = useRef(value);
  useEffect(() => { setDraft(value); sent.current = value; }, [value]);
  // A refused or failed save did not change the zone: the same choice may be sent again.
  const failed = save?.error;
  useEffect(() => { if (failed) sent.current = value; }, [failed, value]);
  useEffect(() => () => clearTimeout(timer.current), []);
  const commit = (next: string) => {
    clearTimeout(timer.current);
    if (next === sent.current) return;
    sent.current = next;
    onChange(next);
  };
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      <span className="input-with-actions">
        <select id={id} value={draft}
          onChange={(event) => {
            const next = event.target.value;
            feedback.touch();
            setDraft(next);
            clearTimeout(timer.current);
            timer.current = setTimeout(() => commit(next), settleMs);
          }}
          onBlur={() => commit(draft)}
          onKeyDown={(event) => { if (event.key === 'Enter') commit(draft); }}>
          {emptyOption && <option value="">{emptyOption}</option>}
          {zones(draft).map((zone) => <option key={zone} value={zone}>{zone}</option>)}
        </select>
        {feedback.pending ? <span className="save-status">{messages.saving}</span> : feedback.status}
      </span>
    </div>
  );
}
