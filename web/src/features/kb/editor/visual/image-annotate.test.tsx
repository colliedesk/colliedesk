import { act, fireEvent, screen, within } from '@testing-library/react';
import type { Editor } from '@tiptap/core';
import { NodeSelection } from '@tiptap/pm/state';
import { expect, test, vi } from 'vitest';
import type { ImageAnnotation } from '../editor-types';
import { make, never } from './visual-editor.test.fixture';

const own = '/api/v1/kb/images/5f0c6a52-8d1e-4b7a-9c3f-2e6d4a1b0c97';

function pickImage(editor: Editor) {
  let at = -1;
  editor.state.doc.descendants((node, pos) => {
    if (at < 0 && node.type.name === 'image') at = pos;
  });
  act(() => { editor.view.dispatch(editor.state.tr.setSelection(NodeSelection.create(editor.state.doc, at))); });
}
const bar = () => screen.getByRole('toolbar', { name: 'Image' });
const annotation = (check: () => string | undefined = () => undefined): ImageAnnotation => ({ check, save: vi.fn(() => Promise.resolve()) });

test('Annotate is offered on an image of the media library, and not on one from another site', async () => {
  const { editor } = await make(`![Logo](https://example.com/logo.png)\n\n![Alt](${own})\n`, never, annotation());
  pickImage(editor);
  expect(within(bar()).queryByRole('button', { name: 'Annotate' })).not.toBeInTheDocument();
  let second = -1;
  editor.state.doc.descendants((node, pos) => { if (node.type.name === 'image') second = pos; });
  act(() => { editor.view.dispatch(editor.state.tr.setSelection(NodeSelection.create(editor.state.doc, second))); });
  expect(within(bar()).getByRole('button', { name: 'Annotate' })).toBeInTheDocument();
});

test('Annotate is not offered where the text has no article to put a copy in', async () => {
  const { editor } = await make(`![Alt](${own})\n`);
  pickImage(editor);
  expect(within(bar()).queryByRole('button', { name: 'Annotate' })).not.toBeInTheDocument();
});

test('Annotate on an article that cannot take a copy yet says why and opens nothing', async () => {
  const { editor } = await make(`![Alt](${own})\n`, never, annotation(() => 'Save the article first.'));
  pickImage(editor);
  fireEvent.click(within(bar()).getByRole('button', { name: 'Annotate' }));
  expect(screen.getByRole('alert')).toHaveTextContent('Save the article first.');
  expect(screen.queryByRole('dialog', { name: 'Annotate image' })).not.toBeInTheDocument();
});

test('Annotate opens the drawing tools on the image, with nothing to save until something is drawn', async () => {
  const { editor } = await make(`![Alt](${own})\n`, never, annotation());
  pickImage(editor);
  fireEvent.click(within(bar()).getByRole('button', { name: 'Annotate' }));
  const dialog = screen.getByRole('dialog', { name: 'Annotate image' });
  for (const tool of ['Arrow', 'Box', 'Text', 'Blur']) expect(within(dialog).getByRole('button', { name: tool })).toBeInTheDocument();
  expect(within(dialog).getByRole('button', { name: 'Arrow' })).toHaveAttribute('aria-pressed', 'true');
  fireEvent.click(within(dialog).getByRole('button', { name: 'Blur' }));
  expect(within(dialog).getByRole('button', { name: 'Blur' })).toHaveAttribute('aria-pressed', 'true');
  expect(within(dialog).getByRole('button', { name: 'Arrow' })).toHaveAttribute('aria-pressed', 'false');
  expect(within(dialog).getByRole('button', { name: 'Save copy' })).toBeDisabled();
  fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
  expect(screen.queryByRole('dialog', { name: 'Annotate image' })).not.toBeInTheDocument();
});
