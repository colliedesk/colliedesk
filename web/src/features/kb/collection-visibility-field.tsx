import { useId } from 'react';
import { visibilitySchema } from './kb-api';
import type { KBVisibility } from './kb-api';
import { visibilityMessages as v } from './visibility-messages';

// Who can see a collection's articles. An article inherits the most restrictive of it and its collections,
// so a collection only ever narrows; the server refuses an opening that would publish unreviewed text.
export function CollectionVisibilityField({ value, onChange }: { value: KBVisibility; onChange: (value: KBVisibility) => void }) {
  const id = useId();
  return <div className="field"><label htmlFor={id}>{v.heading}</label>
    <select id={id} value={value} aria-describedby={`${id}-help`} onChange={(event) => onChange(visibilitySchema.parse(event.target.value))}>
      {visibilitySchema.options.map((option) => <option key={option} value={option}>{v.names[option]}</option>)}
    </select>
    <small id={`${id}-help`} className="muted-copy">{v.collectionHelp}</small></div>;
}
