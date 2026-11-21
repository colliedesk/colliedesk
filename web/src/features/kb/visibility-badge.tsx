import { visibilitySchema } from './kb-api';
import type { KBVisibility } from './kb-api';
import { visibilityMessages as v } from './visibility-messages';

// A badge for an article or collection that not everyone sees; nothing for one everyone sees.
export function VisibilityBadge({ visibility }: { visibility: KBVisibility }) {
  if (visibility === 'everyone') return null;
  return <span className="badge" data-visibility={visibility}>{v.names[visibility]}</span>;
}

// The list filter "Who can see": Anyone, or the articles one audience sees.
export function VisibilityFilter({ value, onChange }: { value?: KBVisibility; onChange: (value?: KBVisibility) => void }) {
  return (
    <select className="kb-visibility-filter" aria-label={v.filter} value={value ?? ''}
      onChange={(event) => onChange(event.target.value === '' ? undefined : visibilitySchema.parse(event.target.value))}>
      <option value="">{v.anyone}</option>
      {visibilitySchema.options.map((option) => <option key={option} value={option}>{v.names[option]}</option>)}
    </select>
  );
}
