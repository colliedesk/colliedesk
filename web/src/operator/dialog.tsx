import { useEffect, useRef } from 'react';
import type { ComponentProps } from 'react';
import { Dialog } from '../ui/dialog';

// ConsoleDialog is the app's Dialog that also takes the focus when it opens, on its first field (else its first
// footer button), so Escape and Tab work at once. It wraps the shared Dialog so the workspace app is unchanged.
export function ConsoleDialog(props: ComponentProps<typeof Dialog>) {
  const box = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const form = box.current?.querySelector<HTMLFormElement>('form');
    if (!form || form.contains(document.activeElement)) return;
    const target = form.querySelector<HTMLElement>('.form-dialog-body :is(input, select, textarea):not(:disabled)')
      ?? form.querySelector<HTMLElement>('.form-dialog-footer button:not(:disabled)');
    if (target) target.focus();
    else { form.tabIndex = -1; form.focus(); }
  }, []);
  return <div className="dialog-frame" ref={box}><Dialog {...props} /></div>;
}
