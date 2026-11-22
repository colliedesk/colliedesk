import { useId, useState } from 'react';
import type { ReactNode } from 'react';
import type { Editor } from '@tiptap/core';
import { rules, within } from '../format-tags';
import { editCaption, setFigure } from './figure-commands';
import type { Picked } from './figure-commands';
import { imageBarMessages as m } from './image-bar-messages';
import { LinkDialog } from './link-dialog';

const named = rules.figure.widths as readonly string[];

// ImageBar sets the picked image's width, alignment, border, caption and link; each change is saved as the
// image's <figure>. extra holds more tools for the image, such as Annotate.
export function ImageBar({ editor, picked, extra }: { editor: Editor; picked: Picked; extra?: ReactNode }) {
  const id = useId();
  const width = picked.attrs.width;
  const [pixelMode, setPixelMode] = useState(false);
  const [invalid, setInvalid] = useState(false);
  const [linking, setLinking] = useState(false);
  const inPixels = pixelMode || (width !== null && !named.includes(width));
  const set = (change: Parameters<typeof setFigure>[2]) => setFigure(editor, picked, change);
  const pickWidth = (value: string) => {
    setInvalid(false);
    setPixelMode(value === 'pixels');
    if (value !== 'pixels') set({ width: value || null });
  };
  const typePixels = (value: string) => {
    const ok = within(value, 1, rules.figure.pixels);
    setInvalid(!ok);
    if (ok) set({ width: String(Number(value)) });
  };
  return (
    <div className="kb-image-bar" role="toolbar" aria-label={m.label}>
      <label htmlFor={`${id}-width`} className="sr-only">{m.width}</label>
      <select id={`${id}-width`} value={inPixels ? 'pixels' : (width ?? '')} onChange={(event) => pickWidth(event.target.value)}>
        <option value="">{m.widths.original}</option>
        {named.map((name) => <option key={name} value={name}>{m.widths[name as keyof typeof m.widths]}</option>)}
        <option value="pixels">{m.widths.pixels}</option>
      </select>
      {inPixels && <>
        <label htmlFor={`${id}-pixels`} className="sr-only">{m.pixels}</label>
        <input id={`${id}-pixels`} type="number" min={1} max={rules.figure.pixels} step={1} inputMode="numeric" className="kb-image-pixels"
          defaultValue={width && !named.includes(width) ? width : ''} aria-invalid={invalid ? true : undefined}
          aria-describedby={invalid ? `${id}-pixels-error` : undefined} onChange={(event) => typePixels(event.target.value)} />
        {invalid && <small id={`${id}-pixels-error`} role="alert" className="error-copy">{m.pixelsInvalid(rules.figure.pixels)}</small>}
      </>}
      <label htmlFor={`${id}-align`} className="sr-only">{m.alignment}</label>
      <select id={`${id}-align`} value={picked.attrs.align ?? ''} onChange={(event) => set({ align: event.target.value || null })}>
        <option value="">{m.aligns.default}</option>
        {(rules.figure.align as readonly string[]).map((name) => <option key={name} value={name}>{m.aligns[name as keyof typeof m.aligns]}</option>)}
      </select>
      <button type="button" aria-pressed={picked.attrs.border} onClick={() => set({ border: !picked.attrs.border })}>{m.border}</button>
      <button type="button" onClick={() => editCaption(editor, picked)}>{m.caption}</button>
      <button type="button" aria-pressed={picked.attrs.href !== null} onClick={() => setLinking(true)}>{m.link}</button>
      {extra}
      {linking && <LinkDialog current={picked.attrs.href ?? ''} onClose={() => setLinking(false)}
        onRemove={() => { setLinking(false); set({ href: null, linkTitle: null }); }}
        onSet={(href) => { setLinking(false); set({ href }); }} />}
    </div>
  );
}
