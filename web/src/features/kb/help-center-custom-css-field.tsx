import { Hint, controlId } from './help-center-theme-fields';
import { themeMessages as text } from './help-center-theme-messages';

// caretAt is the offset in text of the place a refusal names ("line 3, column 14: …", both from 1 and in
// characters, \r\n, \r, \n and \f each ending a line as the server counts them), or undefined.
export function caretAt(textValue: string, message: string): number | undefined {
  const found = /^line (\d+), column (\d+):/.exec(message);
  if (!found) return undefined;
  const line = Number(found[1]);
  const column = Number(found[2]);
  let offset = 0;
  for (let at = 1; at < line && offset < textValue.length;) {
    const char = textValue[offset];
    offset += char === '\r' && textValue[offset + 1] === '\n' ? 2 : 1;
    if (char === '\n' || char === '\r' || char === '\f') at++;
  }
  for (let at = 1; at < column && offset < textValue.length; at++) {
    offset += textValue.codePointAt(offset)! > 0xffff ? 2 : 1;
  }
  return offset;
}

// CustomCSSField is the Advanced part of the brand form: CSS served after the theme, admins only. It is
// folded unless the saved brand has some; a refused save opens it (the form moves the focus there).
// pending is the server's reason the preview leaves the text out while it is refused.
export function CustomCSSField({ value, savedHasCSS, error, pending, onChange }: {
  value: string; savedHasCSS: boolean; error?: string; pending?: string; onChange: (value: string) => void;
}) {
  const id = controlId('custom_css');
  return (
    <details className="theme-advanced" open={savedHasCSS || undefined}>
      <summary>{text.advanced}</summary>
      <div className="field"><label htmlFor={id}>{text.customCSS}</label>
        <textarea id={id} rows={8} spellCheck={false} autoCapitalize="off" autoCorrect="off" value={value}
          aria-invalid={error ? true : undefined} aria-describedby={`${id}-hint`} onChange={(event) => onChange(event.target.value)} />
        <Hint id={`${id}-hint`} help={text.customCSSHelp} error={error} warning={pending ? text.customCSSPending(pending) : undefined} /></div>
    </details>
  );
}
