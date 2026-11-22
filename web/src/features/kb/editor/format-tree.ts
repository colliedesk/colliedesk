import { isBlock, isCell, isInline, isTable, parseTag, parseTags, rules, within } from './format-tags';
import type { FormatTag } from './format-tags';

// A Markdown tree (mdast) as the formatting reader sees it. formatTree adds these node types: kbInline
// (a <u>, <mark> or <span> with what it wraps), kbTable → kbRow → kbCell (a table written as HTML, each
// cell holding blocks), kbFigure (an image, or a link around one, and an optional kbCaption holding the
// caption's inline text), and data.kbWrap on a paragraph or heading its <p>/<hN> wrapper gave attributes.
export interface FormatNode {
  type: string;
  value?: string;
  depth?: number;
  tag?: FormatTag;
  children?: FormatNode[];
  position?: { start: Point; end: Point };
  data?: { kbWrap?: FormatTag; hName?: string; hProperties?: Record<string, unknown> } & Record<string, unknown>;
}

interface Point { line: number; column: number; offset?: number }

// span is the source range from the first node's start to the last node's end, so a node formatTree
// builds or unwraps still points at all of its text.
const span = (first: FormatNode | undefined, last: FormatNode | undefined) =>
  first?.position && last?.position ? { start: first.position.start, end: last.position.end } : undefined;

// Parents whose children are blocks; their raw HTML is an HTML block of its own.
export const flowParents = ['root', 'blockquote', 'listItem', 'footnoteDefinition', 'kbCell'];

// formatTree reads a parsed text's formatting HTML in place, as the server's kbformat.Transformer does:
// formatting HTML it cannot read stays raw HTML, for the page's own rules.
export function formatTree(parent: FormatNode) {
  if (!parent.children) return;
  if (flowParents.includes(parent.type)) {
    buildTables(parent);
    buildFigures(parent);
    unwrapBlocks(parent);
  } else {
    pairInline(parent);
  }
  for (const child of parent.children) formatTree(child);
}

const tagsOf = (node: FormatNode | undefined) => (node?.type === 'html' ? parseTags(node.value ?? '') : undefined);

// pairInline turns each opening inline tag and the closing tag that matches it at the same level into a
// kbInline node. A tag without its partner at that level (one crossing the edge of a link or an emphasis)
// stays raw HTML, which the page leaves out.
function pairInline(parent: FormatNode) {
  const out: FormatNode[] = [];
  const stack: { at: number; tag: FormatTag }[] = [];
  for (const child of parent.children!) {
    const tag = child.type === 'html' ? parseTag(child.value ?? '') : undefined;
    if (!tag || !isInline(tag)) {
      out.push(child);
    } else if (!tag.close) {
      stack.push({ at: out.length, tag });
      out.push(child);
    } else if (stack.at(-1)?.tag.name === tag.name) {
      const opened = stack.pop()!;
      const inner = out.splice(opened.at).slice(1);
      out.push({ type: 'kbInline', tag: opened.tag, children: inner });
    } else {
      // A closing tag that crosses an inner opening (<u><mark></u>) leaves both unpaired; one with no
      // opening at this level is left alone.
      const at = stack.map((open) => open.tag.name).lastIndexOf(tag.name);
      if (at >= 0) stack.length = at;
      out.push(child);
    }
  }
  parent.children = out;
}

// unwrapBlocks finds each paragraph or heading between an opening <p> or <hN> block and its closing
// block, keeps the wrapper's tag on it and removes the wrapper.
function unwrapBlocks(parent: FormatNode) {
  const children = parent.children!;
  for (let at = 0; at < children.length; at++) {
    const opening = tagsOf(children[at]);
    const inner = children[at + 1];
    if (opening?.length !== 1 || opening[0]!.close || !isBlock(opening[0]!) || !inner || !wrappable(inner, opening[0]!.name)) continue;
    const closing = tagsOf(children[at + 2]);
    if (closing?.length !== 1 || !closing[0]!.close || closing[0]!.name !== opening[0]!.name) continue;
    inner.data = { ...inner.data, kbWrap: opening[0]! };
    inner.position = span(children[at], children[at + 2]) ?? inner.position;
    children.splice(at, 3, inner);
  }
}

const wrappable = (node: FormatNode, name: string) => (name === 'p' ? node.type === 'paragraph' : node.type === 'heading' && `h${node.depth}` === name);

// buildTables replaces each run of children that spells a table (HTML blocks of table, row and cell tags,
// with the blocks of each cell between its tags) with a kbTable node. A run that breaks the shape (text
// outside a cell, another tag, no </table>) is left as it was.
function buildTables(parent: FormatNode) {
  const children = parent.children!;
  for (let at = 0; at < children.length; at++) {
    const tags = tagsOf(children[at]);
    if (!tags || tags[0]!.close || tags[0]!.name !== 'table') continue;
    const read = readTable(children, at);
    if (read) children.splice(at, read.last - at + 1, read.table);
  }
}

// readTable reads the table starting at nodes[first]; it returns the table and the index of the last
// node it took.
function readTable(nodes: FormatNode[], first: number): { table: FormatNode; last: number } | undefined {
  const rows: FormatNode[][] = [];
  let row: FormatNode[] = [];
  let cell: FormatNode | undefined;
  let inRow = false;
  for (let at = first; at < nodes.length; at++) {
    const tags = tagsOf(nodes[at]);
    // A block with no table tag in it is the cell's content, a paragraph's wrapper too.
    if (!tags?.some(isTable)) {
      if (!cell) return undefined;
      cell.children!.push(nodes[at]!);
      continue;
    }
    for (const [i, tag] of tags.entries()) {
      if (tag.name === 'table' && !tag.close && at === first && i === 0) continue;
      if (tag.name === 'tr' && !tag.close && !inRow && !cell) {
        inRow = true;
        row = [];
      } else if (isCell(tag) && !tag.close && inRow && !cell) {
        cell = { type: 'kbCell', tag, children: [] };
      } else if (isCell(tag) && tag.close && cell?.tag!.name === tag.name) {
        row.push(cell);
        cell = undefined;
      } else if (tag.name === 'tr' && tag.close && inRow && !cell && row.length > 0) {
        rows.push(row);
        inRow = false;
      } else if (tag.name === 'table' && tag.close && !inRow && rows.length > 0 && i === tags.length - 1) {
        const table: FormatNode = { type: 'kbTable', children: rows.map((cells) => ({ type: 'kbRow', children: cells })) };
        table.position = span(nodes[first], nodes[at]);
        return { table, last: at };
      } else {
        return undefined;
      }
    }
  }
  return undefined;
}

// figureTag reads node as the one tag <name>, or </name> when closing.
function figureTag(node: FormatNode | undefined, name: string, closing: boolean) {
  const tags = tagsOf(node);
  return tags?.length === 1 && tags[0]!.name === name && tags[0]!.close === closing ? tags[0] : undefined;
}

// imageOf is the image a figure shows: the node itself, or the only child of a link around it.
export function imageOf(node: FormatNode | undefined): FormatNode | undefined {
  const inner = node?.type === 'link' && node.children?.length === 1 ? node.children[0] : node;
  return inner?.type === 'image' ? inner : undefined;
}

// buildFigures replaces each run of children that spells an image's figure (an opening <figure> block, a
// paragraph holding only an image or a link around one, an optional <figcaption> block, a paragraph and
// </figcaption>, then </figure>) with a kbFigure node. A run that breaks the shape is left as it was.
function buildFigures(parent: FormatNode) {
  const children = parent.children!;
  for (let at = 0; at < children.length; at++) {
    const opening = figureTag(children[at], 'figure', false);
    const picture = children[at + 1];
    if (!opening || picture?.type !== 'paragraph' || picture.children?.length !== 1 || !imageOf(picture.children[0])) continue;
    let last = at + 2;
    let caption: FormatNode | undefined;
    if (figureTag(children[last], 'figcaption', false)) {
      caption = children[last + 1];
      if (caption?.type !== 'paragraph' || !figureTag(children[last + 2], 'figcaption', true)) continue;
      last += 3;
    }
    if (!figureTag(children[last], 'figure', true)) continue;
    const shown = picture.children[0]!;
    const width = opening.attrs['data-width'];
    if (width && within(width, 1, rules.figure.pixels)) {
      const image = imageOf(shown)!;
      image.data = { ...image.data, hProperties: { ...image.data?.hProperties, width } };
    }
    const figure: FormatNode = { type: 'kbFigure', tag: opening, children: [shown] };
    if (caption) figure.children!.push({ type: 'kbCaption', children: caption.children ?? [], position: caption.position });
    figure.position = span(children[at], children[last]);
    children.splice(at, last - at + 1, figure);
  }
}
