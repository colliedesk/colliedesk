import type { ReactNode } from 'react';
import type { CalloutType } from '../callouts';
import type { Align, Palette } from './format-tags';

// What the article page and the toolbar ask of an editor, whichever one shows the text: the Markdown source
// (CodeMirror) or the visual editor (ProseMirror). Nothing here loads either.

// EditorHandle is what the article page asks of the editor.
export interface EditorHandle {
  // markdown is the text as it would be saved, now (onChange runs a moment after typing stops).
  markdown(): string;
  // load replaces the text, with a fresh undo history.
  load(markdown: string): void;
  uploading(): boolean;
  focus(): void;
  // replace changes the first place the text holds find into by (an image's alt text, set from the page); false
  // when the text no longer holds it.
  replace(find: string, by: string): boolean;
  // insert puts a block of Markdown (a template's text) at the cursor, on lines of its own.
  insert(markdown: string): void;
}

export type ViewMode = 'edit' | 'split' | 'preview';

// Named is what the page hears of an upload: a file, or an image a paste holds by its address.
export type Named = { name: string };

// UploadHooks connect the editor to the article page: which files it accepts (refusing the others with a
// reason), how one is sent and how it is described, and what the page shows meanwhile.
export interface UploadHooks {
  accept(files: File[]): File[];
  // upload sends a file and gives what the article holds for it (an image's Markdown, a video's HTML);
  // progress (0 to 1) shows in its place while a large file goes.
  // signal aborts when the person cancels the upload from its place in the text.
  upload(file: File, progress: (done: number) => void, signal: AbortSignal): Promise<{ snippet: string }>;
  // store puts an image a paste holds by its address (web, blob: or data:) in the media library, and gives
  // the address the article uses for it.
  store(src: string, signal: AbortSignal): Promise<string>;
  onStart(item: Named): void;
  onDone(item: Named): void;
  onError(item: Named, error: unknown): void;
}

// ImageAnnotation lets the visual editor offer Annotate on an image of the media library. check says why the
// article cannot take an annotated copy now (unsaved changes, not saved yet); save stores the drawn image as a
// new library item that takes the original's place in the article, the original staying as its earlier version.
export interface ImageAnnotation {
  check(): string | undefined;
  save(src: string, image: Blob): Promise<void>;
}

export interface EditorProps {
  initial: string;
  uploads: UploadHooks;
  label: string;
  describedBy: string;
  invalid: boolean;
  mode: ViewMode;
  // The article being edited, left out of the list a link to an article is picked from.
  articleId?: string;
  // preview is shown beside the text (split) or instead of it (preview).
  preview?: ReactNode;
  // annotate is left out where the text is not an article's (a template, a translation).
  annotate?: ImageAnnotation;
  onChange: (markdown: string) => void;
  onReady: (handle: EditorHandle) => void;
}

export type Block = 'paragraph' | 'h1' | 'h2' | 'h3' | 'h4';
export type LineKind = 'bullet' | 'ordered' | 'task' | 'quote';
export type MarkKind = 'bold' | 'italic' | 'strike' | 'code' | 'underline' | 'highlight';
// FormatAt is what alignment and indent act on where the cursor is: a top-level paragraph or heading (both), the
// selected table cells (alignment only) or nothing (in a list, a quote or a callout, where the server keeps neither).
export type FormatAt = 'block' | 'cell' | 'none';
// TableAction is what the table tools do to the table the cursor is in; bg-… sets the selected cells' background.
export type TableAction = 'headerRow' | 'rowBefore' | 'rowAfter' | 'columnBefore' | 'columnAfter' | 'deleteRow' | 'deleteColumn'
  | 'merge' | 'split' | 'deleteTable' | `bg-${Palette}` | 'bg-none';

// Active is what the toolbar shows pressed where the cursor is.
export interface Active {
  block: Block;
  strong: boolean;
  emphasis: boolean;
  code: boolean;
  link: boolean;
  bullet: boolean;
  ordered: boolean;
  task: boolean;
  quote: boolean;
  underline: boolean;
  highlight: boolean;
  // color is the palette colour of the selected words, '' for none.
  color: string;
  align: 'left' | Align;
  indent: number;
  formatAt: FormatAt;
  // table: the cursor is in a table the table tools can change; headerRow: its first row is a header row.
  table: boolean;
  headerRow: boolean;
  canMerge: boolean;
  canSplit: boolean;
  canUndo: boolean;
  canRedo: boolean;
}

export const sameActive = (a: Active | undefined, b: Active) =>
  !!a && (Object.keys(b) as (keyof Active)[]).every((key) => a[key] === b[key]);

// ToolTarget is what the toolbar, its menus and its dialogs do to the text, in the editor that shows it.
export interface ToolTarget {
  mark(kind: MarkKind): void;
  link(): void;
  block(block: Block): void;
  lines(kind: LineKind): void;
  // insert puts a block of Markdown after the block the cursor is in; select is the range of it to select.
  insert(markdown: string, select?: [number, number]): void;
  callout(type: CalloutType): void;
  codeBlock(language: string): void;
  table(): void;
  // color sets the selected words' palette colour; null takes it off.
  color(color: Palette | null): void;
  // align aligns the block or the selected cells; null is the default, left.
  align(align: Align | null): void;
  indent(delta: 1 | -1): void;
  // tableEdit changes the table the cursor is in; only the visual editor has it, as Markdown holds no merged cell.
  tableEdit?: (action: TableAction) => void;
  rule(): void;
  articleLink(article: { title: string; url: string }): void;
  upload(files: File[]): void;
  // search opens find and replace; an editor without one leaves it to the browser's own.
  search?: () => void;
  undo(): void;
  redo(): void;
  focus(): void;
}
