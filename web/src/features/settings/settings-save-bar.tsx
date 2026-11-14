import { useEffect, useRef } from 'react';
import { messages } from '../../messages';
import { XIcon } from '../../ui/icons';
import { settingsMessages } from './settings-messages';
import './settings-save.css';

// The key the bar names: Command on Apple systems, Ctrl elsewhere.
const shortcut = /Mac|iPhone|iPad/.test(typeof navigator === 'undefined' ? '' : navigator.platform) ? '⌘S' : 'Ctrl S';

interface SaveBarProps { labels: string[]; saving: boolean; failure?: { label: string; message: string }; onDiscard: () => void; onSave: () => void }

// SaveBar floats at the foot of the page while text is unsaved: it names what changed, and never moves the page.
export function SaveBar({ labels, saving, failure, onDiscard, onSave }: SaveBarProps) {
  const bar = useRef<HTMLElement>(null);
  // The bar grows with long field names and a refusal; the toast stacks above whatever height it has.
  useEffect(() => {
    const node = bar.current;
    if (!node) return;
    const root = document.documentElement;
    const publish = () => root.style.setProperty('--settings-save-bar-height', `${node.offsetHeight}px`);
    publish();
    const observer = typeof ResizeObserver === 'undefined' ? undefined : new ResizeObserver(publish);
    observer?.observe(node);
    return () => { observer?.disconnect(); root.style.removeProperty('--settings-save-bar-height'); };
  }, []);
  return <section ref={bar} className="settings-save-bar" aria-label={settingsMessages.unsavedChanges}>
    <p><span className="settings-save-dot" aria-hidden="true" /><strong>{settingsMessages.unsavedChanges}</strong><span> · {labels.join(', ')}</span></p>
    {failure && <p className="settings-save-failure">{failure.label}: {failure.message}</p>}
    <button type="button" disabled={saving} onClick={onDiscard}>{settingsMessages.discard}</button>
    <button type="button" className="primary-action" disabled={saving} aria-keyshortcuts="Control+S Meta+S" onClick={onSave}>
      {saving ? settingsMessages.saving : settingsMessages.save}{!saving && <kbd aria-hidden="true">{shortcut}</kbd>}</button>
  </section>;
}

// SavedToast says a choice was saved at once; Undo, when the page gave one, puts the previous value back. A refused
// Undo says so here and keeps the button. raised lifts it above the save bar when both are on screen; inSheet draws it in
// a side sheet's own slot, in the sheet's tab order and above its footer, instead of over the page.
export function SavedToast({ onUndo, onDismiss, error, raised, inSheet }: { onUndo?: () => void; onDismiss: () => void; error?: string; raised?: boolean; inSheet?: boolean }) {
  return <div className={`settings-toast${raised ? ' settings-toast-raised' : ''}${inSheet ? ' settings-toast-in-sheet' : ''}`} role="status">
    {error ? <span role="alert">{error}</span> : <span>{settingsMessages.saved}</span>}
    {onUndo && <button type="button" onClick={onUndo}>{messages.undo}</button>}
    <button type="button" className="settings-toast-close" aria-label={messages.dismiss} onClick={onDismiss}><XIcon /></button>
  </div>;
}
