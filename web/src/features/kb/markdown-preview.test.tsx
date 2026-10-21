import { render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import shared from '../../../../server/internal/kbhtml/testdata/articles.json';
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
