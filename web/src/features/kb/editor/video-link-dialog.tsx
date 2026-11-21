import { useId, useRef, useState } from 'react';
import { HelpTip } from '../../../ui/help-tip';
import { kbMessages } from '../kb-messages';
import { KBDialog } from '../kb-ui';
import { embedSnippet, parseVideoLink } from '../video-link';

// VideoLinkDialog asks for a YouTube or Vimeo link and gives the player's HTML, or says why the link cannot be used.
export function VideoLinkDialog({ onClose, onInsert }: { onClose: () => void; onInsert: (snippet: string) => void }) {
  const id = useId();
  const videoLink = useRef<HTMLInputElement>(null);
  const [error, setError] = useState('');
  const t = kbMessages.tools;
  const insert = () => {
    const parsed = parseVideoLink(videoLink.current?.value ?? '');
    if ('error' in parsed) {
      setError(parsed.error);
      videoLink.current?.focus();
      return;
    }
    onInsert(embedSnippet(parsed));
  };
  return (
    <KBDialog title={t.videoLink} closeLabel={kbMessages.close} returnFocus={false} onClose={onClose} footer={<>
      <button type="button" onClick={onClose}>{kbMessages.cancel}</button>
      <button type="button" className="primary-action" onClick={insert}>{kbMessages.insert}</button>
    </>}>
      <div className="field md-video">
        <label htmlFor={`${id}-video-link`}>{kbMessages.videoLink}</label>
        <input ref={videoLink} id={`${id}-video-link`} type="url" inputMode="url" autoComplete="off" placeholder="https://www.youtube.com/watch?v=…"
          aria-invalid={error ? true : undefined} aria-describedby={error ? `${id}-video-help` : `${id}-video-help ${id}-video-tip`} onChange={() => { if (error) setError(''); }}
          onKeyDown={(event) => { if (event.key === 'Enter') { event.preventDefault(); insert(); } }} />
        <small className={error ? 'error-copy' : 'kb-hint'} role={error ? 'alert' : undefined}>
          <span id={`${id}-video-help`}>{error || kbMessages.videoLinkHelp}</span>
          {!error && <>{' '}<HelpTip label={kbMessages.videoLink} text={kbMessages.videoLinkTip} id={`${id}-video-tip`} /></>}
        </small>
      </div>
    </KBDialog>
  );
}
