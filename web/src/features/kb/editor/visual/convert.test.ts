import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import type { Node } from '@tiptap/pm/model';
import { describe, expect, it } from 'vitest';
import articles from '../../../../../../server/internal/kbhtml/testdata/articles.json';
import normalize from '../../../../../../server/internal/kbformat/testdata/normalize.json';
import rendered from '../../../../../../server/internal/kbformat/testdata/render.json';
import { parseTag, tagString } from '../format-tags';
import { markdownToDoc } from './from-markdown';
import { docToMarkdown } from './to-markdown';

const everything = readFileSync(join(process.cwd(), '../server/internal/kbformat/testdata/visual-editor.md'), 'utf8');
const save = (text: string) => docToMarkdown(markdownToDoc(text));

function inventory(doc: Node) {
  const nodes = new Set<string>();
  const marks = new Set<string>();
  doc.descendants((node) => {
    nodes.add(node.type.name);
    for (const mark of node.marks) marks.add(mark.type.name);
  });
  return { nodes: [...nodes].sort(), marks: [...marks].sort() };
}

const blocksKept = (doc: Node) => {
  const sources: string[] = [];
  doc.descendants((node) => {
    if (node.type.name === 'markdownBlock') sources.push(node.attrs.source as string);
  });
  return sources;
};

// The server keeps what the visual editor writes as it is (server/internal/kbformat/normalize_test.go).
describe('the visual editor saves an article as it read it', () => {
  it('a text holding every element it writes comes back byte for byte', () => {
    expect(save(everything)).toBe(everything);
  });

  it('it reads each of those elements as itself, none as a Markdown block', () => {
    expect(inventory(markdownToDoc(everything))).toEqual({
      nodes: ['blockquote', 'bulletList', 'callout', 'codeBlock', 'hardBreak', 'heading', 'horizontalRule', 'image', 'listItem',
        'orderedList', 'paragraph', 'table', 'tableCell', 'tableHeader', 'tableRow', 'taskItem', 'taskList', 'text'],
      marks: ['bold', 'code', 'color', 'highlight', 'italic', 'link', 'strike', 'underline'],
    });
  });

  it('a block it cannot show keeps its source, as written', () => {
    const text = 'Intro\n\n<div class="x">\n<kbd>Ctrl</kbd>\n</div>\n\nSee [the guide][g].\n\n[g]: https://example.com/guide\n';
    const doc = markdownToDoc(text);
    expect(blocksKept(doc)).toEqual(['<div class="x">\n<kbd>Ctrl</kbd>\n</div>', 'See [the guide][g].', '[g]: https://example.com/guide']);
    expect(docToMarkdown(doc)).toBe(text);
  });

  it('a table whose cells do not make a grid stays as written', () => {
    const text = '<table>\n<tr>\n<td colspan="2">\n\nA\n\n</td>\n</tr>\n<tr>\n<td>\n\nB\n\n</td>\n</tr>\n</table>\n';
    expect(blocksKept(markdownToDoc(text))).toEqual([text.trimEnd()]);
  });

  it('a list keeps the blank lines between its items', () => {
    expect(save('- one\n\n- two\n')).toBe('- one\n\n- two\n');
    expect(save('1. one\n2. two\n')).toBe('1. one\n2. two\n');
  });

  it('a table with merged or coloured cells is written as HTML, a plain one as Markdown', () => {
    expect(save('| a | b |\n| --- | --- |\n| 1 | 2 |\n')).toBe('| a | b |\n| - | - |\n| 1 | 2 |\n');
    expect(save('<table>\n<tr>\n<th>\n\na\n\n</th>\n</tr>\n<tr>\n<td>\n\n1\n\n</td>\n</tr>\n</table>\n')).toBe('| a |\n| - |\n| 1 |\n');
    expect(save('<table>\n<tr>\n<td>\n\nno header\n\n</td>\n</tr>\n</table>\n')).toBe('<table>\n<tr>\n<td>\n\nno header\n\n</td>\n</tr>\n</table>\n');
  });

  it('spaces at the edge of bold go outside it, so the text stays bold', () => {
    const doc = markdownToDoc('a **b** c\n');
    const bold = doc.type.schema.marks.bold!.create();
    const paragraph = doc.type.schema.nodes.paragraph!.create(null, [
      doc.type.schema.text('a'), doc.type.schema.text(' b ', [bold]), doc.type.schema.text('c'),
    ]);
    expect(docToMarkdown(doc.type.create(null, paragraph))).toBe('a **b** c\n');
  });
});

const corpus = [
  ...normalize.map((c) => [c.name, c.out] as const),
  ...rendered.cases.map((c) => [c.name, c.markdown] as const),
  ...articles.cases.map((c) => [c.name, c.markdown] as const),
];
// tags are the HTML tags outside code, where a tag is only text.
const tags = (text: string) => text.replace(/^```[^]*?^```$|`[^`\n]*`/gm, '').match(/<\/?[A-Za-z][^>]*>/g) ?? [];

describe('saving any article twice changes nothing more', () => {
  it.each(corpus)('%s', (_name, text) => {
    const once = save(text);
    expect(save(once)).toBe(once);
    for (const tag of tags(once)) {
      const read = parseTag(tag);
      if (read) expect(tagString(read)).toBe(tag);
    }
  });
});
