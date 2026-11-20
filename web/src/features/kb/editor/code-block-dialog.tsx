import { useId, useRef, useState } from 'react';
import { kbMessages } from '../kb-messages';
import { KBDialog } from '../kb-ui';

// Languages the help center colours, offered first; any other one word is kept as written.
const common = ['bash', 'shell', 'json', 'yaml', 'javascript', 'typescript', 'python', 'go', 'ruby', 'php', 'java', 'csharp', 'sql', 'html', 'css', 'diff'];

// CodeBlockDialog asks for the language of a new code block; empty means plain text.
export function CodeBlockDialog({ onInsert, onClose }: { onInsert: (language: string) => void; onClose: () => void }) {
  const id = useId();
  const field = useRef<HTMLInputElement>(null);
  const [invalid, setInvalid] = useState(false);
  const insert = () => {
    const language = (field.current?.value ?? '').trim();
    if (!/^[\w+#.-]*$/.test(language)) {
      setInvalid(true);
      field.current?.focus();
      return;
    }
    onInsert(language.toLowerCase());
  };
  return (
    <KBDialog title={kbMessages.tools.codeBlock} closeLabel={kbMessages.close} returnFocus={false} onClose={onClose} footer={<>
      <button type="button" onClick={onClose}>{kbMessages.cancel}</button>
      <button type="button" className="primary-action" onClick={insert}>{kbMessages.insert}</button>
    </>}>
      <div className="field md-video">
        <label htmlFor={`${id}-language`}>{kbMessages.codeLanguage}</label>
        <input ref={field} id={`${id}-language`} list={`${id}-languages`} autoComplete="off" spellCheck={false} placeholder="bash"
          aria-invalid={invalid ? true : undefined} aria-describedby={`${id}-help`} onChange={() => setInvalid(false)}
          onKeyDown={(event) => { if (event.key === 'Enter') { event.preventDefault(); insert(); } }} />
        <datalist id={`${id}-languages`}>{common.map((name) => <option key={name} value={name} />)}</datalist>
        <small id={`${id}-help`} className={invalid ? 'error-copy' : 'kb-hint'} role={invalid ? 'alert' : undefined}>
          {invalid ? kbMessages.codeLanguageInvalid : kbMessages.codeLanguageHelp}
        </small>
      </div>
    </KBDialog>
  );
}
