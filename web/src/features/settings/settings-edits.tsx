import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { useBlocker, useRouter } from '@tanstack/react-router';
import { settingsMessages } from './settings-messages';
import { SaveBar, SavedToast } from './settings-save-bar';

// One save model for the Settings pages. A choice (a toggle, a pick from a list) saves when it is made and says so
// with a toast that can undo it (useSettingsSaved). Text is typed first and saved together: each changed field
// registers itself (useSettingsEdit), the page shows one bar naming them with Discard and Save (Ctrl/Cmd+S), and
// leaving the page with text not yet saved asks once.

export interface SettingsEdit {
  // label names the field in the bar.
  label: string;
  // save resolves when the server took the value and rejects when it refused it; the field shows the reason.
  save: () => Promise<unknown> | unknown;
  discard: () => void;
}

interface SettingsEdits {
  register: (id: string, edit: SettingsEdit | undefined) => void;
  announce: (undo?: () => unknown) => void;
  save: () => Promise<void>;
}

// Outside a provider (a section drawn alone) nothing is collected; the field still edits, and nothing saves it.
const Edits = createContext<SettingsEdits>({ register: () => undefined, announce: () => undefined, save: () => Promise.resolve() });

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

// useSettingsSaved tells the person a choice was saved. undo runs the one request that puts the previous value back.
export function useSettingsSaved(): (undo?: () => unknown) => void {
  return useContext(Edits).announce;
}

// useInstantSave saves a choice when it is made: save(next) runs, then the page says Saved with an Undo that runs
// save(previous), one request. A refused save says nothing here; the control shows the reason it was given.
export function useInstantSave() {
  const saved = useSettingsSaved();
  return useCallback(async <T,>(save: (value: T) => Promise<unknown> | void, next: T, previous: T) => {
    try { await save(next); } catch { return; }
    saved(() => save(previous));
  }, [saved]);
}

// useSaveChanges runs the Save of the bar, for a field that saves on Enter.
export function useSaveChanges(): () => Promise<void> {
  return useContext(Edits).save;
}

// How long the Saved toast stays when nobody dismisses it.
const toastMilliseconds = 8000;
const isSave = (event: KeyboardEvent) => (event.metaKey || event.ctrlKey) && !event.altKey && !event.shiftKey && event.key.toLowerCase() === 's';

export function SettingsEditsProvider({ children }: { children: ReactNode }) {
  const edits = useRef(new Map<string, SettingsEdit>());
  const [labels, setLabels] = useState<string[]>([]);
  const [saving, setSaving] = useState(false);
  const [toast, setToast] = useState<{ undo?: () => unknown }>();
  const running = useRef(false);
  const register = useCallback((id: string, edit: SettingsEdit | undefined) => {
    if (edit) edits.current.set(id, edit); else edits.current.delete(id);
    setLabels([...edits.current.values()].map((item) => item.label));
  }, []);
  // The fields are saved one after the other: some pages keep one document that each save sends whole. A refusal
  // stops the run; that field says why and every field keeps what was typed.
  const save = useCallback(async () => {
    if (running.current) return;
    running.current = true;
    setSaving(true);
    try {
      for (const edit of [...edits.current.values()]) await edit.save();
    } catch { /* the refused field shows its reason */ } finally {
      running.current = false;
      setSaving(false);
    }
  }, []);
  const discard = useCallback(() => { for (const edit of [...edits.current.values()]) edit.discard(); }, []);
  const announce = useCallback((undo?: () => unknown) => setToast({ undo }), []);
  const changed = labels.length > 0;
  const dirty = useRef(changed);
  dirty.current = changed;

  useEffect(() => {
    if (!changed) return;
    const onKey = (event: KeyboardEvent) => { if (isSave(event)) { event.preventDefault(); void save(); } };
    const onUnload = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = ''; };
    document.addEventListener('keydown', onKey);
    window.addEventListener('beforeunload', onUnload);
    return () => { document.removeEventListener('keydown', onKey); window.removeEventListener('beforeunload', onUnload); };
  }, [changed, save]);
  useEffect(() => {
    if (!toast) return;
    const timer = setTimeout(() => setToast(undefined), toastMilliseconds);
    return () => clearTimeout(timer);
  }, [toast]);

  const value = useMemo(() => ({ register, announce, save }), [register, announce, save]);
  // Unit tests draw sections without a router; there is nowhere to leave to.
  const routed = Boolean(useRouter({ warn: false }));
  return <Edits.Provider value={value}>
    {children}
    {routed && <LeaveGuard dirty={dirty} />}
    {changed && <SaveBar labels={labels} saving={saving} onDiscard={discard} onSave={() => void save()} />}
    {toast && <SavedToast onUndo={toast.undo && (() => { const { undo } = toast; setToast(undefined); void undo?.(); })} onDismiss={() => setToast(undefined)} />}
  </Edits.Provider>;
}

// LeaveGuard asks before the router opens another page (a menu link, Back to app, Esc, the browser's Back) while
// text is unsaved. A link to a section of the same page keeps the text and asks nothing.
function LeaveGuard({ dirty }: { dirty: { current: boolean } }) {
  useBlocker({
    shouldBlockFn: ({ current, next }) => dirty.current
      && (current.pathname !== next.pathname || JSON.stringify(current.search) !== JSON.stringify(next.search))
      && !window.confirm(settingsMessages.leaveUnsaved),
    enableBeforeUnload: false,
  });
  return null;
}
