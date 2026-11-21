import { useId, useRef, useState } from 'react';
import { kbMessages } from '../../kb-messages';
import { KBDialog } from '../../kb-ui';

// An address a link can lead to: a web page, an email address, a page of this site or a place in the article.
const address = /^(https?:\/\/\S+|mailto:\S+|\/\S*|#\S+)$/i;

// LinkDialog asks for the address of a link in the visual editor (the Markdown source types [text](address)).
// current is the address of the link the cursor is in, which Remove link takes away.
export function LinkDialog({ current, onSet, onRemove, onClose }: {
  current: string; onSet: (href: string) => void; onRemove: () => void; onClose: () => void;
}) {
  const id = useId();
  const field = useRef<HTMLInputElement>(null);
  const [invalid, setInvalid] = useState(false);
  const set = () => {
    const href = (field.current?.value ?? '').trim();
    if (!address.test(href)) {
      setInvalid(true);
      field.current?.focus();
      return;
    }
    onSet(href);
  };
  return (
    <KBDialog title={kbMessages.tools.link} closeLabel={kbMessages.close} returnFocus={false} onClose={onClose} footer={<>
      {current && <button type="button" onClick={onRemove}>{kbMessages.removeLink}</button>}
      <button type="button" onClick={onClose}>{kbMessages.cancel}</button>
      <button type="button" className="primary-action" onClick={set}>{kbMessages.insert}</button>
    </>}>
      <div className="field md-video">
        <label htmlFor={`${id}-address`}>{kbMessages.linkAddress}</label>
        <input ref={field} id={`${id}-address`} type="url" inputMode="url" autoComplete="off" spellCheck={false} defaultValue={current} placeholder="https://"
          aria-invalid={invalid ? true : undefined} aria-describedby={`${id}-help`} onChange={() => setInvalid(false)}
          onKeyDown={(event) => { if (event.key === 'Enter') { event.preventDefault(); set(); } }} />
        <small id={`${id}-help`} className={invalid ? 'error-copy' : 'kb-hint'} role={invalid ? 'alert' : undefined}>
          {invalid ? kbMessages.linkAddressInvalid : kbMessages.linkAddressHelp}
        </small>
      </div>
    </KBDialog>
  );
}
