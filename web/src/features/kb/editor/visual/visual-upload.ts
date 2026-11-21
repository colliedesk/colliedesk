import { Plugin, PluginKey } from '@tiptap/pm/state';
import type { EditorView } from '@tiptap/pm/view';
import { Decoration, DecorationSet } from '@tiptap/pm/view';
import { kbMessages } from '../../kb-messages';
import type { UploadHooks } from '../editor-types';
import { putBlocks } from './visual-insert';

type Place = { id: object; pos: number; name: string; cancel: () => void; inline: boolean; percent?: number };
type Change = { add?: Place; remove?: object };

const key = new PluginKey<DecorationSet>('kbUploads');

function placeDOM(place: Place) {
  const box = document.createElement(place.inline ? 'span' : 'div');
  box.className = 'cm-uploading';
  const status = document.createElement('span');
  status.setAttribute('role', 'status');
  status.textContent = place.percent === undefined ? kbMessages.uploadingImage(place.name) : kbMessages.uploadingPercent(place.name, place.percent);
  const cancel = document.createElement('button');
  cancel.type = 'button';
  cancel.className = 'link-action';
  cancel.textContent = kbMessages.cancel;
  cancel.setAttribute('aria-label', kbMessages.cancelUpload(place.name));
  // The editor keeps the focus and the cursor; the press only cancels.
  cancel.addEventListener('mousedown', (event) => event.preventDefault());
  cancel.addEventListener('click', () => place.cancel());
  box.append(status, ' ', cancel);
  return box;
}

const widget = (place: Place) => Decoration.widget(place.pos, () => placeDOM(place), {
  id: place.id, place, side: 1, ignoreSelection: true, stopEvent: () => true, key: `${place.name}:${place.percent ?? ''}`,
});

// uploadPlaces shows where each upload goes while it is on its way: a decoration, never text, so what is saved
// never holds a placeholder, and the place moves with the edits made meanwhile.
export const uploadPlaces = new Plugin<DecorationSet>({
  key,
  state: {
    init: () => DecorationSet.empty,
    apply(tr, set) {
      let next = set.map(tr.mapping, tr.doc);
      const change = tr.getMeta(key) as Change | undefined;
      if (change?.remove) next = next.remove(next.find(undefined, undefined, (spec) => spec.id === change.remove));
      if (change?.add) next = next.add(tr.doc, [widget(change.add)]);
      return next;
    },
  },
  props: { decorations: (state) => key.getState(state) },
});

const found = (view: EditorView, id: object) => key.getState(view.state)?.find(undefined, undefined, (spec) => spec.id === id)[0];
export const placeOf = (view: EditorView, id: object) => found(view, id)?.from;
export const uploadsPending = (view: EditorView) => (key.getState(view.state)?.find().length ?? 0) > 0;

export function holdPlace(view: EditorView, pos: number, name: string, cancel: () => void, inline: boolean) {
  const id = {};
  view.dispatch(view.state.tr.setMeta(key, { add: { id, pos, name, cancel, inline } }).setMeta('addToHistory', false));
  return id;
}
export function releasePlace(view: EditorView, id: object) {
  if (placeOf(view, id) !== undefined) view.dispatch(view.state.tr.setMeta(key, { remove: id }).setMeta('addToHistory', false));
}
function showProgress(view: EditorView, id: object, percent: number) {
  const old = found(view, id);
  if (!old) return;
  const place = { ...(old.spec.place as Place), pos: old.from, percent };
  view.dispatch(view.state.tr.setMeta(key, { remove: id }).setMeta('addToHistory', false));
  view.dispatch(view.state.tr.setMeta(key, { add: place }).setMeta('addToHistory', false));
}

// after is the place between top-level blocks just after the one pos is in.
const after = (view: EditorView, pos: number) => {
  const $pos = view.state.doc.resolve(Math.min(pos, view.state.doc.content.size));
  return $pos.depth === 0 ? $pos.pos : $pos.after(1);
};

// startUpload sends each accepted file and puts what the article holds for it after the block pos is in, once
// its address is known; a refused or cancelled upload only takes its place away.
export function startUpload(view: EditorView, files: File[], pos: number, hooks: UploadHooks) {
  for (const file of hooks.accept(files)) {
    const controller = new AbortController();
    // settled: the page hears of each upload once, done, failed or cancelled.
    let settled = false;
    const id = holdPlace(view, after(view, pos), file.name, () => {
      if (settled) return;
      settled = true;
      controller.abort();
      releasePlace(view, id);
      hooks.onError(file, new DOMException('Cancelled', 'AbortError'));
    }, false);
    hooks.onStart(file);
    let shown = -1;
    const progress = (done: number) => {
      const percent = Math.min(99, Math.floor(done * 100));
      if (percent === shown || placeOf(view, id) === undefined) return;
      shown = percent;
      showProgress(view, id, percent);
    };
    hooks.upload(file, progress, controller.signal).then(({ snippet }) => {
      if (settled) return;
      settled = true;
      // The place is gone when the text was replaced meanwhile (a restore, their version): the upload still ends.
      const at = placeOf(view, id);
      if (at !== undefined) {
        releasePlace(view, id);
        putBlocks(view, at, snippet);
      }
      hooks.onDone(file);
    }, (error: unknown) => {
      if (settled) return;
      settled = true;
      releasePlace(view, id);
      hooks.onError(file, error);
    });
  }
}
