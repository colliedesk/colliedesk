import { useEffect, useRef, useState } from 'react';
import { ChangedElsewhereError, InvalidRequestError } from '../../api/client';
import type { Macro, MacroActions } from '../../api/schemas';
import type { SettableStatus } from '../../api/schemas';
import { messages } from '../../messages';
import { HelpTip } from '../../ui/help-tip';
import { macroMessages } from './macro-messages';
import { Refused, useSaveChanges, useSettingsEdit } from './settings-edits';
import { settingsMessages } from './settings-messages';
import './saved-replies.css';

interface Person { id: string; display_name: string; email: string; deactivated_at: string | null }
interface Tag { id: string; name: string }
type Scope = 'shared' | 'personal';
type Priority = 'low' | 'normal' | 'high' | 'urgent';

interface Draft {
  title: string;
  body: string;
  scope: Scope;
  status: '' | SettableStatus;
  priority: '' | Priority;
  assignee: '' | 'none' | string;
  addTags: string[];
  removeTags: string[];
}

const emptyDraft = (): Draft => ({ title: '', body: '', scope: 'shared', status: '', priority: '', assignee: '', addTags: [], removeTags: [] });

function draftOf(macro: Macro): Draft {
  return {
    title: macro.title, body: macro.body, scope: macro.scope,
    status: macro.actions.status ?? '', priority: macro.actions.priority ?? '',
    assignee: macro.actions.set_assignee ? (macro.actions.assignee_id ?? 'none') : '',
    addTags: macro.actions.add_tag_ids ?? [], removeTags: macro.actions.remove_tag_ids ?? [],
  };
}

function actionsOf(draft: Draft): MacroActions {
  const actions: MacroActions = {};
  if (draft.status) actions.status = draft.status;
  if (draft.priority) actions.priority = draft.priority;
  if (draft.assignee === 'none') { actions.set_assignee = true; actions.assignee_id = null; }
  if (draft.assignee && draft.assignee !== 'none') { actions.set_assignee = true; actions.assignee_id = draft.assignee; }
  if (draft.addTags.length > 0) actions.add_tag_ids = draft.addTags;
  if (draft.removeTags.length > 0) actions.remove_tag_ids = draft.removeTags;
  return actions;
}

function summary(macro: Macro, people: Person[], tags: Tag[]): string {
  const parts: string[] = [];
  if (macro.actions.status) parts.push(macro.actions.status);
  if (macro.actions.priority) parts.push(macro.actions.priority);
  if (macro.actions.set_assignee) {
    const person = people.find((item) => item.id === macro.actions.assignee_id);
    parts.push(person ? (person.display_name || person.email) : macroMessages.unassigned);
  }
  const name = (id: string) => tags.find((tag) => tag.id === id)?.name ?? id;
  if (macro.actions.add_tag_ids?.length) parts.push(`+${macro.actions.add_tag_ids.map(name).join(', ')}`);
  if (macro.actions.remove_tag_ids?.length) parts.push(`−${macro.actions.remove_tag_ids.map(name).join(', ')}`);
  return parts.join(' · ');
}

interface MacrosSectionProps {
  macros: Macro[];
  trash: Macro[];
  people: Person[];
  tags: Tag[];
  onCreate: (value: { title: string; body: string; scope: Scope; actions: MacroActions }) => Promise<unknown>;
  onUpdate: (id: string, value: { title: string; body: string; scope: Scope; actions: MacroActions; updated_at: string }) => Promise<unknown>;
  onDelete: (id: string, updatedAt: string) => Promise<unknown>;
  onRestore: (id: string) => Promise<unknown>;
  onReload?: () => void;
}

// Every agent with macros permissions manages them. A personal macro is only listed for its owner.
export function MacrosSection({ macros, trash, people, tags, onCreate, onUpdate, onDelete, onRestore, onReload }: MacrosSectionProps) {
  const [draft, setDraft] = useState<Draft>(emptyDraft);
  const [editing, setEditing] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);
  const [confirming, setConfirming] = useState<string | null>(null);
  const [showTrash, setShowTrash] = useState(false);
  const [problem, setProblem] = useState<string>();
  const [stale, setStale] = useState(false);
  const [busy, setBusy] = useState(false);
  const running = useRef(false);
  const newButton = useRef<HTMLButtonElement>(null);
  const [returnFocus, setReturnFocus] = useState(false);
  useEffect(() => { if (returnFocus && !adding) { newButton.current?.focus(); setReturnFocus(false); } }, [returnFocus, adding]);
  const active = people.filter((person) => person.deactivated_at == null);
  const run = async (action: () => Promise<unknown>, done: () => void) => {
    if (running.current) return;
    running.current = true;
    setBusy(true);
    setProblem(undefined);
    setStale(false);
    try {
      await action();
      done();
    } catch (error) {
      setStale(error instanceof ChangedElsewhereError);
      setProblem(error instanceof InvalidRequestError ? error.message : macroMessages.actionFailed);
    } finally {
      running.current = false;
      setBusy(false);
    }
  };
  const saveChanges = useSaveChanges();
  const attempt = async (action: () => Promise<unknown>, done: () => void) => {
    let saved = false;
    await run(async () => { await action(); saved = true; }, done);
    if (!saved) throw new Error('refused');
  };
  const usable = (value: Draft) => value.title.trim() !== '' && (value.body.trim() !== '' || value.status !== '' || value.priority !== '' || value.assignee !== '' || value.addTags.length > 0 || value.removeTags.length > 0);
  const payload = (value: Draft) => ({ title: value.title, body: value.body, scope: value.scope, actions: actionsOf(value) });
  const open = adding || editing !== null;
  useSettingsEdit('macro', open && usable(draft) ? {
    label: editing ? (macros.find((item) => item.id === editing)?.title ?? draft.title) : macroMessages.newMacro,
    discard: () => { setAdding(false); setEditing(null); setDraft(emptyDraft()); setProblem(undefined); setReturnFocus(adding); },
    save: () => {
      if (!usable(draft)) { setProblem(macroMessages.needsTextOrAction); return Promise.reject(new Refused(macroMessages.needsTextOrAction)); }
      if (editing) {
        const current = macros.find((item) => item.id === editing);
        if (!current) return Promise.reject(new Refused(macroMessages.actionFailed));
        return attempt(() => onUpdate(current.id, { ...payload(draft), updated_at: current.updated_at }), () => { setEditing(null); setDraft(emptyDraft()); });
      }
      return attempt(() => onCreate(payload(draft)), () => { setAdding(false); setDraft(emptyDraft()); setReturnFocus(true); });
    },
  } : undefined);
  const toggleTag = (which: 'addTags' | 'removeTags', id: string) => setDraft((value) => ({
    ...value,
    [which]: value[which].includes(id) ? value[which].filter((item) => item !== id) : [...value[which], id],
  }));
  return (
    <section className="settings-card saved-replies" aria-labelledby="macros-heading">
      <div className="section-heading"><h2 id="macros-heading" className="sr-only">{messages.macros}</h2><p>{macroMessages.description}</p></div>
      <div className="settings-body">
        {problem && <p role="alert" className="error-copy">{problem}</p>}
        {stale && <div className="saved-reply-bar"><button type="button" onClick={() => { setEditing(null); setProblem(undefined); setStale(false); onReload?.(); }}>{settingsMessages.reload}</button></div>}
        {!open && <div className="saved-reply-bar">
          <button type="button" ref={newButton} className="new-saved-reply" onClick={() => { setProblem(undefined); setDraft(emptyDraft()); setAdding(true); }}>{macroMessages.newMacro}</button>
          <button type="button" aria-expanded={showTrash} onClick={() => setShowTrash((value) => !value)}>{showTrash ? macroMessages.closeTrash : macroMessages.trash}</button>
        </div>}
        {open && <form className="saved-reply-form" aria-label={editing ? macroMessages.editMacro(draft.title) : macroMessages.addMacro}
          onSubmit={(event) => { event.preventDefault(); if (!busy) void saveChanges(); }}>
          <label className="field"><span>{macroMessages.title}</span><input autoFocus value={draft.title} maxLength={200} onChange={(event) => setDraft({ ...draft, title: event.target.value })} /></label>
          <div className="field"><span className="heading-with-tip"><label htmlFor="macro-body">{macroMessages.text}</label><HelpTip label={macroMessages.text} text={macroMessages.placeholders} /></span><textarea id="macro-body" rows={4} value={draft.body} maxLength={20000} onChange={(event) => setDraft({ ...draft, body: event.target.value })} /></div>
          <p className="help">{macroMessages.textHint}</p>
          <fieldset><legend>{macroMessages.scope}</legend>
            <label><input type="radio" name="macro-scope" checked={draft.scope === 'shared'} onChange={() => setDraft({ ...draft, scope: 'shared' })} /> {macroMessages.shared}</label>
            <label><input type="radio" name="macro-scope" checked={draft.scope === 'personal'} onChange={() => setDraft({ ...draft, scope: 'personal' })} /> {macroMessages.personal}</label>
          </fieldset>
          <label className="field"><span>{macroMessages.status}</span>
            <select value={draft.status} onChange={(event) => setDraft({ ...draft, status: event.target.value as Draft['status'] })}>
              <option value="">{macroMessages.noChange}</option>
              <option value="open">Open</option><option value="pending">Pending</option><option value="resolved">Resolved</option><option value="closed">Closed</option>
            </select></label>
          <label className="field"><span>{macroMessages.priority}</span>
            <select value={draft.priority} onChange={(event) => setDraft({ ...draft, priority: event.target.value as Draft['priority'] })}>
              <option value="">{macroMessages.noChange}</option>
              <option value="low">Low</option><option value="normal">Normal</option><option value="high">High</option><option value="urgent">Urgent</option>
            </select></label>
          <label className="field"><span>{macroMessages.assignee}</span>
            <select value={draft.assignee} onChange={(event) => setDraft({ ...draft, assignee: event.target.value })}>
              <option value="">{macroMessages.noChange}</option>
              <option value="none">{macroMessages.unassigned}</option>
              {active.map((person) => <option key={person.id} value={person.id}>{person.display_name || person.email}</option>)}
            </select></label>
          {tags.length === 0 ? <p className="help">{macroMessages.noTags}</p> : <>
            <fieldset><legend>{macroMessages.addTags}</legend>{tags.map((tag) => <label key={tag.id}><input type="checkbox" checked={draft.addTags.includes(tag.id)} onChange={() => toggleTag('addTags', tag.id)} /> {tag.name}</label>)}</fieldset>
            <fieldset><legend>{macroMessages.removeTags}</legend>{tags.map((tag) => <label key={tag.id}><input type="checkbox" checked={draft.removeTags.includes(tag.id)} onChange={() => toggleTag('removeTags', tag.id)} /> {tag.name}</label>)}</fieldset>
          </>}
          <div className="saved-reply-actions"><button type="button" disabled={busy} onClick={() => { setAdding(false); setEditing(null); setDraft(emptyDraft()); setProblem(undefined); setReturnFocus(true); }}>{messages.cancel}</button></div>
        </form>}
        {showTrash && (trash.length === 0 ? <p className="empty-copy">{macroMessages.trashEmpty}</p> : <ul className="saved-reply-list">{trash.map((macro) => (
          <li key={macro.id}><strong>{macro.title}</strong> <button type="button" onClick={() => void run(() => onRestore(macro.id), () => undefined)}>{macroMessages.restore(macro.title)}</button></li>
        ))}</ul>)}
        {!open && (macros.length === 0 ? <p className="empty-copy">{macroMessages.none}</p> : <ul className="saved-reply-list">{macros.map((macro) => (
          <li key={macro.id}>
            <strong>{macro.title}</strong> <span>{macro.scope === 'personal' ? macroMessages.personalBadge : macroMessages.sharedBadge}</span>
            {summary(macro, people, tags) && <p>{summary(macro, people, tags)}</p>}
            <div className="saved-reply-actions">
              <button type="button" onClick={() => { setProblem(undefined); setDraft(draftOf(macro)); setEditing(macro.id); setAdding(false); }}>{messages.edit}</button>
              {confirming === macro.id
                ? <button type="button" disabled={busy} onClick={() => void run(() => onDelete(macro.id, macro.updated_at), () => setConfirming(null))}>{macroMessages.confirmDelete}</button>
                : <button type="button" onClick={() => setConfirming(macro.id)}>{macroMessages.delete(macro.title)}</button>}
            </div>
          </li>
        ))}</ul>)}
      </div>
    </section>
  );
}
