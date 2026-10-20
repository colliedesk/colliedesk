import { StateEffect, StateField } from '@codemirror/state';
import { Decoration, EditorView, WidgetType } from '@codemirror/view';
import type { DecorationSet } from '@codemirror/view';
import { kbMessages } from '../kb-messages';
import { blockAt, blockChange } from './commands';

// UploadHooks connect the editor to the article page: which files it accepts (refusing the others with a
// reason), how one is sent and how it is described, and what the page shows meanwhile.
export interface UploadHooks {
  accept(files: File[]): File[];
  // upload sends a file and gives what the article holds for it (an image's Markdown, a video's HTML);
  // progress (0 to 1) shows in its place while a large file goes.
  upload(file: File, progress: (done: number) => void): Promise<{ snippet: string }>;
  onStart(file: File): void;
  onDone(file: File): void;
  onError(file: File, error: unknown): void;
}

class UploadingWidget extends WidgetType {
  constructor(readonly name: string, readonly percent?: number) {
    super();
  }
  eq(other: UploadingWidget) {
    return other.name === this.name && other.percent === this.percent;
  }
  toDOM() {
    const box = document.createElement('div');
    box.className = 'cm-uploading';
    box.setAttribute('role', 'status');
    box.textContent = this.percent === undefined ? kbMessages.uploadingImage(this.name) : kbMessages.uploadingPercent(this.name, this.percent);
    return box;
  }
  ignoreEvent() {
    return true;
  }
}

const add = StateEffect.define<{ id: object; pos: number; name: string }>({
  map: (value, change) => ({ ...value, pos: change.mapPos(value.pos, 1) }),
});
const remove = StateEffect.define<object>();
const progress = StateEffect.define<{ id: object; percent: number }>();

// The upload's place is a widget decoration, not text: the document (what is saved) never carries a placeholder,
// and the widget moves with the edits made while the image is on its way.
export const uploads = StateField.define<DecorationSet>({
  create: () => Decoration.none,
  update(set, tr) {
    let next = set.map(tr.changes);
    for (const effect of tr.effects) {
      if (effect.is(add))
        next = next.update({
          add: [
            Decoration.widget({
              widget: new UploadingWidget(effect.value.name),
              block: true,
              side: 1,
              id: effect.value.id,
            }).range(effect.value.pos),
          ],
        });
      if (effect.is(remove)) next = next.update({ filter: (_from, _to, deco) => deco.spec.id !== effect.value });
      if (effect.is(progress)) {
        // The widget is made again with the new percent, at the place it has now.
        let at: number | undefined;
        let name = '';
        next.between(0, tr.state.doc.length, (from, _to, deco) => {
          if (deco.spec.id === effect.value.id) { at = from; name = (deco.spec.widget as UploadingWidget).name; }
        });
        if (at !== undefined)
          next = next.update({
            filter: (_from, _to, deco) => deco.spec.id !== effect.value.id,
            add: [Decoration.widget({ widget: new UploadingWidget(name, effect.value.percent), block: true, side: 1, id: effect.value.id }).range(at)],
          });
      }
    }
    return next;
  },
  provide: (field) => EditorView.decorations.from(field),
});

export const uploadsPending = (view: EditorView) => view.state.field(uploads).size > 0;

const where = (view: EditorView, id: object) => {
  let found: number | undefined;
  view.state.field(uploads).between(0, view.state.doc.length, (from, _to, deco) => {
    if (deco.spec.id === id) found = from;
  });
  return found;
};

// startUpload sends each accepted file and puts the image (or the video) after the block pos is in, as a block of
// its own, once its address is known; a refused upload only takes its widget away.
export function startUpload(view: EditorView, files: File[], pos: number, hooks: UploadHooks) {
  const accepted = hooks.accept(files);
  for (const file of accepted) {
    const id = {};
    view.dispatch({ effects: add.of({ id, pos: blockAt(view.state.doc, pos), name: file.name }) });
    hooks.onStart(file);
    let shown = -1;
    const onProgress = (done: number) => {
      const percent = Math.min(99, Math.floor(done * 100));
      if (percent === shown || where(view, id) === undefined) return;
      shown = percent;
      view.dispatch({ effects: progress.of({ id, percent }) });
    };
    hooks.upload(file, onProgress).then(
      ({ snippet }) => {
        // The place is gone when the text was replaced meanwhile (a restore, their version): the upload still ends.
        const at = where(view, id);
        if (at !== undefined) {
          const change = blockChange(view.state.doc, at, snippet);
          view.dispatch({ changes: { from: change.from, insert: change.insert }, effects: remove.of(id), userEvent: 'input.upload' });
        }
        hooks.onDone(file);
      },
      (error: unknown) => {
        if (where(view, id) !== undefined) view.dispatch({ effects: remove.of(id) });
        hooks.onError(file, error);
      },
    );
  }
}

const imagesOf = (list: FileList | null | undefined) => [...(list ?? [])];

// Paste and drop: files win over the text/html a browser also puts on the clipboard when an image is copied from a page.
export const uploadEvents = (hooks: { current: UploadHooks }) =>
  EditorView.domEventHandlers({
    paste(event, view) {
      const files = imagesOf(event.clipboardData?.files);
      if (files.length === 0) return false;
      event.preventDefault();
      startUpload(view, files, view.state.selection.main.head, hooks.current);
      return true;
    },
    dragover(event) {
      if (!event.dataTransfer?.types.includes('Files')) return false;
      event.preventDefault();
      return false;
    },
    drop(event, view) {
      const files = imagesOf(event.dataTransfer?.files);
      if (files.length === 0) return false;
      event.preventDefault();
      const pos = view.posAtCoords({ x: event.clientX, y: event.clientY }) ?? view.state.selection.main.head;
      startUpload(view, files, pos, hooks.current);
      return true;
    },
  });
