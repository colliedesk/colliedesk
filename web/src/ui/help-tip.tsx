import { useEffect, useId, useRef, useState } from 'react';
import type { FocusEvent, PointerEvent } from 'react';
import { autoUpdate, flip, hide, offset, shift, useFloating } from '@floating-ui/react';
import { messages } from '../messages';
import { InfoIcon, LockIcon } from './icons';
import './tooltip.css';

export interface HelpTipProps {
  // label names what the tip is about: the button is "About {label}" (a lock: "Why {label} is locked").
  label: string;
  // text is the how, why or definition the row no longer shows; it is the button's accessible description.
  text: string;
  // link: at most one, such as Learn more to the docs. The tip then stays open while the pointer or focus is in it,
  // and is a disclosure (the button says expanded and controls it), since a tooltip may not hold a link.
  link?: { href: string; label: string };
  // id of the text, so the control the tip is about can be described by it too (aria-describedby).
  id?: string;
  icon?: 'info' | 'lock';
}

interface Why { hover: boolean; focus: boolean; pinned: boolean }
const closed: Why = { hover: false, focus: false, pinned: false };
// The pointer crosses the gap between the button and the tip in this time without closing it.
const leaveDelay = 120;

// HelpTip is an ⓘ button whose text opens on hover, keyboard focus and tap or click, and closes with Escape, a press
// outside and when focus leaves (WCAG 1.4.13). The tip lives next to its button in the DOM, so Tab reaches its link,
// and is positioned fixed by Floating UI, so it stays inside the screen (8 px from each edge) and above scrolling panels;
// it hides while its button is scrolled out of sight. A press on its own text keeps it open (the layer takes focus).
// It takes the look of the icon tooltips (.tooltip: --surface-inverse, --text-inverse).
export function HelpTip({ label, text, link, id, icon = 'info' }: HelpTipProps) {
  const own = useId();
  const textId = id ?? `${own}-text`;
  const layerId = `${own}-layer`;
  const [why, setWhy] = useState<Why>(closed);
  const open = why.hover || why.focus || why.pinned;
  const wrap = useRef<HTMLSpanElement>(null);
  const button = useRef<HTMLButtonElement>(null);
  const leaving = useRef<ReturnType<typeof setTimeout>>(undefined);
  const { refs, floatingStyles, middlewareData } = useFloating({
    open, placement: 'top', strategy: 'fixed', whileElementsMounted: autoUpdate,
    middleware: [offset(6), flip({ padding: 8 }), shift({ padding: 8 }), hide()],
  });
  const change = (next: Partial<Why>) => setWhy((prev) => ({ ...prev, ...next }));
  const cancelLeave = () => clearTimeout(leaving.current);
  useEffect(() => cancelLeave, []);
  useEffect(() => {
    if (!open) return;
    const press = (event: Event) => { if (!wrap.current?.contains(event.target as Node)) setWhy(closed); };
    // Escape closes the tip alone. Listening on window in the capture phase, the tip sees it before the editor or
    // dialog around it, which then never does (it would cancel). Focus inside the tip goes back to its button first.
    const key = (event: globalThis.KeyboardEvent) => {
      if (event.key !== 'Escape') return;
      event.stopPropagation();
      event.preventDefault();
      if (wrap.current?.contains(document.activeElement) && document.activeElement !== button.current) button.current?.focus();
      setWhy(closed);
    };
    document.addEventListener('pointerdown', press);
    window.addEventListener('keydown', key, true);
    return () => { document.removeEventListener('pointerdown', press); window.removeEventListener('keydown', key, true); };
  }, [open]);
  // A finger also reports pointerenter and pointerleave; only a mouse or a pen hovers.
  const hovers = (event: PointerEvent) => event.pointerType !== 'touch';
  const onPointerEnter = (event: PointerEvent) => { if (!hovers(event)) return; cancelLeave(); change({ hover: true }); };
  const onPointerLeave = (event: PointerEvent) => {
    if (!hovers(event)) return;
    cancelLeave();
    leaving.current = setTimeout(() => change({ hover: false }), leaveDelay);
  };
  const onBlur = (event: FocusEvent) => {
    if (!wrap.current?.contains(event.relatedTarget as Node | null)) change({ focus: false, pinned: false });
  };
  // A click or tap keeps a tip opened by hover or focus open; on an open tip it closes it.
  const onClick = () => (why.pinned ? setWhy(closed) : change({ pinned: true }));
  const name = icon === 'lock' ? messages.lockedReasonOf(label) : messages.helpTipAbout(label);
  return (
    <span ref={wrap} className="help-tip" onPointerEnter={onPointerEnter} onPointerLeave={onPointerLeave}
      onFocus={() => change({ focus: true })} onBlur={onBlur}>
      <button type="button" ref={(node) => { button.current = node; refs.setReference(node); }} className="help-tip-button"
        aria-label={name} aria-describedby={textId} aria-expanded={link ? open : undefined} aria-controls={link ? layerId : undefined}
        data-open={open || undefined} onClick={onClick}>
        {icon === 'lock' ? <LockIcon /> : <InfoIcon />}
      </button>
      <span id={layerId} role={link ? undefined : 'tooltip'} tabIndex={-1} className="tooltip help-tip-layer" hidden={!open}
        ref={open ? refs.setFloating : undefined} style={open ? floatingStyles : undefined}
        data-reference-hidden={(open && middlewareData.hide?.referenceHidden) || undefined}>
        <span id={textId}>{text}</span>
        {link && <> <a href={link.href}>{link.label}</a></>}
      </span>
    </span>
  );
}

// LockedReason is the lock beside a control that cannot be changed here; its tip says why.
export function LockedReason(props: Omit<HelpTipProps, 'icon'>) {
  return <HelpTip {...props} icon="lock" />;
}
