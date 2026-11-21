import type { Editor } from '@tiptap/core';
import { Fragment } from '@tiptap/pm/model';
import type { Node as PMNode } from '@tiptap/pm/model';
import { NodeSelection, TextSelection } from '@tiptap/pm/state';
import type { EditorState, Transaction } from '@tiptap/pm/state';
import type { FigureAttrs } from './article-figure';

// Picked is the image the image bar works on: an image in a line of text, or one in a figure.
export interface Picked {
  pos: number;
  figure: boolean;
  attrs: FigureAttrs;
}

// pickedImage is the image the selection is on, or the figure the cursor is in.
export function pickedImage(state: EditorState): Picked | undefined {
  const { selection } = state;
  if (selection instanceof NodeSelection) {
    const node = selection.node;
    if (node.type.name === 'articleFigure') return { pos: selection.from, figure: true, attrs: node.attrs as FigureAttrs };
    if (node.type.name === 'image' && canBeFigure(state, selection.from)) return { pos: selection.from, figure: false, attrs: imageAttrs(node) };
    return undefined;
  }
  const { $from } = selection;
  for (let depth = $from.depth; depth > 0; depth -= 1) {
    const node = $from.node(depth);
    if (node.type.name === 'articleFigure') return { pos: $from.before(depth), figure: true, attrs: node.attrs as FigureAttrs };
  }
  return undefined;
}

export const samePicked = (a: Picked | undefined, b: Picked | undefined) => JSON.stringify(a) === JSON.stringify(b);

function imageAttrs(image: PMNode): FigureAttrs {
  const link = image.marks.find((mark) => mark.type.name === 'link');
  return {
    src: image.attrs.src as string, alt: (image.attrs.alt as string | null) ?? null, title: (image.attrs.title as string | null) ?? null,
    href: (link?.attrs.href as string | undefined) ?? null, linkTitle: (link?.attrs.title as string | null | undefined) ?? null,
    width: null, align: null, border: false,
  };
}

// An image becomes a figure in place of its line, so the line must stand where a figure can.
function canBeFigure(state: EditorState, pos: number) {
  const $pos = state.doc.resolve(pos);
  if ($pos.depth < 1) return false;
  const holder = $pos.node($pos.depth - 1);
  const index = $pos.index($pos.depth - 1);
  return holder.canReplaceWith(index, index + 1, state.schema.nodes.articleFigure!);
}

// trimmed is a line's text with the spaces and line breaks at the side the image was on taken away.
function trimmed(content: Fragment, side: 'end' | 'start') {
  const nodes: PMNode[] = [];
  content.forEach((node) => { nodes.push(node); });
  const at = side === 'end' ? () => nodes.length - 1 : () => 0;
  while (nodes.length) {
    const node = nodes[at()]!;
    if (node.type.name === 'hardBreak') { nodes.splice(at(), 1); continue; }
    if (!node.isText) break;
    const text = side === 'end' ? node.text!.replace(/\s+$/, '') : node.text!.replace(/^\s+/, '');
    if (text === node.text) break;
    if (text) { nodes[at()] = node.type.schema.text(text, node.marks); break; }
    nodes.splice(at(), 1);
  }
  return Fragment.fromArray(nodes);
}

// toFigure puts the image at pos in a figure of its own, splitting its line around it; the line's alignment
// and indent stay on the text before and after it. It gives where the figure starts.
function toFigure(tr: Transaction, pos: number, attrs: FigureAttrs) {
  const $pos = tr.doc.resolve(pos);
  const line = $pos.parent;
  const image = tr.doc.nodeAt(pos)!;
  const before = trimmed(line.content.cut(0, $pos.parentOffset), 'end');
  const after = trimmed(line.content.cut($pos.parentOffset + image.nodeSize), 'start');
  const blocks: PMNode[] = [];
  if (before.size) blocks.push(line.type.create(line.attrs, before));
  blocks.push(tr.doc.type.schema.nodes.articleFigure!.create(attrs));
  if (after.size) blocks.push(line.type.create(line.attrs, after));
  tr.replaceWith($pos.before(), $pos.after(), blocks);
  return $pos.before() + (before.size ? blocks[0]!.nodeSize : 0);
}

// A figure with no caption has no text, so Tiptap would take a change to an article's only image for the
// article being cleared, and empty it.
const kept = (tr: Transaction) => tr.setMeta('preventClearDocument', true);

// setFigure changes what the picked image's figure holds, first putting the image in a figure when it is in a
// line of text, and leaves the figure picked.
export function setFigure(editor: Editor, picked: Picked, change: Partial<FigureAttrs>) {
  const attrs = { ...picked.attrs, ...change };
  const tr = editor.state.tr;
  let pos = picked.pos;
  if (picked.figure) tr.setNodeMarkup(pos, undefined, attrs);
  else pos = toFigure(tr, pos, attrs);
  tr.setSelection(NodeSelection.create(tr.doc, pos));
  editor.view.dispatch(kept(tr));
}

// editCaption puts the cursor at the end of the picked image's caption, starting one when it has none.
export function editCaption(editor: Editor, picked: Picked) {
  const tr = editor.state.tr;
  const pos = picked.figure ? picked.pos : toFigure(tr, picked.pos, picked.attrs);
  tr.setSelection(TextSelection.create(tr.doc, pos + tr.doc.nodeAt(pos)!.nodeSize - 1));
  editor.view.dispatch(kept(tr).scrollIntoView());
  editor.commands.focus();
}
