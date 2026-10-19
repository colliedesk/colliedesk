import { useEffect, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { savedViewsApi } from '../../api/saved-views';
import { InvalidRequestError } from '../../api/client';
import type { ConversationFilters, SavedView } from '../../api/schemas';
import { formatDay } from '../../lib/format-date';
import { sameQuery } from '../../lib/inbox-query';
import { filtersOf, queryOf } from '../../lib/inbox-query-edit';
import type { InboxQuery } from '../../lib/inbox-query';
import { messages } from '../../messages';
import { savedViewMessages } from './filter-messages';
import { BookmarkIcon } from '../../ui/icons';

export interface SavedViewChange {
  name?: string;
  scope?: SavedView['scope'];
  filters?: ConversationFilters;
}

interface SavedViewsMenuProps {
  views: SavedView[];
  trash?: SavedView[];
  // The filters on screen: the view they match is current, and saving stores them.
  query: InboxQuery;
  timezone: string;
  onApply?: (query: InboxQuery) => void;
  onCreate?: (view: { name: string; scope: SavedView['scope']; filters: ConversationFilters }) => Promise<unknown>;
  onUpdate?: (view: SavedView, change: SavedViewChange) => Promise<unknown>;
  // A delete moves the view to the trash; onRestore takes it out again.
  onDelete?: (view: SavedView) => Promise<unknown>;
  onRestore?: (view: SavedView) => Promise<unknown>;
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

interface ViewRowProps extends Pick<SavedViewsMenuProps, 'onApply' | 'onUpdate'> {
  view: SavedView;
  current: boolean;
  query: InboxQuery;
  onDelete: (view: SavedView) => void;
}

function ViewRow({ view, current, query, onApply, onUpdate, onDelete }: ViewRowProps) {
  const actions = useRef<HTMLDetailsElement>(null);
  const [renaming, setRenaming] = useState(false);
  const [name, setName] = useState(view.name);
  const [error, setError] = useState('');
  const run = async (change: SavedViewChange) => {
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
      <button type="button" className="view-open" aria-current={current || undefined} onClick={() => onApply?.(queryOf(view.filters))}>{view.name}</button>
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
              <button type="button" className="menu-item" onClick={() => void run({ filters: filtersOf(query) })}>{text.updateFilters}</button>
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

// SavedViewsMenu loads the views and their trash and keeps them current after each change. It is a lazy
// part of the inbox: the views are only needed once the person looks at them.
export function SavedViewsMenu({ query, timezone, onApply }: Pick<SavedViewsMenuProps, 'query' | 'timezone' | 'onApply'>) {
  const client = useQueryClient();
  const [opened, setOpened] = useState(false);
  const [trashOpened, setTrashOpened] = useState(false);
  const views = useQuery({ queryKey: ['saved-views'], queryFn: savedViewsApi.list, enabled: opened });
  const trash = useQuery({ queryKey: ['saved-views', 'trash'], queryFn: savedViewsApi.trash, enabled: trashOpened });
  const changed = () => client.invalidateQueries({ queryKey: ['saved-views'] });
  const create = useMutation({ mutationFn: (view: Parameters<typeof savedViewsApi.create>[0]) => savedViewsApi.create(view), onSettled: changed });
  // A change carries the version it was made on; someone else's save since is refused (409), not overwritten.
  const update = useMutation({ mutationFn: ({ view, change }: { view: SavedView; change: SavedViewChange }) => savedViewsApi.update(view.id, { ...change, updated_at: view.updated_at }),
    onSettled: changed });
  const remove = useMutation({ mutationFn: (view: SavedView) => savedViewsApi.remove(view.id), onSettled: changed });
  const restore = useMutation({ mutationFn: (view: SavedView) => savedViewsApi.restore(view.id), onSettled: changed });
  return <SavedViewsMenuView views={views.data?.items ?? []} trash={trash.data?.items} query={query} timezone={timezone} onApply={onApply}
    onOpen={() => setOpened(true)} onOpenTrash={() => setTrashOpened(true)}
    onCreate={(view) => create.mutateAsync(view)} onUpdate={(view, change) => update.mutateAsync({ view, change })}
    onDelete={(view) => remove.mutateAsync(view)} onRestore={(view) => restore.mutateAsync(view)} />;
}

// SavedViewsMenuView keeps named sets of inbox filters: the team's shared views and the person's own.
// Choosing one sets its filters, which the address then carries.
export function SavedViewsMenuView({ views, trash, query, timezone, onApply, onCreate, onUpdate, onDelete, onRestore, onOpen, onOpenTrash }: SavedViewsMenuProps) {
  const menu = useRef<HTMLDetailsElement>(null);
  const [name, setName] = useState('');
  const [scope, setScope] = useState<SavedView['scope']>('personal');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [deleted, setDeleted] = useState<SavedView>();
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
      await onCreate({ name: name.trim(), scope, filters: filtersOf(query) });
      setName('');
    } catch (failure) {
      setError(refusal(failure));
    } finally {
      setBusy(false);
    }
  };
  const remove = (view: SavedView) => {
    void onDelete?.(view).then(() => setDeleted(view), () => setError(text.failed));
  };
  const undo = () => {
    if (!deleted) return;
    const view = deleted;
    setDeleted(undefined);
    setUndoError('');
    void onRestore?.(view).catch((failure) => setUndoError(refusal(failure)));
  };
  const isCurrent = (view: SavedView) => sameQuery(queryOf(view.filters), query);
  const groups = [['shared', text.shared], ['personal', text.personal]] as const;
  return (
    <>
      <details className="filter-menu views-menu" ref={menu} onKeyDown={closeOnEscape} onBlur={closeOnFocusOut}
        onToggle={(event) => { if (event.currentTarget.open) onOpen?.(); }}>
        <summary><BookmarkIcon /><span>{text.title}</span></summary>
        <div className="filter-popover views-popover" role="group" aria-label={text.title}>
          {views.length === 0 && <p className="filter-hint">{text.none}</p>}
          {groups.map(([group, label]) => {
            const items = views.filter((view) => view.scope === group);
            return items.length > 0 && (
              <section key={group} className="view-group"><h3 id={`views-${group}`}>{label}</h3>
                <ul aria-labelledby={`views-${group}`}>
                  {items.map((view) => <ViewRow key={view.id} view={view} current={isCurrent(view)} query={query} onApply={onApply} onUpdate={onUpdate} onDelete={remove} />)}
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
