import { EditorSelection } from '@codemirror/state';
import type { ChangeSpec, EditorState, Text } from '@codemirror/state';
import type { EditorView } from '@codemirror/view';
import { syntaxTree } from '@codemirror/language';
import { redoDepth, undoDepth } from '@codemirror/commands';

// The toolbar edits Markdown source: it adds or takes away the syntax around the selection, it never parses into a
// document model, so the text stays exactly what is stored in body_md.

export type Block = 'paragraph' | 'h1' | 'h2' | 'h3';

export interface Active {
  block: Block;
  strong: boolean;
  emphasis: boolean;
  code: boolean;
  link: boolean;
  bullet: boolean;
  ordered: boolean;
  task: boolean;
  quote: boolean;
  canUndo: boolean;
  canRedo: boolean;
}

const headingOf = (line: string): Block => {
  const match = /^(#{1,6})\s/.exec(line);
  if (!match) return 'paragraph';
  return match[1].length <= 3 ? (`h${match[1].length}` as Block) : 'paragraph';
};

// inside reports whether the cursor sits in a node of one of the names (Emphasis, StrongEmphasis…).
function inside(state: EditorState, names: string[]) {
  const pos = state.selection.main.head;
  for (const side of [-1, 1] as const) {
    for (
      let node: ReturnType<ReturnType<typeof syntaxTree>['resolveInner']> | null = syntaxTree(state).resolveInner(
        pos,
        side,
      );
      node;
      node = node.parent
    )
      if (names.includes(node.name)) return true;
  }
  return false;
}

export function activeOf(state: EditorState): Active {
  const line = state.doc.lineAt(state.selection.main.head).text;
  return {
    block: headingOf(line),
    strong: inside(state, ['StrongEmphasis']),
    emphasis: inside(state, ['Emphasis']),
    code: inside(state, ['InlineCode', 'FencedCode']),
    link: inside(state, ['Link']),
    bullet: /^\s*[-*+]\s(?!\[[ xX]\]\s)/.test(line),
    ordered: /^\s*\d+[.)]\s/.test(line),
    task: /^\s*[-*+]\s\[[ xX]\]\s/.test(line),
    quote: /^\s*>/.test(line),
    canUndo: undoDepth(state) > 0,
    canRedo: redoDepth(state) > 0,
  };
}

export const sameActive = (a: Active | undefined, b: Active) =>
  !!a && (Object.keys(b) as (keyof Active)[]).every((key) => a[key] === b[key]);

// toggleMark wraps each selection in mark (** for bold), or takes the mark off when it is already there, inside or
// just outside the selection. An empty selection gets the pair with the cursor between.
export function toggleMark(view: EditorView, mark: string) {
  const { state } = view;
  const single = mark === '*';
  // A lone * next to another * belongs to ** (bold), so italic does not take it.
  const isMarkAt = (from: number, to: number) =>
    state.sliceDoc(from, to) === mark &&
    (!single ||
      (state.sliceDoc(from - 1, from) !== '*' && state.sliceDoc(to, to + 1) !== '*') ||
      state.sliceDoc(from - 2, to + 2) === '***');
  view.dispatch(
    state.changeByRange((range) => {
      const len = mark.length;
      const text = state.sliceDoc(range.from, range.to);
      if (
        text.length >= len * 2 &&
        text.startsWith(mark) &&
        text.endsWith(mark) &&
        (!single || !text.startsWith('**') || text.startsWith('***'))
      )
        return {
          changes: [
            { from: range.from, to: range.from + len },
            { from: range.to - len, to: range.to },
          ],
          range: EditorSelection.range(range.from, range.to - len * 2),
        };
      if (isMarkAt(range.from - len, range.from) && isMarkAt(range.to, range.to + len))
        return {
          changes: [
            { from: range.from - len, to: range.from },
            { from: range.to, to: range.to + len },
          ],
          range: EditorSelection.range(range.from - len, range.to - len),
        };
      return {
        changes: [
          { from: range.from, insert: mark },
          { from: range.to, insert: mark },
        ],
        range: EditorSelection.range(range.from + len, range.to + len),
      };
    }),
    { scrollIntoView: true, userEvent: 'input.format' },
  );
}

// inlineCode wraps a selection on one line in backticks; a selection over several lines becomes a fenced block.
export function inlineCode(view: EditorView) {
  const { state } = view;
  const range = state.selection.main;
  if (state.doc.lineAt(range.from).number === state.doc.lineAt(range.to).number) return toggleMark(view, '`');
  const from = state.doc.lineAt(range.from).from;
  const to = state.doc.lineAt(range.to).to;
  view.dispatch({
    changes: [
      { from, insert: '```\n' },
      { from: to, insert: '\n```' },
    ],
    selection: EditorSelection.range(from + 4, to + 4),
    userEvent: 'input.format',
  });
}

const listPrefix = /^(\s*)(?:[-*+]\s\[[ xX]\]\s|[-*+]\s|\d+[.)]\s)/;
const linesOf = (state: EditorState) => {
  const seen = new Set<number>();
  const lines: { from: number; text: string; number: number }[] = [];
  for (const range of state.selection.ranges) {
    const last = state.doc.lineAt(range.to).number;
    for (let at = state.doc.lineAt(range.from).number; at <= last; at += 1) {
      if (seen.has(at)) continue;
      seen.add(at);
      lines.push(state.doc.line(at));
    }
  }
  return lines;
};

export type LineKind = 'bullet' | 'ordered' | 'task' | 'quote';
const prefixOf: Record<LineKind, (index: number) => string> = {
  bullet: () => '- ',
  ordered: (index) => `${index + 1}. `,
  task: () => '- [ ] ',
  quote: () => '> ',
};
const hasKind: Record<LineKind, RegExp> = {
  bullet: /^\s*[-*+]\s(?!\[[ xX]\]\s)/,
  ordered: /^\s*\d+[.)]\s/,
  task: /^\s*[-*+]\s\[[ xX]\]\s/,
  quote: /^\s*>\s?/,
};

// toggleLines puts a list, checklist or quote marker on every selected line, or takes it off when every line has it.
// Switching between list kinds replaces the old marker instead of stacking a second one.
export function toggleLines(view: EditorView, kind: LineKind) {
  const { state } = view;
  const lines = linesOf(state).filter((line, _, all) => all.length === 1 || line.text.trim() !== '');
  const off = lines.every((line) => hasKind[kind].test(line.text));
  const changes: ChangeSpec[] = lines.map((line, index) => {
    if (off) {
      const match = (kind === 'quote' ? hasKind.quote : listPrefix).exec(line.text)!;
      const indent = kind === 'quote' ? '' : (match[1] ?? '');
      return { from: line.from + indent.length, to: line.from + match[0].length };
    }
    const current = kind === 'quote' ? null : listPrefix.exec(line.text);
    const indent = current?.[1] ?? /^\s*/.exec(line.text)![0];
    return {
      from: line.from + indent.length,
      to: line.from + (current ? current[0].length : indent.length),
      insert: prefixOf[kind](index),
    };
  });
  dispatchLines(view, changes);
}

// dispatchLines applies line marker changes; a cursor where a marker goes in ends up after it, ready to type.
function dispatchLines(view: EditorView, changes: ChangeSpec[]) {
  const set = view.state.changes(changes);
  view.dispatch({ changes: set, selection: view.state.selection.map(set, 1), scrollIntoView: true, userEvent: 'input.format' });
}

// setHeading makes the cursor's lines a heading of that level, or plain text.
export function setHeading(view: EditorView, block: Block) {
  const hashes = block === 'paragraph' ? '' : `${'#'.repeat(Number(block[1]))} `;
  const changes = linesOf(view.state).map((line) => {
    const current = /^#{1,6}\s+/.exec(line.text);
    return { from: line.from, to: line.from + (current ? current[0].length : 0), insert: hashes };
  });
  dispatchLines(view, changes);
}

// insertLink turns the selection into [text](address) and selects the part to type next: the address, or the text
// when the selection already was an address.
export function insertLink(view: EditorView) {
  const { state } = view;
  view.dispatch(
    state.changeByRange((range) => {
      const text = state.sliceDoc(range.from, range.to);
      if (/^(https?:\/\/|mailto:)\S+$/.test(text)) {
        const insert = `[](${text})`;
        return { changes: { from: range.from, to: range.to, insert }, range: EditorSelection.cursor(range.from + 1) };
      }
      const label = text || 'link text';
      const insert = `[${label}](https://)`;
      const start = range.from + label.length + 3;
      return {
        changes: { from: range.from, to: range.to, insert },
        range: text
          ? EditorSelection.range(start, start + 8)
          : EditorSelection.range(range.from + 1, range.from + 1 + label.length),
      };
    }),
    { scrollIntoView: true, userEvent: 'input.format' },
  );
}

// blockAt is where a block (image, table) goes so it stands as a paragraph of its own: after the run of non-blank
// lines the position is in (a paragraph, a table, a list), never inside a line, a table row or a word.
export function blockAt(doc: Text, pos: number) {
  let line = doc.lineAt(pos);
  if (line.text.trim() === '') return line.from;
  while (line.number < doc.lines && doc.line(line.number + 1).text.trim() !== '') line = doc.line(line.number + 1);
  return line.to;
}

// blockChange is the insert of text at a block boundary, padded with a blank line on each side.
export function blockChange(doc: Text, at: number, text: string) {
  const before = doc.sliceString(Math.max(0, at - 2), at);
  const after = doc.sliceString(at, at + 2);
  const lead = at === 0 || before === '\n\n' ? '' : before.endsWith('\n') ? '\n' : '\n\n';
  const tail = at === doc.length ? '\n' : after === '\n\n' ? '' : after.startsWith('\n') ? '\n' : '\n\n';
  return { from: at, insert: `${lead}${text}${tail}`, start: at + lead.length };
}

export function insertBlock(view: EditorView, text: string, select?: [number, number]) {
  const { state } = view;
  const change = blockChange(state.doc, blockAt(state.doc, state.selection.main.head), text);
  view.dispatch({
    changes: { from: change.from, insert: change.insert },
    selection: select
      ? EditorSelection.range(change.start + select[0], change.start + select[1])
      : EditorSelection.cursor(change.start + text.length),
    scrollIntoView: true,
    userEvent: 'input.format',
  });
}

export const tableTemplate = '| Column | Column |\n| --- | --- |\n| Text | Text |';
export const codeBlockTemplate = '```\ncode\n```';
