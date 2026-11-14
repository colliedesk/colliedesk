import { messages } from '../../messages';
import { XIcon } from '../../ui/icons';
import { settingsMessages } from './settings-messages';
import './settings-save.css';

// The key the bar names: Command on Apple systems, Ctrl elsewhere.
const shortcut = /Mac|iPhone|iPad/.test(typeof navigator === 'undefined' ? '' : navigator.platform) ? '⌘S' : 'Ctrl S';

interface SaveBarProps { labels: string[]; saving: boolean; onDiscard: () => void; onSave: () => void }

// SaveBar floats at the foot of the page while text is unsaved: it names what changed, and never moves the page.
export function SaveBar({ labels, saving, onDiscard, onSave }: SaveBarProps) {
  return <section className="settings-save-bar" aria-label={settingsMessages.unsavedChanges}>
    <p><span className="settings-save-dot" aria-hidden="true" /><strong>{settingsMessages.unsavedChanges}</strong><span> · {labels.join(', ')}</span></p>
    <button type="button" disabled={saving} onClick={onDiscard}>{settingsMessages.discard}</button>
    <button type="button" className="primary-action" disabled={saving} aria-keyshortcuts="Control+S Meta+S" onClick={onSave}>
      {saving ? settingsMessages.saving : settingsMessages.save}{!saving && <kbd aria-hidden="true">{shortcut}</kbd>}</button>
  </section>;
}

// SavedToast says a choice was saved at once; Undo, when the page gave one, puts the previous value back. A refused
// Undo says so here and keeps the button. raised lifts it above the save bar when both are on screen.
export function SavedToast({ onUndo, onDismiss, error, raised }: { onUndo?: () => void; onDismiss: () => void; error?: string; raised?: boolean }) {
  return <div className={`settings-toast${raised ? ' settings-toast-raised' : ''}`} role="status">
    {error ? <span role="alert">{error}</span> : <span>{settingsMessages.saved}</span>}
    {onUndo && <button type="button" onClick={onUndo}>{messages.undo}</button>}
    <button type="button" className="settings-toast-close" aria-label={messages.dismiss} onClick={onDismiss}><XIcon /></button>
  </div>;
}
