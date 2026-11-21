import type { Editor } from '@tiptap/core';
import type { Node as PMNode } from '@tiptap/pm/model';
import type { EditorState } from '@tiptap/pm/state';
import { isInTable } from '@tiptap/pm/tables';
import { rules } from '../format-tags';
import type { Align, Palette } from '../format-tags';
import type { Active, FormatAt, TableAction } from '../editor-types';

// The formatting the server keeps beyond Markdown, in the visual editor: underline, highlight, the palette's
// colours, alignment and indent of top-level blocks, and the table tools.

type Format = Pick<Active, 'underline' | 'highlight' | 'color' | 'align' | 'indent' | 'formatAt' | 'table' | 'headerRow' | 'canMerge' | 'canSplit'>;

// around is the innermost node the cursor is in that has one of the table roles.
function around(state: EditorState, roles: string[]) {
  const { $head } = state.selection;
  for (let depth = $head.depth; depth > 0; depth--) {
    const node = $head.node(depth);
    if (roles.includes(String(node.type.spec.tableRole))) return node;
  }
  return undefined;
}

// formatAtOf: alignment and indent are stored on a top-level paragraph or heading (the <p>/<hN> wrapper) and on
// a table cell; a block in a list, a quote or a callout keeps neither, so the tools are off there.
function formatAtOf(state: EditorState): FormatAt {
  if (isInTable(state)) return 'cell';
  const { $head } = state.selection;
  return $head.depth === 1 && ['paragraph', 'heading'].includes($head.parent.type.name) ? 'block' : 'none';
}

// topBlocks is every top-level paragraph and heading the selection touches.
function topBlocks(state: EditorState) {
  const found: { node: PMNode; pos: number }[] = [];
  const { from, to } = state.selection;
  state.doc.nodesBetween(from, to, (node, pos, parent) => {
    if (parent === state.doc && ['paragraph', 'heading'].includes(node.type.name)) found.push({ node, pos });
    return false;
  });
  return found;
}

export function formatOf(editor: Editor): Format {
  const { state } = editor;
  const formatAt = formatAtOf(state);
  const holder = formatAt === 'cell' ? around(state, ['cell', 'header_cell']) : formatAt === 'block' ? state.selection.$head.parent : undefined;
  const firstRow = formatAt === 'cell' ? around(state, ['table'])?.firstChild : undefined;
  let headerRow = !!firstRow;
  firstRow?.forEach((cell) => { if (cell.type.name !== 'tableHeader') headerRow = false; });
  return {
    underline: editor.isActive('underline'),
    highlight: editor.isActive('highlight'),
    color: editor.isActive('color') ? String(editor.getAttributes('color').color ?? '') : '',
    align: (holder?.attrs.align as Align | null) ?? 'left',
    indent: formatAt === 'block' ? Number(holder?.attrs.indent ?? 0) : 0,
    formatAt,
    table: formatAt === 'cell',
    headerRow,
    canMerge: formatAt === 'cell' && editor.can().mergeCells(),
    canSplit: formatAt === 'cell' && editor.can().splitCell(),
  };
}

export function setColor(editor: Editor, color: Palette | null) {
  const chain = editor.chain().focus();
  if (color) chain.setMark('color', { color }).run();
  else chain.unsetMark('color').run();
}

export function setAlign(editor: Editor, align: Align | null) {
  const { state } = editor;
  const at = formatAtOf(state);
  if (at === 'cell') { editor.chain().focus().setCellAttribute('align', align).run(); return; }
  if (at === 'none') return;
  const tr = state.tr;
  for (const { node, pos } of topBlocks(state)) tr.setNodeMarkup(pos, undefined, { ...node.attrs, align });
  editor.view.dispatch(tr);
}

export function setIndent(editor: Editor, delta: 1 | -1) {
  const { state } = editor;
  if (formatAtOf(state) !== 'block') return;
  const tr = state.tr;
  for (const { node, pos } of topBlocks(state)) {
    const indent = Math.min(rules.indent, Math.max(0, Number(node.attrs.indent ?? 0) + delta));
    tr.setNodeMarkup(pos, undefined, { ...node.attrs, indent });
  }
  editor.view.dispatch(tr);
}

export function editTable(editor: Editor, action: TableAction) {
  const chain = editor.chain().focus();
  if (action.startsWith('bg-')) {
    const bg = action.slice(3);
    chain.setCellAttribute('bg', bg === 'none' ? null : bg).run();
    return;
  }
  switch (action) {
    case 'headerRow': chain.toggleHeaderRow().run(); break;
    case 'rowBefore': chain.addRowBefore().run(); break;
    case 'rowAfter': chain.addRowAfter().run(); break;
    case 'columnBefore': chain.addColumnBefore().run(); break;
    case 'columnAfter': chain.addColumnAfter().run(); break;
    case 'deleteRow': chain.deleteRow().run(); break;
    case 'deleteColumn': chain.deleteColumn().run(); break;
    case 'merge': chain.mergeCells().run(); break;
    case 'split': chain.splitCell().run(); break;
    case 'deleteTable': chain.deleteTable().run(); break;
  }
}
