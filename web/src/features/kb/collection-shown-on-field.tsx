import { useId } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useHolds } from '../../lib/permissions-context';
import { helpCenterName, helpCentersApi, helpCentersKey } from './help-centers-api';
import { helpCentersMessages as text } from './help-centers-messages';

interface ShownOnProps {
  // value is the chosen help centers; null keeps what it has (a new collection: the primary help center).
  value: string[] | null;
  // saved is what the collection is shown on now, for a collection being edited.
  saved?: string[];
  nested: boolean;
  onChange: (ids: string[]) => void;
}

// CollectionShownOnField chooses the help centers whose home page lists a top-level collection. A collection
// inside another follows its top-level collection, and a workspace with one help center has nothing to choose.
// Choosing needs kb_articles:publish, since it changes what readers see; the last checked box stays checked.
export function CollectionShownOnField({ value, saved, nested, onChange }: ShownOnProps) {
  const id = useId();
  const holds = useHolds();
  const allowed = holds('help_center:read') && holds('kb_articles:publish');
  const list = useQuery({ queryKey: helpCentersKey, queryFn: helpCentersApi.list, enabled: allowed });
  const centers = list.data?.help_centers ?? [];
  if (!allowed || centers.length < 2) return null;
  if (nested) return <p className="muted-copy">{text.shownOnNested}</p>;
  const primary = centers.find((center) => center.primary);
  const chosen = new Set(value ?? saved ?? (primary ? [primary.id] : []));
  const toggle = (center: string) => {
    const next = new Set(chosen);
    if (next.has(center)) next.delete(center); else next.add(center);
    onChange(centers.filter((item) => next.has(item.id)).map((item) => item.id));
  };
  return <fieldset className="field kb-shown-on" aria-describedby={`${id}-help`}>
    <legend>{text.shownOn}</legend>
    {centers.map((center) => {
      const checked = chosen.has(center.id);
      return <label key={center.id} className="check-row">
        <input type="checkbox" checked={checked} disabled={checked && chosen.size === 1} onChange={() => toggle(center.id)} />
        <span>{helpCenterName(center, text.primaryName)}</span>
      </label>;
    })}
    <small id={`${id}-help`} className="muted-copy">{text.shownOnHelp}</small>
  </fieldset>;
}
