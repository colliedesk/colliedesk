import { act, fireEvent, screen, within } from '@testing-library/react';
import type { Editor } from '@tiptap/core';
import { NodeSelection, TextSelection } from '@tiptap/pm/state';
import { expect, test } from 'vitest';
import { make } from './visual-editor.test.fixture';

const src = '/api/v1/kb/images/5f0c6a52-8d1e-4b7a-9c3f-2e6d4a1b0c97';

// pickImage selects the first image or figure, as a click on it does.
function pickImage(editor: Editor) {
  let at = -1;
  editor.state.doc.descendants((node, pos) => {
    if (at < 0 && (node.type.name === 'image' || node.type.name === 'articleFigure')) at = pos;
  });
  act(() => { editor.view.dispatch(editor.state.tr.setSelection(NodeSelection.create(editor.state.doc, at))); });
}
const bar = () => screen.getByRole('toolbar', { name: 'Image' });

test('no image bar shows until an image is picked', async () => {
  const { editor } = await make(`Intro\n\n![Alt](${src})\n`);
  act(() => { editor.commands.setTextSelection(2); });
  expect(screen.queryByRole('toolbar', { name: 'Image' })).not.toBeInTheDocument();
  pickImage(editor);
  expect(bar()).toBeInTheDocument();
});

test('a width in pixels and an alignment put the image in a figure the server keeps', async () => {
  const { handle, editor } = await make(`![Alt](${src})\n`);
  pickImage(editor);
  fireEvent.change(within(bar()).getByRole('combobox', { name: 'Width' }), { target: { value: 'pixels' } });
  fireEvent.change(within(bar()).getByRole('spinbutton', { name: 'Width in pixels' }), { target: { value: '480' } });
  fireEvent.change(within(bar()).getByRole('combobox', { name: 'Alignment' }), { target: { value: 'center' } });
  expect(handle.markdown()).toBe(`<figure data-width="480" data-align="center">\n\n![Alt](${src})\n\n</figure>\n`);
});

test('a width the help center cannot show is refused and changes nothing', async () => {
  const { handle, editor } = await make(`![Alt](${src})\n`);
  pickImage(editor);
  fireEvent.change(within(bar()).getByRole('combobox', { name: 'Width' }), { target: { value: 'pixels' } });
  const pixels = within(bar()).getByRole('spinbutton', { name: 'Width in pixels' });
  for (const value of ['0', '5000']) {
    fireEvent.change(pixels, { target: { value } });
    expect(pixels).toHaveAttribute('aria-invalid', 'true');
    expect(handle.markdown()).toBe(`![Alt](${src})\n`);
  }
});

test('a named width, the border and its removal are saved on the figure', async () => {
  const { handle, editor } = await make(`<figure data-align="right">\n\n![Alt](${src})\n\n</figure>\n`);
  pickImage(editor);
  fireEvent.change(within(bar()).getByRole('combobox', { name: 'Width' }), { target: { value: 'small' } });
  const border = within(bar()).getByRole('button', { name: 'Border' });
  fireEvent.click(border);
  expect(border).toHaveAttribute('aria-pressed', 'true');
  expect(handle.markdown()).toBe(`<figure data-width="small" data-align="right" data-border>\n\n![Alt](${src})\n\n</figure>\n`);
  fireEvent.click(border);
  expect(handle.markdown()).toBe(`<figure data-width="small" data-align="right">\n\n![Alt](${src})\n\n</figure>\n`);
});

test('Caption puts the cursor under the image, and what is typed there is saved as its caption', async () => {
  const { handle, editor } = await make(`Intro\n\n![Alt](${src})\n`);
  pickImage(editor);
  fireEvent.click(within(bar()).getByRole('button', { name: 'Caption' }));
  expect(editor.state.selection).toBeInstanceOf(TextSelection);
  act(() => { editor.commands.insertContent('Settings page'); });
  expect(handle.markdown()).toBe(`Intro\n\n<figure>\n\n![Alt](${src})\n\n<figcaption>\n\nSettings page\n\n</figcaption>\n\n</figure>\n`);
});

test('Link makes the image lead to an address, and Remove link takes it away', async () => {
  const { handle, editor } = await make(`<figure data-width="medium">\n\n![Alt](${src})\n\n</figure>\n`);
  pickImage(editor);
  fireEvent.click(within(bar()).getByRole('button', { name: 'Link' }));
  fireEvent.change(screen.getByRole('textbox', { name: 'Address' }), { target: { value: 'https://example.com/settings' } });
  fireEvent.click(screen.getByRole('button', { name: 'Insert' }));
  expect(handle.markdown()).toBe(`<figure data-width="medium">\n\n[![Alt](${src})](https://example.com/settings)\n\n</figure>\n`);
  pickImage(editor);
  fireEvent.click(within(bar()).getByRole('button', { name: 'Link' }));
  fireEvent.click(screen.getByRole('button', { name: 'Remove link' }));
  expect(handle.markdown()).toBe(`<figure data-width="medium">\n\n![Alt](${src})\n\n</figure>\n`);
});

test('an image in a line of text keeps the text around it when it becomes a figure', async () => {
  const { handle, editor } = await make(`Before [![Alt](${src})](https://example.com) after\n`);
  pickImage(editor);
  fireEvent.change(within(bar()).getByRole('combobox', { name: 'Alignment' }), { target: { value: 'inline' } });
  expect(handle.markdown()).toBe(`Before\n\n<figure data-align="inline">\n\n[![Alt](${src})](https://example.com)\n\n</figure>\n\nafter\n`);
});

test('a figure copied from elsewhere keeps only the width and alignment the help center knows', async () => {
  const { handle, editor } = await make('');
  act(() => { editor.commands.setContent(`<figure data-figure="" data-width="small" data-align="middle"><div><img src="${src}" alt="Alt"></div><figcaption></figcaption></figure>`); });
  expect(handle.markdown()).toBe(`<figure data-width="small">\n\n![Alt](${src})\n\n</figure>\n`);
});
