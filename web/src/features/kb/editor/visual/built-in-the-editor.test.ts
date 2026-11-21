import { describe, expect, it } from 'vitest';
import { markdownToDoc } from './from-markdown';
import { schema } from './schema';
import { docToMarkdown } from './to-markdown';

// Documents a person can build in the visual editor that no Markdown text reads to.
const built = (...content: object[]) => {
  const doc = schema.nodeFromJSON({ type: 'doc', content });
  doc.check();
  return doc;
};
const p = (...content: object[]) => ({ type: 'paragraph', content });
const t = (text: string) => ({ type: 'text', text });
const items = (type: string, ...texts: string[]) =>
  texts.map((text) => ({ type, ...(type === 'taskItem' ? { attrs: { checked: false } } : {}), content: [p(t(text))] }));

// under is a one-item list whose item holds a line of text and the block under it.
const under = (block: object) => ({ type: 'bulletList', content: [{ type: 'listItem', content: [p(t('a')), block] }] });
const cell = (type: string, text: string) => ({ type, content: [p(t(text))] });
const cells = { type: 'table', content: [{ type: 'tableRow', content: [cell('tableHeader', 'h')] }, { type: 'tableRow', content: [cell('tableCell', '1')] }] };

const kept =(text: string) => {
  const sources: string[] = [];
  markdownToDoc(text).descendants((node) => {
    if (node.type.name === 'markdownBlock') sources.push(node.attrs.source as string);
  });
  return sources;
};

describe('a document built in the editor reads back as itself', () => {
  it('a line break at the end of a paragraph is dropped, never written as a backslash', () => {
    expect(docToMarkdown(built(p(t('foo'), { type: 'hardBreak' })))).toBe('foo\n');
    expect(docToMarkdown(built(p(t('a'), { type: 'hardBreak' }, t('b'))))).toBe('a\\\nb\n');
  });

  it.each([
    ['an empty quote', [{ type: 'blockquote', content: [{ type: 'paragraph' }] }]],
    ['two bullet lists in a row', [{ type: 'bulletList', content: items('listItem', 'a') }, { type: 'bulletList', content: items('listItem', 'b') }]],
    ['a bullet list then a task list', [{ type: 'bulletList', content: items('listItem', 'a') }, { type: 'taskList', content: items('taskItem', 'b') }]],
    ['two numbered lists in a row', [{ type: 'orderedList', content: items('listItem', 'a') }, { type: 'orderedList', attrs: { start: 5 }, content: items('listItem', 'b') }]],
    ['a list item with a rule under its text', [under({ type: 'horizontalRule' })]],
    ['a list item with a list under its text', [under({ type: 'bulletList', content: items('listItem', 'b') })]],
    ['a list item with a table under its text', [under(cells)]],
    ['a list item with a quote under its text', [under({ type: 'blockquote', content: [p(t('q'))] })]],
    ['a list item with code under its text', [under({ type: 'codeBlock', content: [t('x')] })]],
    ['an empty list item', [{ type: 'bulletList', content: [{ type: 'listItem', content: [{ type: 'paragraph' }] }, ...items('listItem', 'b')] }]],
  ])('%s', (_name, content) => {
    const doc = built(...content);
    const text = docToMarkdown(doc);
    expect(kept(text)).toEqual([]);
    expect(markdownToDoc(text).toJSON()).toEqual(doc.toJSON());
  });

  // Markdown lets neither start a list right under a line of text, so a blank line comes first: the
  // list keeps its words and numbers, and its items show as paragraphs.
  it('a list from 5, or one whose first item is empty, under a list item\'s text stands a line apart', () => {
    expect(docToMarkdown(built(under({ type: 'orderedList', attrs: { start: 5 }, content: items('listItem', 'b') })))).toBe('- a\n\n  5. b\n');
    expect(docToMarkdown(built(under({ type: 'bulletList', content: [{ type: 'listItem', content: [{ type: 'paragraph' }] }] })))).toBe('- a\n\n  -\n');
    const inner = (text: string) => markdownToDoc(text).firstChild!.firstChild!.lastChild!;
    expect(inner('- a\n\n  5. b\n').type.name).toBe('orderedList');
    expect(inner('- a\n\n  5. b\n').attrs.start).toBe(5);
    expect(inner('- a\n\n  -\n').type.name).toBe('bulletList');
  });
});
