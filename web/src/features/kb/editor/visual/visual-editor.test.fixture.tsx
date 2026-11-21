import { render, screen, waitFor } from '@testing-library/react';
import type { Editor } from '@tiptap/core';
import { expect, vi } from 'vitest';
import type { EditorHandle, UploadHooks } from '../editor-types';
import VisualEditor from './visual-editor';

// jsdom lays nothing out; ProseMirror measures text ranges to scroll the cursor into view.
if (!('getClientRects' in Range.prototype)) Object.assign(Range.prototype, { getClientRects: () => [], getBoundingClientRect: () => new DOMRect() });
if (!('elementFromPoint' in document)) Object.assign(document, { elementFromPoint: () => null });

export const never: UploadHooks = { accept: (files) => files, upload: () => new Promise(() => {}), store: () => new Promise(() => {}), onStart: () => {}, onDone: () => {}, onError: () => {} };

// make shows the visual editor with initial as its text, and gives what a test drives it with.
export async function make(initial: string, uploads: UploadHooks = never) {
  let handle: EditorHandle | undefined;
  const onChange = vi.fn();
  render(<VisualEditor initial={initial} uploads={uploads} label="Text" describedBy="help" invalid={false} mode="edit" onChange={onChange}
    onReady={(ready) => { handle = ready; }} />);
  await waitFor(() => expect(handle).toBeDefined());
  const text = screen.getByRole('textbox', { name: 'Text' });
  // Tiptap puts the editor on its element, as the page's own scripts never need it.
  const editor = (text as HTMLElement & { editor: Editor }).editor;
  return { handle: handle!, onChange, text, editor };
}
