import { useEffect, useId, useRef, useState } from 'react';
import type { FocusEvent, KeyboardEvent, PointerEvent } from 'react';
import { autoUpdate, flip, offset, shift, useFloating } from '@floating-ui/react';
import { messages } from '../messages';
import { InfoIcon, LockIcon } from './icons';
import './tooltip.css';

export interface HelpTipProps {
  // label names what the tip is about: the button is "About {label}" (a lock: "Why {label} is locked").
  label: string;
  // text is the how, why or definition the row no longer shows; it is the button's accessible description.
  text: string;
  // link: at most one, such as Learn more to the docs. The tip then stays open while the pointer or focus is in it.
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
// and is positioned fixed by Floating UI, so it stays inside the screen (8 px from each edge) and above scrolling panels.
// It takes the look of the icon tooltips (.tooltip: --surface-inverse, --text-inverse).
export function HelpTip({ label, text, link, id, icon = 'info' }: HelpTipProps) {
  const own = useId();
  const textId = id ?? `${own}-text`;
  const [why, setWhy] = useState<Why>(closed);
  const open = why.hover || why.focus || why.pinned;
  const wrap = useRef<HTMLSpanElement>(null);
  const leaving = useRef<ReturnType<typeof setTimeout>>(undefined);
  const { refs, floatingStyles } = useFloating({
    open, placement: 'top', strategy: 'fixed', whileElementsMounted: autoUpdate,
    middleware: [offset(6), flip({ padding: 8 }), shift({ padding: 8 })],
  });
  const change = (next: Partial<Why>) => setWhy((prev) => ({ ...prev, ...next }));
  const cancelLeave = () => clearTimeout(leaving.current);
  useEffect(() => cancelLeave, []);
  useEffect(() => {
    if (!open) return;
    const press = (event: Event) => { if (!wrap.current?.contains(event.target as Node)) setWhy(closed); };
    const key = (event: globalThis.KeyboardEvent) => { if (event.key === 'Escape') setWhy(closed); };
    document.addEventListener('pointerdown', press);
    document.addEventListener('keydown', key);
    return () => { document.removeEventListener('pointerdown', press); document.removeEventListener('keydown', key); };
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
  const onKeyDown = (event: KeyboardEvent) => {
    if (event.key !== 'Escape' || !open) return;
    // The tip closes alone: a dialog or menu around it stays open.
    event.stopPropagation();
    setWhy(closed);
  };
  // A click or tap keeps a tip opened by hover or focus open; on an open tip it closes it.
  const onClick = () => (why.pinned ? setWhy(closed) : change({ pinned: true }));
  const name = icon === 'lock' ? messages.lockedReasonOf(label) : messages.helpTipAbout(label);
  return (
    <span ref={wrap} className="help-tip" onPointerEnter={onPointerEnter} onPointerLeave={onPointerLeave}
      onFocus={() => change({ focus: true })} onBlur={onBlur} onKeyDown={onKeyDown}>
      <button type="button" ref={refs.setReference} className="help-tip-button" aria-label={name} aria-describedby={textId}
        aria-expanded={open} onClick={onClick}>
        {icon === 'lock' ? <LockIcon /> : <InfoIcon />}
      </button>
      <span role="tooltip" className="tooltip help-tip-layer" hidden={!open} ref={open ? refs.setFloating : undefined}
        style={open ? floatingStyles : undefined}>
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
