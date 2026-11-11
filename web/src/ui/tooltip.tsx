import { cloneElement, isValidElement, useState, type ReactElement, type Ref } from 'react';
import {
  FloatingPortal, autoUpdate, flip, offset, shift, useDismiss, useFloating, useFocus, useHover, useInteractions, useMergeRefs, useRole,
} from '@floating-ui/react';
import './tooltip.css';

// Tooltip shows a short text next to its one child on hover and keyboard focus, positioned by Floating UI
// so it stays on screen and above scrolling panels. Escape hides it (WCAG 1.4.13). The child keeps its
// own name; the tooltip describes it (aria-describedby), so a screen reader hears the text too.
export function Tooltip({ text, children }: { text: string; children: ReactElement<{ ref?: Ref<HTMLElement> }> }) {
  const [open, setOpen] = useState(false);
  const { refs, floatingStyles, context } = useFloating({
    open, onOpenChange: setOpen, placement: 'top', whileElementsMounted: autoUpdate,
    middleware: [offset(6), flip({ fallbackAxisSideDirection: 'start' }), shift({ padding: 8 })],
  });
  const hover = useHover(context, { move: false, delay: { open: 150, close: 0 } });
  const focus = useFocus(context);
  const dismiss = useDismiss(context);
  const role = useRole(context, { role: 'tooltip' });
  const { getReferenceProps, getFloatingProps } = useInteractions([hover, focus, dismiss, role]);
  const childRef = isValidElement(children) ? children.props.ref : undefined;
  const ref = useMergeRefs([refs.setReference, childRef]);
  if (!isValidElement(children)) return children;
  return (
    <>
      {cloneElement(children, getReferenceProps({ ref, ...children.props }) as Record<string, unknown>)}
      {open && (
        <FloatingPortal>
          <div ref={refs.setFloating} className="tooltip" style={floatingStyles} {...getFloatingProps()}>{text}</div>
        </FloatingPortal>
      )}
    </>
  );
}
