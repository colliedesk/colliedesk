import { useEffect, useId, useRef } from 'react';
import type { FormEvent, ReactNode } from 'react';
import { dialogKeys, keepDialogFocus, useReturnFocus } from './dialog-keys';
import './controls.css';

// Dialog is the frame of a form dialog: Escape closes, Tab stays inside, focus returns to what opened it.
// alert makes it an alertdialog without a close button (a confirmation). focusFirst moves the focus, when no
// autoFocus control took it, to the first field (else the first footer button), so Escape and Tab work at once.
export function Dialog({ title, help, onClose, children, footer, onSubmit, alert, closeLabel, className = '', focusFirst = false }: {
  title: string; help?: string; onClose: () => void; children: ReactNode; footer: ReactNode; onSubmit?: (event: FormEvent) => void; alert?: boolean;
  closeLabel: string; className?: string; focusFirst?: boolean;
}) {
  useReturnFocus();
  const root = useRef<HTMLFormElement>(null);
  useEffect(() => {
    const form = root.current;
    if (!focusFirst || !form || form.contains(document.activeElement)) return;
    const target = form.querySelector<HTMLElement>('.form-dialog-body :is(input, select, textarea):not(:disabled)')
      ?? form.querySelector<HTMLElement>('.form-dialog-footer button:not(:disabled)') ?? form;
    if (target === form) form.tabIndex = -1;
    target.focus();
  }, [focusFirst]);
  const titleId = useId();
  return (
    <div className="dialog-backdrop" onMouseDown={keepDialogFocus}>
      <form className={`dialog form-dialog ${className}`.trim()} role={alert ? 'alertdialog' : 'dialog'} aria-modal="true" aria-labelledby={titleId} ref={root}
        onKeyDown={dialogKeys(root, onClose)} onSubmit={(event) => { event.preventDefault(); onSubmit?.(event); }} noValidate>
        <header><div><h2 id={titleId}>{title}</h2>{help && <p>{help}</p>}</div>
          {!alert && <button type="button" className="icon-button" aria-label={closeLabel} onClick={onClose}>×</button>}</header>
        <div className="form-dialog-body">{children}</div>
        <footer className="button-row form-dialog-footer">{footer}</footer>
      </form>
    </div>
  );
}

// Field is a labelled control with its refusal and help tied to it (aria-describedby).
export function Field({ label, error, help, children, id }: { label: string; error?: string; help?: string; children: (props: { id: string; 'aria-invalid'?: true; 'aria-describedby'?: string }) => ReactNode; id: string }) {
  const note = `${id}-note`;
  const hint = `${id}-help`;
  // The refusal comes first; the field's own help stays under it, since it often says how to fix it.
  const described = [error && note, help && hint].filter(Boolean).join(' ');
  return <div className={`field${error ? ' field-bad' : ''}`}><label className="field-label" htmlFor={id}>{label}</label>
    {children({ id, ...(error ? { 'aria-invalid': true } : {}), ...(described ? { 'aria-describedby': described } : {}) })}
    {error && <span className="error-copy field-note" id={note}>{error}</span>}
    {help && <span className="help field-note" id={hint}>{help}</span>}
  </div>;
}
