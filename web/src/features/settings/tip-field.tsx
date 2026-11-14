import type { ReactNode } from 'react';
import { HelpTip } from '../../ui/help-tip';

// TipField is a labelled control like Field (ui/dialog.tsx) with a help tip after its label: the how or why the row no
// longer shows. The tip's text also describes the control, after its refusal and its one short line. Field cannot hold
// the tip itself: the operator console bundles it, and has no room for the tip's positioning code.
export function TipField({ label, tip, error, help, children, id }: {
  label: string; tip: string; error?: string; help?: string; id: string;
  children: (props: { id: string; 'aria-invalid'?: true; 'aria-describedby': string }) => ReactNode;
}) {
  const note = `${id}-note`;
  const hint = `${id}-help`;
  const tipId = `${id}-tip`;
  const described = [error && note, help && hint, tipId].filter(Boolean).join(' ');
  return <div className={`field${error ? ' field-bad' : ''}`}>
    <span className="heading-with-tip"><label className="field-label" htmlFor={id}>{label}</label><HelpTip label={label} text={tip} id={tipId} /></span>
    {children({ id, ...(error ? { 'aria-invalid': true as const } : {}), 'aria-describedby': described })}
    {error && <span className="error-copy field-note" id={note}>{error}</span>}
    {help && <span className="help field-note" id={hint}>{help}</span>}
  </div>;
}
