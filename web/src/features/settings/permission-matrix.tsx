import { useEffect, useRef, useState } from 'react';
import type { TokenPermission } from '../../api/schemas';
import { CheckIcon, ExternalLinkIcon, LockIcon } from '../../ui/icons';
import { actionLabel, allPermissions, tokenResources } from './token-permissions';
import { roleMessages as text } from './roles-messages';
import './permission-matrix.css';

interface PermissionMatrixProps {
  picked: readonly TokenPermission[];
  // allowed: what the person may give (what they hold). A chip outside it is shown, locked.
  allowed: ReadonlySet<string>;
  onToggle?: (permission: TokenPermission) => void;
  onSetMany?: (permissions: TokenPermission[], on: boolean) => void;
  readOnly?: boolean;
  // Keeps the ids of two matrices on one page apart.
  idPrefix: string;
  // Chips to mark as changed, for example after a role was changed by someone else.
  changed?: ReadonlySet<string>;
}

// PermissionMatrix lists every resource with a chip for each action. A chip the person does not hold is
// still listed, locked and dashed, so they see what exists without being able to give it.
export function PermissionMatrix({ picked, allowed, onToggle, onSetMany, readOnly = false, idPrefix, changed }: PermissionMatrixProps) {
  const [focused, setFocused] = useState<TokenPermission>();
  const description = allPermissions.find((item) => item.name === focused)?.describe ?? text.describeHint;
  // The count is announced after a bulk change, or once the person stops ticking for a second.
  // It reads the count of the render after the change, so it is never one tick behind.
  const [announced, setAnnounced] = useState('');
  const count = text.selectedCount(picked.length, allPermissions.length);
  const pending = useRef<'now' | 'later' | undefined>(undefined);
  const announce = (now: boolean) => { pending.current = now ? 'now' : 'later'; };
  useEffect(() => {
    const when = pending.current;
    pending.current = undefined;
    if (when === 'now') { setAnnounced(count); return undefined; }
    if (when !== 'later') return undefined;
    const timer = setTimeout(() => setAnnounced(count), 1000);
    return () => clearTimeout(timer);
  }, [count]);
  const lockedNote = `${idPrefix}-locked-note`;
  const editable = (name: TokenPermission) => !readOnly && allowed.has(name);
  const viewOnly = () => {
    onSetMany?.(allPermissions.filter((item) => editable(item.name) && !item.name.endsWith(':read')).map((item) => item.name), false);
    onSetMany?.(allPermissions.filter((item) => editable(item.name) && item.name.endsWith(':read')).map((item) => item.name), true);
    announce(true);
  };
  const clearAll = () => {
    onSetMany?.(allPermissions.filter((item) => editable(item.name)).map((item) => item.name), false);
    announce(true);
  };
  return (
    <div className="permission-matrix" id={`${idPrefix}-matrix`}>
      <div className="permission-toolbar">
        <span className="permission-count">{count}</span>
        {!readOnly && <span className="button-row">
          <button type="button" onClick={viewOnly}>{text.viewOnly}</button>
          <button type="button" onClick={clearAll}>{text.clearAll}</button>
        </span>}
        <span className="permission-legend"><span aria-hidden="true"><ExternalLinkIcon /></span>{text.outsideLegend}</span>
        <span className="sr-only" aria-live="polite">{announced}</span>
      </div>
      <p id={lockedNote} className="sr-only">{text.lockedNote}</p>
      {tokenResources.map((resource) => {
        const names = resource.permissions.map((item) => item.name);
        const ticked = names.filter((name) => picked.includes(name)).length;
        const mine = names.filter(editable);
        const all = mine.length > 0 && mine.every((name) => picked.includes(name));
        const some = mine.some((name) => picked.includes(name));
        const legend = `${idPrefix}-${resource.resource}`;
        return (
          <fieldset key={resource.resource} className="permission-row" aria-describedby={legend}>
            <legend className="permission-resource">
              {resource.label}
              {resource.permissions.some((item) => item.outside) && <span className="permission-outside" aria-hidden="true"><ExternalLinkIcon /></span>}
              <span className="permission-tally" id={legend}>{text.tally(ticked, names.length)}</span>
            </legend>
            <span className="permission-chips">
              {resource.permissions.map((item) => {
                const locked = !readOnly && !allowed.has(item.name);
                const label = `${actionLabel(item.name)}${item.outside ? text.reachesOutside : ''}`;
                return (
                  <label key={item.name} className="permission-chip" data-locked={locked || undefined} data-changed={changed?.has(item.name) || undefined}
                    onMouseEnter={() => setFocused(item.name)}>
                    <input type="checkbox" checked={picked.includes(item.name)} aria-label={`${resource.label}: ${label}`}
                      aria-disabled={locked || readOnly || undefined} aria-describedby={locked ? lockedNote : undefined}
                      onFocus={() => setFocused(item.name)}
                      onChange={() => { if (!locked && !readOnly) { onToggle?.(item.name); announce(false); } }} />
                    {locked && <span className="permission-lock" aria-hidden="true"><LockIcon /></span>}
                    {picked.includes(item.name) && <span className="permission-check" aria-hidden="true"><CheckIcon /></span>}
                    {actionLabel(item.name)}
                    {item.outside && <span className="permission-outside" aria-hidden="true"><ExternalLinkIcon /></span>}
                  </label>
                );
              })}
            </span>
            {!readOnly && mine.length > 0 && <button type="button" className="permission-all" aria-pressed={all ? true : some ? 'mixed' : false}
              aria-label={text.selectAll(resource.label)}
              onClick={() => { onSetMany?.(mine, !all); announce(true); }}>{text.all}</button>}
          </fieldset>
        );
      })}
      <p className="permission-describe" aria-live="off">{description}</p>
    </div>
  );
}
