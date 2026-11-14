import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { useBlocker, useRouter } from '@tanstack/react-router';
import { InvalidRequestError } from '../../api/client';
import { settingsMessages } from './settings-messages';
import { SaveBar, SavedToast } from './settings-save-bar';
import { registerUnsavedProbe } from './unsaved-probe';

// One save model for the Settings pages. A choice (a toggle, a pick from a list) saves when it is made and says so
// with a toast that can undo it (useInstantSave). Text is typed first and saved together: each changed field
// registers itself (useSettingsEdit, or useSettingsDraft where the text must outlive the panel that shows it), the
// page shows one bar naming them with Discard and Save (Ctrl/Cmd+S), and leaving the page with text not yet saved
// asks once. Text that has its own button (a step that adds something) counts for that question too
// (useUnsavedText). Moving between the tabs of one page is not leaving it.

// Refused is what a field's save rejects with when it has words for the person (a value it will not send): the bar says them.
export class Refused extends Error {}

export interface SettingsEdit {
  // label names the field in the bar.
  label: string;
  // save resolves when the server took the value and rejects when it refused it; the field shows the reason.
  save: () => Promise<unknown> | unknown;
  discard: () => void;
}

interface DraftEntry { value: unknown; label: string; save: (value: never) => Promise<unknown> | unknown }

interface SettingsEdits {
  register: (id: string, edit: SettingsEdit | undefined) => void;
  announce: (undo?: () => unknown) => void;
  save: () => Promise<void>;
  // drafts is the page's store of typed text that outlives its panel; undefined where no page holds one.
  drafts?: Map<string, DraftEntry>;
  touch: () => void;
  guard: (id: string, on: boolean) => void;
  dropDrafts: (prefix: string) => void;
  isUnsaved: (id: string) => boolean;
  releaseGuards: () => void;
  version?: number;
}

// Outside a provider (a section drawn alone) nothing is collected; the field still edits, and nothing saves it.
const Edits = createContext<SettingsEdits>({ register: () => undefined, announce: () => undefined, save: () => Promise.resolve(), touch: () => undefined, guard: () => undefined, dropDrafts: () => undefined, isUnsaved: () => false, releaseGuards: () => undefined });

// useSettingsEdit registers one field while it differs from what is saved: pass undefined while it does not.
export function useSettingsEdit(id: string, edit: SettingsEdit | undefined): void {
  const { register } = useContext(Edits);
  const latest = useRef(edit);
  latest.current = edit;
  const label = edit?.label;
  useEffect(() => {
    if (label === undefined) return;
    register(id, { label, save: () => latest.current?.save(), discard: () => latest.current?.discard() });
    return () => register(id, undefined);
  }, [id, label, register]);
}

// useSettingsDraft keeps typed text in the page, not in the panel that shows it: a tab switch unmounts the panel, and
// the text, the bar and the question on leaving all stay. The value is the saved one until something differs;
// typing back to the saved value clears the draft. Save sends the draft (save) and drops it once it is accepted.
export function useSettingsDraft<T>(id: string, saved: T, { label, save }: { label: string; save: (value: T) => Promise<unknown> | unknown }): [T, (value: T) => void] {
  const { drafts, touch } = useContext(Edits);
  const [local, setLocal] = useState<{ value: T }>();
  const entry = drafts?.get(id);
  useEffect(() => { if (entry) entry.save = save as DraftEntry['save']; });
  const value = (drafts ? (entry ? entry.value : saved) : local ? local.value : saved) as T;
  const set = (next: T) => {
    const same = JSON.stringify(next) === JSON.stringify(saved);
    if (!drafts) setLocal(same ? undefined : { value: next });
    else if (same) drafts.delete(id);
    else drafts.set(id, { value: next, label, save: save as DraftEntry['save'] });
    touch();
  };
  return [value, set];
}

// useDraftIds lists the drafts the page holds whose id starts with prefix: a panel that opens again finds what was typed.
export function useDraftIds(prefix: string): string[] {
  const { drafts } = useContext(Edits);
  return drafts ? [...drafts.keys()].filter((id) => id.startsWith(prefix)) : [];
}

// useDropDrafts throws away the page's drafts whose id starts with prefix, once the person agreed to lose them.
export function useDropDrafts(): (prefix: string) => void {
  return useContext(Edits).dropDrafts;
}

// useReleaseUnsaved is for a step that saves and then opens the next page itself: call it just before, so the text that was
// being saved is not asked about on the way out.
export function useReleaseUnsaved(): () => void {
  return useContext(Edits).releaseGuards;
}

// useUnsavedText marks text that is typed and has its own button (adding a holiday, a support address, an invite) as
// unsaved for the question on leaving the page and for closing the tab. It draws no bar.
export function useUnsavedText(id: string, typed: boolean): void {
  const { guard } = useContext(Edits);
  useEffect(() => {
    guard(id, typed);
    return () => guard(id, false);
  }, [id, typed, guard]);
}

// useConfirmDrop is for an action inside the page that would drop typed text (opening another row's editor, turning
// the page's cards off): it asks once, and says whether to go on. ids are the fields that action would drop.
export function useConfirmDrop(): (...ids: string[]) => boolean {
  const { isUnsaved } = useContext(Edits);
  return (...ids) => !ids.some(isUnsaved) || window.confirm(settingsMessages.leaveUnsaved);
}

// useSettingsSaved tells the person a choice was saved. undo runs the one request that puts the previous value back.
export function useSettingsSaved(): (undo?: () => unknown) => void {
  return useContext(Edits).announce;
}

// useInstantSave saves a choice when it is made: save(next) runs, then the page says Saved with an Undo that runs
// save(previous), one request. A refused save says nothing here; the control shows the reason it was given.
export function useInstantSave() {
  const saved = useSettingsSaved();
  return useCallback(async <T,>(save: (value: T) => Promise<unknown> | void, next: T, previous: T, undo: (value: T) => Promise<unknown> | void = save) => {
    try { await save(next); } catch { return; }
    saved(() => undo(previous));
  }, [saved]);
}

// useSaveChanges runs the Save of the bar, for a field that saves on Enter.
export function useSaveChanges(): () => Promise<void> {
  return useContext(Edits).save;
}

// How long the Saved toast stays when nobody dismisses it.
const toastMilliseconds = 8000;
const isSave = (event: KeyboardEvent) => (event.metaKey || event.ctrlKey) && !event.altKey && !event.shiftKey && event.key.toLowerCase() === 's';
// Whatever is open over the page owns the keys, as it owns Esc.
const dialogOpen = () => Boolean(document.querySelector('dialog[open], [role="dialog"], [role="alertdialog"], [aria-modal="true"]'));
// The page's heading takes the focus when the bar goes: the body would hand Esc the power to leave Settings.
function focusHeading() {
  const heading = document.querySelector<HTMLElement>('h1');
  if (!heading) return;
  if (!heading.hasAttribute('tabindex')) heading.setAttribute('tabindex', '-1');
  heading.focus({ preventScroll: true });
}

export function SettingsEditsProvider({ children }: { children: ReactNode }) {
  const edits = useRef(new Map<string, SettingsEdit>());
  const drafts = useRef(new Map<string, DraftEntry>());
  const guards = useRef(new Set<string>());
  // version changes whenever a draft or a guard does, so the fields that read the page's store draw again.
  const [version, setVersion] = useState(0);
  const touch = useCallback(() => setVersion((n) => n + 1), []);
  const [saving, setSaving] = useState(false);
  const [toast, setToast] = useState<{ undo?: () => unknown; error?: string }>();
  // Why the last Save stopped, with the name of the field: the field may be on a tab that is not shown.
  const [failure, setFailure] = useState<{ label: string; message: string }>();
  const running = useRef(false);
  const register = useCallback((id: string, edit: SettingsEdit | undefined) => {
    if (edit) edits.current.set(id, edit); else edits.current.delete(id);
    touch();
  }, [touch]);
  const guard = useCallback((id: string, on: boolean) => {
    if (on === guards.current.has(id)) return;
    if (on) guards.current.add(id); else guards.current.delete(id);
    touch();
  }, [touch]);
  const dropDrafts = useCallback((prefix: string) => {
    for (const id of [...drafts.current.keys()]) if (id.startsWith(prefix)) drafts.current.delete(id);
    touch();
  }, [touch]);
  const releaseGuards = useCallback(() => { guards.current.clear(); touch(); }, [touch]);
  const isUnsaved = useCallback((id: string) => edits.current.has(id) || drafts.current.has(id) || guards.current.has(id), []);
  // The fields are saved one after the other: some pages keep one document that each save sends whole. A refusal
  // stops the run; that field says why and every field keeps what was typed.
  const save = useCallback(async () => {
    if (running.current) return;
    running.current = true;
    setSaving(true);
    setFailure(undefined);
    let label = '';
    try {
      for (const edit of [...edits.current.values()]) { label = edit.label; await edit.save(); }
      for (const [id, draft] of [...drafts.current]) {
        label = draft.label;
        await (draft.save as (value: unknown) => unknown)(draft.value);
        drafts.current.delete(id);
        touch();
      }
      focusHeading();
    } catch (error) {
      setFailure({ label, message: error instanceof Refused || error instanceof InvalidRequestError ? error.message : settingsMessages.saveFailed });
    } finally {
      running.current = false;
      setSaving(false);
    }
  }, [touch]);
  const discard = useCallback(() => {
    for (const edit of [...edits.current.values()]) edit.discard();
    drafts.current.clear();
    setFailure(undefined);
    touch();
    focusHeading();
  }, [touch]);
  const announce = useCallback((undo?: () => unknown) => setToast({ undo }), []);
  const undoToast = async () => {
    const undo = toast?.undo;
    try {
      await undo?.();
      setToast(undefined);
    } catch (error) {
      setToast({ undo, error: error instanceof InvalidRequestError ? error.message : settingsMessages.undoFailed });
    }
  };
  const labels = [...edits.current.values(), ...drafts.current.values()].map((item) => item.label);
  const changed = labels.length > 0;
  const unsaved = changed || guards.current.size > 0;
  const dirty = useRef(unsaved);
  dirty.current = unsaved;
  useEffect(() => registerUnsavedProbe(() => dirty.current), []);

  useEffect(() => {
    if (!changed) return;
    const onKey = (event: KeyboardEvent) => { if (isSave(event) && !dialogOpen()) { event.preventDefault(); void save(); } };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [changed, save]);
  useEffect(() => {
    if (!unsaved) return;
    const onUnload = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = ''; };
    window.addEventListener('beforeunload', onUnload);
    return () => window.removeEventListener('beforeunload', onUnload);
  }, [unsaved]);
  useEffect(() => {
    if (!toast || toast.error) return;
    const timer = setTimeout(() => setToast(undefined), toastMilliseconds);
    return () => clearTimeout(timer);
  }, [toast]);

  const value = useMemo(() => ({ register, announce, save, drafts: drafts.current, touch, guard, dropDrafts, isUnsaved, releaseGuards, version }), [register, announce, save, touch, guard, dropDrafts, isUnsaved, releaseGuards, version]);
  // Unit tests draw sections without a router; there is nowhere to leave to.
  const routed = Boolean(useRouter({ warn: false }));
  return <Edits.Provider value={value}>
    {children}
    {routed && <LeaveGuard dirty={dirty} />}
    <p className="sr-only" aria-live="polite">{changed ? `${settingsMessages.unsavedChanges}: ${labels.join(', ')}.${failure ? ` ${failure.label}: ${failure.message}` : ''}` : ''}</p>
    {changed && <SaveBar labels={labels} saving={saving} failure={changed ? failure : undefined} onDiscard={discard} onSave={() => void save()} />}
    {toast && <SavedToast raised={changed} error={toast.error} onUndo={toast.undo && (() => void undoToast())} onDismiss={() => setToast(undefined)} />}
  </Edits.Provider>;
}

// LeaveGuard asks before the router opens another page (a menu link, Back to app, Esc, the browser's Back) while
// text is unsaved. A link to a section of the same page, or another tab of it (?tab=), is not leaving.
function LeaveGuard({ dirty }: { dirty: { current: boolean } }) {
  useBlocker({
    shouldBlockFn: ({ current, next }) => dirty.current && current.pathname !== next.pathname && !window.confirm(settingsMessages.leaveUnsaved),
    enableBeforeUnload: false,
  });
  return null;
}
