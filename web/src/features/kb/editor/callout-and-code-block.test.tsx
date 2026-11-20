import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { EditorView } from '@codemirror/view';
import { expect, test, vi } from 'vitest';
import CodeMirrorEditor from './codemirror-editor';
import type { EditorHandle, UploadHooks } from './codemirror-editor';

if (!('getClientRects' in Range.prototype)) Object.assign(Range.prototype, { getClientRects: () => [], getBoundingClientRect: () => new DOMRect() });

const never: UploadHooks = { accept: (files) => files, upload: () => new Promise(() => {}), onStart: () => {}, onDone: () => {}, onError: () => {} };

async function make(initial: string) {
  let handle: EditorHandle | undefined;
  render(<CodeMirrorEditor initial={initial} uploads={never} label="Text" describedBy="help" invalid={false} mode="edit" onChange={vi.fn()}
    onReady={(ready) => { handle = ready; }} />);
  await waitFor(() => expect(handle).toBeDefined());
  const text = screen.getByRole('textbox', { name: 'Text' });
  const selected = () => {
    const view = EditorView.findFromDOM(text)!;
    return view.state.sliceDoc(view.state.selection.main.from, view.state.selection.main.to);
  };
  return { handle: handle!, text, selected };
}

test.each([
  ['Note callout', '> [!NOTE]\n> Text'],
  ['Tip callout', '> [!TIP]\n> Text'],
  ['Important callout', '> [!IMPORTANT]\n> Text'],
  ['Warning callout', '> [!WARNING]\n> Text'],
  ['Caution callout', '> [!CAUTION]\n> Text'],
])('the Insert menu adds a %s as GitHub alert syntax, its words selected to type over', async (name, block) => {
  const { handle, selected } = await make('Intro');
  fireEvent.click(screen.getByRole('button', { name: 'Insert image, video or callout' }));
  fireEvent.click(screen.getByRole('menuitem', { name: name }));
  expect(handle.markdown()).toBe(`Intro\n\n${block}\n`);
  expect(selected()).toBe('Text');
});

const openCodeBlock = () => {
  fireEvent.click(screen.getByRole('button', { name: 'More formatting' }));
  fireEvent.click(screen.getByRole('menuitem', { name: 'Code block' }));
};

test('Code block asks for the language first and writes it after the fence, the code selected to type over', async () => {
  const { handle, text, selected } = await make('Intro');
  openCodeBlock();
  expect(screen.getByRole('dialog', { name: 'Code block' })).toBeInTheDocument();
  const language = screen.getByRole('combobox', { name: 'Language' });
  await waitFor(() => expect(language).toHaveFocus());
  fireEvent.change(language, { target: { value: ' bash ' } });
  fireEvent.keyDown(language, { key: 'Enter' });
  expect(handle.markdown()).toBe('Intro\n\n```bash\ncode\n```\n');
  expect(selected()).toBe('code');
  expect(screen.queryByRole('dialog', { name: 'Code block' })).not.toBeInTheDocument();
  await waitFor(() => expect(text).toHaveFocus());
});

test('a code block with no language is plain text; a language with a space is refused and says why', async () => {
  const { handle } = await make('Intro');
  openCodeBlock();
  const language = screen.getByRole('combobox', { name: 'Language' });
  fireEvent.change(language, { target: { value: 'shell script' } });
  fireEvent.click(screen.getByRole('button', { name: 'Insert' }));
  expect(screen.getByRole('alert')).toHaveTextContent('A language is one word, such as bash, go or json.');
  expect(language).toHaveAttribute('aria-invalid', 'true');
  expect(handle.markdown()).toBe('Intro');

  fireEvent.change(language, { target: { value: '' } });
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Insert' })); });
  expect(handle.markdown()).toBe('Intro\n\n```\ncode\n```\n');
});
