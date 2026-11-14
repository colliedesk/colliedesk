import type { ReactNode } from 'react';
import { HelpTip } from '../../ui/help-tip';

// TipField is a labelled control like ui/dialog's Field, with the how and why in a tip after its label instead of
// a paragraph under the control. The tip is also the control's description, next to the refusal and the short help.
export function TipField({ label, error, help, tip, children, id }: {
  label: string; error?: string; help?: string; tip?: string; id: string;
  children: (props: { id: string; 'aria-invalid'?: true; 'aria-describedby'?: string }) => ReactNode;
}) {
  const note = `${id}-note`;
  const hint = `${id}-help`;
  const why = `${id}-why`;
  // The refusal comes first; the short help and the tip stay after it, since they often say how to fix it.
  const described = [error && note, help && hint, tip && why].filter(Boolean).join(' ');
  return <div className={`field${error ? ' field-bad' : ''}`}>
    <span className="heading-with-tip"><label className="field-label" htmlFor={id}>{label}</label>{tip && <HelpTip id={why} label={label} text={tip} />}</span>
    {children({ id, ...(error ? { 'aria-invalid': true } : {}), ...(described ? { 'aria-describedby': described } : {}) })}
    {error && <span className="error-copy field-note" id={note}>{error}</span>}
    {help && <span className="help field-note" id={hint}>{help}</span>}
  </div>;
}
