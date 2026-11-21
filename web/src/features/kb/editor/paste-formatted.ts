import { EditorView } from '@codemirror/view';
import { kbMessages } from '../kb-messages';
import { activeOf } from './commands';
import { pastedMarkdown } from './paste-html';
import { nameOf, stored } from './paste-images';
import { holdPlace, releasePlace, where } from './upload';
import type { UploadHooks } from './upload';

// Pasted is the last paste of formatted text: where it went, both ways to write it, and the one shown.
export type Pasted = { from: number; formatted: string; plain: string; shown: 'formatted' | 'plain' };

function put(view: EditorView, from: number, to: number, text: string, plain: string, onPasted: (pasted: Pasted) => void) {
  view.dispatch({ changes: { from, to, insert: text }, selection: { anchor: from + text.length }, userEvent: 'input.paste', scrollIntoView: true });
  if (plain && plain !== text) onPasted({ from, formatted: text, plain, shown: 'formatted' });
}

// pasteFormatted puts copied HTML in as Markdown. Its images go to the media library first, while a place at
// the cursor shows the paste waiting; Cancel there drops the paste. False leaves the paste to CodeMirror.
export function pasteFormatted(view: EditorView, html: string, plain: string, hooks: UploadHooks, onPasted: (pasted: Pasted) => void) {
  const { markdown, images } = pastedMarkdown(html);
  if (!markdown.trim()) return false;
  const { from, to } = view.state.selection.main;
  if (images.length === 0) {
    put(view, from, to, markdown, plain, onPasted);
    return true;
  }
  if (to > from) view.dispatch({ changes: { from, to }, userEvent: 'delete.paste' });
  const controller = new AbortController();
  const items = images.map(nameOf);
  const settled = images.map(() => false);
  let cancelled = false;
  const id = holdPlace(view, from, images.length === 1 ? items[0].name : kbMessages.tools.pastedImages(images.length), () => {
    cancelled = true;
    controller.abort();
    if (where(view, id) !== undefined) view.dispatch({ effects: releasePlace(id) });
    items.forEach((item, at) => { if (!settled[at]) hooks.onError(item, new DOMException('Cancelled', 'AbortError')); });
  });
  const jobs = images.map((src, at) => {
    hooks.onStart(items[at]);
    return hooks.store(src, controller.signal).then((url) => {
      if (cancelled) return undefined;
      settled[at] = true;
      hooks.onDone(items[at]);
      return url;
    }, (error: unknown) => {
      if (cancelled) return undefined;
      settled[at] = true;
      hooks.onError(items[at], error);
      return undefined;
    });
  });
  void Promise.all(jobs).then((urls) => {
    // The place is gone when the paste was cancelled or the text replaced meanwhile (a restore, theirs).
    const at = cancelled ? undefined : where(view, id);
    if (at === undefined) return;
    view.dispatch({ effects: releasePlace(id) });
    const text = stored(markdown, images, urls);
    if (text) put(view, at, at, text, plain, onPasted);
  });
  return true;
}

// switchPaste writes the last paste the other way; undefined when the text there is no longer the paste.
export function switchPaste(view: EditorView, pasted: Pasted): Pasted | undefined {
  const now = pasted.shown === 'formatted' ? pasted.formatted : pasted.plain;
  if (view.state.sliceDoc(pasted.from, pasted.from + now.length) !== now) return undefined;
  const next = pasted.shown === 'formatted' ? pasted.plain : pasted.formatted;
  view.dispatch({ changes: { from: pasted.from, to: pasted.from + now.length, insert: next }, selection: { anchor: pasted.from + next.length }, userEvent: 'input.paste' });
  return { ...pasted, shown: pasted.shown === 'formatted' ? 'plain' : 'formatted' };
}

// Copied HTML becomes Markdown, except in a code block, and from a code editor (VS Code marks its copies),
// where the plain text is the code.
export const pasteEvents = (hooks: { current: UploadHooks }, onPasted: (pasted: Pasted) => void) =>
  EditorView.domEventHandlers({
    paste(event, view) {
      const data = event.clipboardData;
      const html = data?.getData('text/html') ?? '';
      if (!data || !html || data.types.includes('vscode-editor-data') || activeOf(view.state).code) return false;
      if (!pasteFormatted(view, html, data.getData('text/plain').replace(/\r\n?/g, '\n'), hooks.current, onPasted)) return false;
      event.preventDefault();
      return true;
    },
  });
