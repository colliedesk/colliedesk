import type { EditorView } from '@tiptap/pm/view';
import { kbMessages } from '../../kb-messages';
import type { UploadHooks } from '../editor-types';
import { pastedMarkdown } from '../paste-html';
import { nameOf, stored } from '../paste-images';
import { pasteBlocks } from './visual-insert';
import { holdPlace, placeOf, releasePlace, startUpload } from './visual-upload';

// pasteFormatted puts copied HTML in as the Markdown an article stores, read back into the editor: what shows is
// what is saved. Its images go to the media library first, while a place at the cursor shows the paste waiting;
// Cancel there drops the paste. False leaves the paste to the editor (plain text, or nothing it can keep).
export function pasteFormatted(view: EditorView, html: string, hooks: UploadHooks) {
  const { markdown, images } = pastedMarkdown(html);
  if (!markdown.trim()) return false;
  if (images.length === 0) {
    pasteBlocks(view, markdown);
    return true;
  }
  if (!view.state.selection.empty) view.dispatch(view.state.tr.deleteSelection());
  const controller = new AbortController();
  const items = images.map(nameOf);
  const settled = images.map(() => false);
  let cancelled = false;
  const id = holdPlace(view, view.state.selection.from, images.length === 1 ? items[0].name : kbMessages.tools.pastedImages(images.length), () => {
    cancelled = true;
    controller.abort();
    releasePlace(view, id);
    items.forEach((item, at) => { if (!settled[at]) hooks.onError(item, new DOMException('Cancelled', 'AbortError')); });
  }, true);
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
    const at = cancelled ? undefined : placeOf(view, id);
    if (at === undefined) return;
    releasePlace(view, id);
    const text = stored(markdown, images, urls);
    if (text) pasteBlocks(view, text, at);
  });
  return true;
}

// pasteEvent is the editor's paste: files are uploaded (they win over the text/html a browser also puts on the
// clipboard when an image is copied from a page), formatted text is read as Markdown, except in a code block and
// from a code editor (VS Code marks its copies), where the plain text is the code.
export function pasteEvent(view: EditorView, event: ClipboardEvent, hooks: UploadHooks) {
  const data = event.clipboardData;
  if (!data) return false;
  const files = [...(data.files ?? [])];
  if (files.length > 0) {
    startUpload(view, files, view.state.selection.head, hooks);
    return true;
  }
  const html = data.getData('text/html');
  if (!html || [...(data.types ?? [])].includes('vscode-editor-data') || view.state.selection.$head.parent.type.spec.code) return false;
  return pasteFormatted(view, html, hooks);
}
