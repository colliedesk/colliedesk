import type { Node as PMNode, Schema } from '@tiptap/pm/model';
import { Slice } from '@tiptap/pm/model';
import { TextSelection } from '@tiptap/pm/state';
import type { EditorView } from '@tiptap/pm/view';
import { markdownToDoc } from './from-markdown';

// read is Markdown as a document of the editor's own schema (the same nodes as the converter's, made again by
// the editor).
export const read = (schema: Schema, markdown: string): PMNode => schema.nodeFromJSON(markdownToDoc(markdown).toJSON());

// putBlocks puts the blocks of some Markdown at a place between the article's top-level blocks, the cursor
// after them.
export function putBlocks(view: EditorView, at: number, markdown: string) {
  const blocks = read(view.state.schema, markdown).content;
  const tr = view.state.tr.insert(at, blocks);
  tr.setSelection(TextSelection.near(tr.doc.resolve(at + blocks.size), -1)).scrollIntoView();
  view.dispatch(tr);
}

// insertBlocks puts the blocks of some Markdown after the top-level block the cursor is in; an empty paragraph
// there is taken over.
export function insertBlocks(view: EditorView, markdown: string) {
  const { $head } = view.state.selection;
  if ($head.depth === 0) return putBlocks(view, $head.pos, markdown);
  const block = $head.node(1);
  if (block.type.name === 'paragraph' && block.content.size === 0) {
    const from = $head.before(1);
    const blocks = read(view.state.schema, markdown).content;
    const tr = view.state.tr.replaceWith(from, from + block.nodeSize, blocks);
    tr.setSelection(TextSelection.near(tr.doc.resolve(from + blocks.size), -1)).scrollIntoView();
    return view.dispatch(tr);
  }
  return putBlocks(view, $head.after(1), markdown);
}

// pasteBlocks puts pasted Markdown over the selection, or at a place (where a paste waited on its images): its
// first and last paragraphs join the text around it.
export function pasteBlocks(view: EditorView, markdown: string, at?: number) {
  const doc = read(view.state.schema, markdown);
  const tr = view.state.tr;
  if (at !== undefined) tr.setSelection(TextSelection.near(tr.doc.resolve(at)));
  view.dispatch(tr.replaceSelection(Slice.maxOpen(doc.content)).scrollIntoView().setMeta('uiEvent', 'paste'));
}
