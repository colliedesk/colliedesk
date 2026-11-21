import { render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import shared from '../../../../server/internal/kbhtml/testdata/articles.json';
import formatting from '../../../../server/internal/kbformat/testdata/render.json';
import { MarkdownPreview } from './markdown-preview';

// The articles the help center renders too (server/internal/helpcenter/markdown_test.go).
describe('the preview shows an article as the help center does', () => {
  it.each(shared.cases.map((c) => [c.name, c] as const))('%s', (_name, c) => {
    const { container } = render(<MarkdownPreview text={c.markdown} />);
    container.querySelector('.kb-html-warning')?.remove();
    expect((container.textContent ?? '').split(/\s+/).filter(Boolean).join(' ')).toBe(c.text);
    expect(container.querySelectorAll('p')).toHaveLength(c.paragraphs);
    expect(container.querySelectorAll('iframe')).toHaveLength(c.iframes);
  });
});

// shape writes a node as its elements, sorted attributes and text, so two renderings compare whatever their
// line breaks between blocks. Heading ids are the help center's own.
function shape(node: Node): string {
  node.normalize();
  return shapeOf(node).replace(/\s*(<\/?(?:p|h[1-6]|table|tbody|tr|th|td)\b[^>]*>)\s*/g, '$1');
}

function shapeOf(node: Node): string {
  if (node.nodeType === Node.TEXT_NODE) return (node.nodeValue ?? '').replace(/\s+/g, ' ');
  if (node.nodeType !== Node.ELEMENT_NODE && node.nodeType !== Node.DOCUMENT_FRAGMENT_NODE) return '';
  const children = Array.from(node.childNodes).map(shapeOf).join('');
  if (node.nodeType === Node.DOCUMENT_FRAGMENT_NODE) return children;
  const element = node as Element;
  const attributes = Array.from(element.attributes).filter((a) => a.name !== 'id').map((a) => ` ${a.name}="${a.value}"`).sort().join('');
  return `<${element.localName}${attributes}>${children}</${element.localName}>`;
}

// The formatting the help center renders (server/internal/helpcenter/formatting_test.go): the same
// elements, classes and text.
describe('the preview shows formatting as the help center does', () => {
  it.each(formatting.cases.map((c) => [c.name, c] as const))('%s', (_name, c) => {
    const { container } = render(<MarkdownPreview text={c.markdown} />);
    container.querySelector('.kb-html-warning')?.remove();
    const want = document.createElement('template');
    want.innerHTML = c.html;
    const body = document.createDocumentFragment();
    body.append(...container.querySelector('.kb-preview')!.childNodes);
    expect(shape(body)).toBe(shape(want.content));
  });

  it('formatting it reads is shown, so nothing is named as left out', () => {
    const { container } = render(<MarkdownPreview text={formatting.cases[0]!.markdown} />);
    expect(container.querySelector('.kb-html-warning')).toBeNull();
  });
});

// GitHub's alert syntax shows as the help center shows it: a box a screen reader announces as a note,
// with the type named first (server/internal/helpcenter/callout_test.go).
describe('the preview shows callouts', () => {
  it.each([
    ['NOTE', 'note', 'Note'],
    ['TIP', 'tip', 'Tip'],
    ['IMPORTANT', 'important', 'Important'],
    ['WARNING', 'warning', 'Warning'],
    ['CAUTION', 'caution', 'Caution'],
    ['note', 'note', 'Note'],
  ])('[!%s]', (marker, type, label) => {
    const { container } = render(<MarkdownPreview text={`> [!${marker}]\n> Deleting a workspace cannot be undone.`} />);
    const callout = container.querySelector('aside');
    expect(callout?.className).toBe(`callout callout-${type}`);
    expect(callout?.getAttribute('role')).toBe('note');
    expect([...(callout?.querySelectorAll('p') ?? [])].map((p) => [p.className, p.textContent])).toEqual([
      ['callout-label', label],
      ['', 'Deleting a workspace cannot be undone.'],
    ]);
    expect(container.querySelector('blockquote')).toBeNull();
  });

  it.each([
    ['two trailing spaces', '> [!NOTE]  \n> Deleting a workspace cannot be undone.'],
    ['a backslash', '> [!NOTE]\\\n> Deleting a workspace cannot be undone.'],
  ])('a marker ended by a hard break (%s) is a callout, as on the server', (_name, text) => {
    const { container } = render(<MarkdownPreview text={text} />);
    const callout = container.querySelector('aside');
    expect(callout?.className).toBe('callout callout-note');
    expect([...(callout?.querySelectorAll('p') ?? [])].map((p) => [p.className, p.textContent])).toEqual([
      ['callout-label', 'Note'],
      ['', 'Deleting a workspace cannot be undone.'],
    ]);
    expect(callout?.querySelector('br')).toBeNull();
  });

  it.each([
    ['an unknown type', '> [!DANGER]\n> Unknown type.'],
    ['words after the marker', '> [!NOTE] same line\n> text.'],
    ['a marker inside a list', '> - [!NOTE]\n>   inside a list item'],
  ])('%s stays a quote', (_name, text) => {
    const { container } = render(<MarkdownPreview text={text} />);
    expect(container.querySelector('aside')).toBeNull();
    expect(container.querySelector('blockquote')).not.toBeNull();
  });
});
