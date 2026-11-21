import { EditorSelection } from '@codemirror/state';
import type { ChangeSpec, EditorState, Line } from '@codemirror/state';
import type { EditorView } from '@codemirror/view';
import { syntaxTree } from '@codemirror/language';
import { parseTag, rules, tagString } from './format-tags';
import type { Align, FormatTag, Palette } from './format-tags';
import type { Active, FormatAt } from './editor-types';

// The formatting the server keeps beyond Markdown, edited in the Markdown source: <u>, <mark>, the palette's
// <span data-color>, and the <p>/<hN> wrapper that holds a top-level block's alignment and indent. Each tag is
// written in the spelling the server keeps (tagString), so saving never changes what was typed here.

const spanOpen = /<span data-color="([a-z]+)">$/;
const spanClose = '</span>';

// toggleWrap wraps each selection in open…close, or takes the pair off when it is already there, inside or just
// outside the selection.
export function toggleWrap(view: EditorView, open: string, close: string) {
  const { state } = view;
  view.dispatch(state.changeByRange((range) => {
    const text = state.sliceDoc(range.from, range.to);
    if (text.length >= open.length + close.length && text.startsWith(open) && text.endsWith(close))
      return { changes: [{ from: range.from, to: range.from + open.length }, { from: range.to - close.length, to: range.to }],
        range: EditorSelection.range(range.from, range.to - open.length - close.length) };
    if (state.sliceDoc(range.from - open.length, range.from) === open && state.sliceDoc(range.to, range.to + close.length) === close)
      return { changes: [{ from: range.from - open.length, to: range.from }, { from: range.to, to: range.to + close.length }],
        range: EditorSelection.range(range.from - open.length, range.to - open.length) };
    return { changes: [{ from: range.from, insert: open }, { from: range.to, insert: close }],
      range: EditorSelection.range(range.from + open.length, range.to + open.length) };
  }), { scrollIntoView: true, userEvent: 'input.format' });
}

// setColor puts the selection in a palette span, changes the span it is already in, or (null) takes it off.
export function setColor(view: EditorView, color: Palette | null) {
  const { state } = view;
  const open = color ? tagString({ name: 'span', close: false, attrs: { 'data-color': color } }) : '';
  view.dispatch(state.changeByRange((range) => {
    const before = spanOpen.exec(state.sliceDoc(state.doc.lineAt(range.from).from, range.from));
    if (before && state.sliceDoc(range.to, range.to + spanClose.length) === spanClose) {
      const start = range.from - before[0].length;
      const changes: ChangeSpec[] = [{ from: start, to: range.from, insert: open }];
      if (!color) changes.push({ from: range.to, to: range.to + spanClose.length });
      const shift = open.length - before[0].length;
      return { changes, range: EditorSelection.range(range.from + shift, range.to + shift) };
    }
    if (!color) return { range };
    return { changes: [{ from: range.from, insert: open }, { from: range.to, insert: spanClose }],
      range: EditorSelection.range(range.from + open.length, range.to + open.length) };
  }), { scrollIntoView: true, userEvent: 'input.format' });
}

// openOn reports whether the selection starts after an open on its line that has not been closed yet.
function openOn(state: EditorState, open: string, close: string) {
  const { from } = state.selection.main;
  const before = state.sliceDoc(state.doc.lineAt(from).from, from);
  return before.lastIndexOf(open) >= 0 && before.lastIndexOf(open) > before.lastIndexOf(close);
}

function colorOn(state: EditorState) {
  const { from } = state.selection.main;
  const before = state.sliceDoc(state.doc.lineAt(from).from, from);
  const opens = [...before.matchAll(/<span data-color="([a-z]+)">/g)];
  const last = opens[opens.length - 1];
  return last && last.index > before.lastIndexOf(spanClose) ? last[1]! : '';
}

// Alignment and indent belong to a top-level paragraph or heading; a list, quote, table, code or HTML has neither.
const notTopLevel = ['ListItem', 'Blockquote', 'Table', 'FencedCode', 'CodeBlock', 'HTMLBlock'];
function formatAtOf(state: EditorState): FormatAt {
  const head = state.selection.main.head;
  if (state.doc.lineAt(head).text.trim() === '') return 'none';
  for (let node: ReturnType<ReturnType<typeof syntaxTree>['resolveInner']> | null = syntaxTree(state).resolveInner(head, -1); node; node = node.parent)
    if (notTopLevel.includes(node.name)) return 'none';
  return 'block';
}

interface Wrapped { first: Line; last: Line; tag?: FormatTag; open?: Line; close?: Line }

// blockOf is the run of lines the cursor is in and the <p>/<hN> wrapper around it, if it has one.
function blockOf(state: EditorState): Wrapped {
  const { doc } = state;
  let first = doc.lineAt(state.selection.main.head);
  let last = first;
  while (first.number > 1 && doc.line(first.number - 1).text.trim() !== '') first = doc.line(first.number - 1);
  while (last.number < doc.lines && doc.line(last.number + 1).text.trim() !== '') last = doc.line(last.number + 1);
  if (first.number < 3 || last.number > doc.lines - 2) return { first, last };
  const open = doc.line(first.number - 2);
  const close = doc.line(last.number + 2);
  const tag = parseTag(open.text);
  const end = parseTag(close.text);
  if (!tag || tag.close || tag.name === 'span' || !end?.close || end.name !== tag.name) return { first, last };
  if (doc.line(first.number - 1).text !== '' || doc.line(last.number + 1).text !== '') return { first, last };
  return { first, last, tag, open, close };
}

export function formatActive(state: EditorState): Pick<Active, 'underline' | 'highlight' | 'color' | 'align' | 'indent' | 'formatAt' | 'table' | 'headerRow' | 'canMerge' | 'canSplit'> {
  const formatAt = formatAtOf(state);
  const tag = formatAt === 'block' ? blockOf(state).tag : undefined;
  return {
    underline: openOn(state, '<u>', '</u>'),
    highlight: openOn(state, '<mark>', '</mark>'),
    color: colorOn(state),
    align: (tag?.attrs['data-align'] as Align | undefined) ?? 'left',
    indent: Number(tag?.attrs['data-indent'] ?? 0),
    formatAt,
    table: false, headerRow: false, canMerge: false, canSplit: false,
  };
}

// setBlockFormat changes the alignment or indent of the cursor's block: it writes, rewrites or takes away the
// <p>/<hN> wrapper, so a block at the default (left, no indent) stays plain Markdown.
export function setBlockFormat(view: EditorView, change: { align?: Align | null; indent?: 1 | -1 }) {
  const { state } = view;
  if (formatAtOf(state) !== 'block') return;
  const block = blockOf(state);
  const level = /^(#{1,6})\s/.exec(block.first.text)?.[1]?.length;
  const name = level ? `h${level}` : 'p';
  const align = change.align !== undefined ? change.align : block.tag?.attrs['data-align'] ?? null;
  const indent = Math.min(rules.indent, Math.max(0, Number(block.tag?.attrs['data-indent'] ?? 0) + (change.indent ?? 0)));
  const attrs: FormatTag['attrs'] = {};
  if (align) attrs['data-align'] = align;
  if (indent > 0) attrs['data-indent'] = String(indent);
  const open = tagString({ name, close: false, attrs });
  const close = tagString({ name, close: true, attrs: {} });
  const plain = Object.keys(attrs).length === 0;
  let changes: ChangeSpec[];
  if (block.open && block.close)
    changes = plain
      ? [{ from: block.open.from, to: block.first.from }, { from: block.last.to, to: block.close.to }]
      : [{ from: block.open.from, to: block.open.to, insert: open }, { from: block.close.from, to: block.close.to, insert: close }];
  else if (plain) return;
  else changes = [{ from: block.first.from, insert: `${open}\n\n` }, { from: block.last.to, insert: `\n\n${close}` }];
  const set = state.changes(changes);
  view.dispatch({ changes: set, selection: state.selection.map(set, 1), scrollIntoView: true, userEvent: 'input.format' });
}
