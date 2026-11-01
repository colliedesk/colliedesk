import { render, screen } from '@testing-library/react';
import { expect, test, vi } from 'vitest';

// Until the renderer's chunk is there (or when it cannot load), a reply's words still show, as plain text.
test('a reply reads as plain text until its Markdown renderer has loaded, then as Markdown', async () => {
  let resolve: (module: typeof import('./markdown-render')) => void = () => undefined;
  vi.doMock('./markdown-render', () => new Promise((done) => { resolve = done as typeof resolve; }));
  const { MarkdownBody } = await import('./markdown-body');
  const { rerender } = render(<MarkdownBody text="Use the **secure link**." />);
  expect(screen.getByText('Use the **secure link**.')).toBeInTheDocument();
  const actual = await vi.importActual<typeof import('./markdown-render')>('./markdown-render');
  resolve(actual);
  await vi.waitFor(() => {
    rerender(<MarkdownBody text="Use the **secure link**." />);
    expect(screen.getByText('secure link').tagName).toBe('STRONG');
  });
});
