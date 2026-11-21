import { useEffect, useRef, useState } from 'react';
import { InvalidRequestError } from '../../api/client';
import { formatDay } from '../../lib/format-date';
import { messages } from '../../messages';
import { BookmarkIcon } from '../../ui/icons';
import { savedViewMessages } from './saved-view-messages';

type Scope = 'personal' | 'shared';
// AnyView is a saved view of one list (the inbox, the knowledge base), with the filters that list stores.
export interface AnyView<F> {
  id: string;
  name: string;
  scope: Scope;
  updated_at: string;
  purges_at: string | null;
  filters: F;
}

export interface SavedViewChange<F> {
  name?: string;
  scope?: Scope;
  filters?: F;
}

export interface SavedViewsPanelProps<F, V extends AnyView<F>> {
  views: V[];
  // loading: the views are on their way, so the menu does not say there are none yet.
  loading?: boolean;
  trash?: V[];
  // The filters on screen: saving a view stores them; isCurrent marks the view they match.
  filters: F;
  isCurrent: (view: V) => boolean;
  timezone: string;
  onApply?: (view: V) => void;
  onCreate?: (view: { name: string; scope: Scope; filters: F }) => Promise<unknown>;
  onUpdate?: (view: V, change: SavedViewChange<F>) => Promise<unknown>;
  // A delete moves the view to the trash; onRestore takes it out again.
  onDelete?: (view: V) => Promise<unknown>;
  onRestore?: (view: V) => Promise<unknown>;
  // The views are asked for when the menu opens, the trash when its section opens; until then trash is
  // undefined, and the section shows no count.
  onOpen?: () => void;
  onOpenTrash?: () => void;
}

const text = { ...savedViewMessages, title: messages.savedViewsTitle };

// A refusal the server worded for people (a taken name, a full list) is shown as it is.
function refusal(error: unknown): string {
  return error instanceof InvalidRequestError ? error.message : text.failed;
}

interface ViewRowProps<F, V extends AnyView<F>> extends Pick<SavedViewsPanelProps<F, V>, 'filters' | 'onApply' | 'onUpdate'> {
  view: V;
  current: boolean;
  onDelete: (view: V) => void;
}

function ViewRow<F, V extends AnyView<F>>({ view, current, filters, onApply, onUpdate, onDelete }: ViewRowProps<F, V>) {
  const actions = useRef<HTMLDetailsElement>(null);
  const [renaming, setRenaming] = useState(false);
  const [name, setName] = useState(view.name);
  const [error, setError] = useState('');
  const run = async (change: SavedViewChange<F>) => {
    setError('');
    try {
      await onUpdate?.(view, change);
      if (actions.current) actions.current.open = false;
      setRenaming(false);
    } catch (failure) {
      setError(refusal(failure));
    }
  };
  return (
    <li className="view-row">
      <button type="button" className="view-open" aria-current={current || undefined} onClick={() => onApply?.(view)}>{view.name}</button>
      <details className="view-actions" ref={actions}>
        <summary aria-label={text.actions(view.name)}><span aria-hidden="true">⋯</span></summary>
        <div className="view-actions-popover">
          {renaming ? (
            <form onSubmit={(event) => { event.preventDefault(); void run({ name: name.trim() }); }}>
              <label className="field"><span className="sr-only">{text.renameTo(view.name)}</span>
                <input aria-label={text.renameTo(view.name)} value={name} maxLength={80} required onChange={(event) => setName(event.currentTarget.value)} autoFocus /></label>
              <div className="view-actions-buttons">
                <button type="submit" disabled={!name.trim()}>{text.saveName}</button>
                <button type="button" onClick={() => { setRenaming(false); setName(view.name); }}>{text.cancel}</button>
              </div>
            </form>
          ) : (
            <>
              <button type="button" className="menu-item" onClick={() => void run({ filters })}>{text.updateFilters}</button>
              <button type="button" className="menu-item" onClick={() => setRenaming(true)}>{text.rename}</button>
              <button type="button" className="menu-item" onClick={() => void run({ scope: view.scope === 'personal' ? 'shared' : 'personal' })}>
                {view.scope === 'personal' ? text.share : text.makePersonal}</button>
              <button type="button" className="menu-item danger-action" onClick={() => { if (actions.current) actions.current.open = false; onDelete(view); }}>{text.delete}</button>
            </>
          )}
          {error && <p role="alert" className="error-copy">{error}</p>}
        </div>
      </details>
    </li>
  );
}
// SavedViewsPanel keeps named sets of filters of one list: the team's shared views and the person's own.
// Choosing one sets its filters, which the address then carries.
export function SavedViewsPanel<F, V extends AnyView<F>>({ views, loading = false, trash, filters, isCurrent, timezone, onApply, onCreate, onUpdate, onDelete, onRestore, onOpen, onOpenTrash }: SavedViewsPanelProps<F, V>) {
  const menu = useRef<HTMLDetailsElement>(null);
  const [name, setName] = useState('');
  const [scope, setScope] = useState<Scope>('personal');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [deleted, setDeleted] = useState<V>();
  const [trashError, setTrashError] = useState('');
  const [undoError, setUndoError] = useState('');
  useEffect(() => {
    const close = (event: PointerEvent) => { if (menu.current?.open && !menu.current.contains(event.target as Node)) menu.current.open = false; };
    document.addEventListener('pointerdown', close);
    return () => document.removeEventListener('pointerdown', close);
  }, []);
  // Focus that moves to something outside closes the menu; focus that is only lost (the Save button turns
  // disabled while it saves) keeps it open, so the new view shows up in it.
  const closeOnFocusOut = (event: React.FocusEvent<HTMLDetailsElement>) => {
    const next = event.relatedTarget as Node | null;
    if (menu.current?.open && next && !menu.current.contains(next)) menu.current.open = false;
  };
  const closeOnEscape = (event: React.KeyboardEvent<HTMLDetailsElement>) => {
    if (event.key !== 'Escape' || !menu.current?.open) return;
    // An inner menu (a row's actions) closes first.
    const inner = menu.current.querySelector<HTMLDetailsElement>('details[open]');
    if (inner) { inner.open = false; inner.querySelector('summary')?.focus(); return; }
    menu.current.open = false;
    menu.current.querySelector('summary')?.focus();
  };
  const save = async () => {
    if (!name.trim() || !onCreate) return;
    setBusy(true);
    setError('');
    try {
      await onCreate({ name: name.trim(), scope, filters });
      setName('');
    } catch (failure) {
      setError(refusal(failure));
    } finally {
      setBusy(false);
    }
  };
  const remove = (view: V) => {
    void onDelete?.(view).then(() => setDeleted(view), () => setError(text.failed));
  };
  const undo = () => {
    if (!deleted) return;
    const view = deleted;
    setDeleted(undefined);
    setUndoError('');
    void onRestore?.(view).catch((failure) => setUndoError(refusal(failure)));
  };
  const groups = [['shared', text.shared], ['personal', text.personal]] as const;
  return (
    <>
      <details className="filter-menu views-menu" ref={menu} onKeyDown={closeOnEscape} onBlur={closeOnFocusOut}
        onToggle={(event) => { if (event.currentTarget.open) onOpen?.(); }}>
        <summary data-tip={text.title}><BookmarkIcon /><span className="sr-only">{text.title}</span></summary>
        <div className="filter-popover views-popover" role="group" aria-label={text.title}>
          {views.length === 0 && <p className="filter-hint" role="status">{loading ? text.loading : text.none}</p>}
          {groups.map(([group, label]) => {
            const items = views.filter((view) => view.scope === group);
            return items.length > 0 && (
              <section key={group} className="view-group"><h3 id={`views-${group}`}>{label}</h3>
                <ul aria-labelledby={`views-${group}`}>
                  {items.map((view) => <ViewRow key={view.id} view={view} current={isCurrent(view)} filters={filters} onApply={onApply} onUpdate={onUpdate} onDelete={remove} />)}
                </ul>
              </section>
            );
          })}
          <form className="view-save" onSubmit={(event) => { event.preventDefault(); void save(); }}>
            <h3>{text.saveCurrent}</h3>
            <label className="field"><span>{text.name}</span>
              <input value={name} maxLength={80} onChange={(event) => setName(event.currentTarget.value)} /></label>
            <fieldset><legend>{text.scope}</legend>
              {(['personal', 'shared'] as const).map((value) => (
                <label key={value} className="check-row"><input type="radio" name="view-scope" checked={scope === value} onChange={() => setScope(value)} /><span>{text.scopes[value]}</span></label>))}
            </fieldset>
            <button type="submit" className="primary-action" disabled={busy || !name.trim()}>{busy ? text.saving : text.save}</button>
            {error && <p role="alert" className="error-copy">{error}</p>}
          </form>
          <details className="view-trash" onToggle={(event) => { if (event.currentTarget.open) onOpenTrash?.(); }}>
            <summary>{trash ? text.trash(trash.length) : text.trashTitle}</summary>
            {trash && (
              <ul>
                {trash.map((view) => (
                  <li key={view.id} className="view-row">
                    <span className="view-trash-name">{view.name}<small>{view.purges_at ? text.purges(formatDay(view.purges_at, timezone)) : null}</small></span>
                    <button type="button" aria-label={text.restoreLabel(view.name)}
                      onClick={() => { setTrashError(''); void onRestore?.(view).catch((failure) => setTrashError(refusal(failure))); }}>{text.restore}</button>
                  </li>))}
              </ul>
            )}
            {trashError && <p role="alert" className="error-copy">{trashError}</p>}
          </details>
        </div>
      </details>
      {deleted && (
        <p role="status" className="view-notice">{text.deleted(deleted.name)}
          <button type="button" className="link-button" onClick={undo}>{text.undo}</button></p>
      )}
      {undoError && <p role="alert" className="error-copy view-notice">{undoError}</p>}
    </>
  );
}
