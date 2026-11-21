import type { EditorView } from '@codemirror/view';
import { kbMessages } from '../kb-messages';
import { Icon } from './icons';
import { switchPaste } from './paste-formatted';
import type { Pasted } from './paste-formatted';

// PasteToast offers the last formatted paste both ways, Markdown or plain text: one click writes it the other
// way. It goes once the person types on or dismisses it.
export function PasteToast({ pasted, view, onChange }: { pasted: Pasted; view?: EditorView; onChange: (next?: Pasted) => void }) {
  const t = kbMessages.tools;
  const show = (shown: Pasted['shown']) => {
    if (!view || shown === pasted.shown) return;
    onChange(switchPaste(view, pasted));
    view.focus();
  };
  return (
    <div className="kb-toast md-paste" role="group" aria-label={t.pasted}>
      <button type="button" aria-pressed={pasted.shown === 'formatted'} onMouseDown={(event) => event.preventDefault()}
        onClick={() => show('formatted')}>{t.keepFormatting}</button>
      <span aria-hidden="true">·</span>
      <button type="button" aria-pressed={pasted.shown === 'plain'} onMouseDown={(event) => event.preventDefault()}
        onClick={() => show('plain')}>{t.pastePlain}</button>
      <button type="button" className="kb-toast-close" aria-label={kbMessages.dismiss} onClick={() => onChange(undefined)}><Icon name="close" /></button>
    </div>
  );
}
