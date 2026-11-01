import { useId } from 'react';
import './controls.css';
import './choice.css';

export interface ChoiceItem<T extends string> { value: T; label: string }

// Choice is one value of a few, as a native radio group drawn on the segmented track of the tabs: the
// arrow keys move between options and pick, Tab leaves the group, as every radio group does.
export function Choice<T extends string>({ legend, items, value, onChange, disabled, describedBy, hideLegend }: {
  legend: string; items: ChoiceItem<T>[]; value: T; onChange: (value: T) => void; disabled?: boolean; describedBy?: string; hideLegend?: boolean }) {
  const name = useId();
  return <fieldset className="seg-choice" disabled={disabled} aria-describedby={describedBy}>
    <legend className={hideLegend ? 'sr-only' : 'seg-choice-legend'}>{legend}</legend>
    <div className="seg-choice-track">
      {items.map((item) => <label key={item.value} className="seg-choice-option">
        <input type="radio" name={name} value={item.value} checked={value === item.value} onChange={() => onChange(item.value)} />
        <span>{item.label}</span>
      </label>)}
    </div>
  </fieldset>;
}
