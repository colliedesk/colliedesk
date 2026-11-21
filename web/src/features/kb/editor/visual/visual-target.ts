import type { Editor } from '@tiptap/core';
import { TextSelection } from '@tiptap/pm/state';
import { articleAddress } from '../article-address';
import type { Active, Block, ToolTarget, UploadHooks } from '../editor-types';
import { insertBlocks } from './visual-insert';
import { startUpload } from './visual-upload';

const levels: Record<number, Block> = { 1: 'h1', 2: 'h2', 3: 'h3' };

// activeOf is what the toolbar shows pressed where the cursor is.
export function activeOf(editor: Editor): Active {
  const { $head } = editor.state.selection;
  const heading = $head.parent.type.name === 'heading' ? levels[$head.parent.attrs.level as number] : undefined;
  return {
    block: heading ?? 'paragraph',
    strong: editor.isActive('bold'),
    emphasis: editor.isActive('italic'),
    code: editor.isActive('code') || editor.isActive('codeBlock'),
    link: editor.isActive('link'),
    bullet: editor.isActive('bulletList'),
    ordered: editor.isActive('orderedList'),
    task: editor.isActive('taskList'),
    quote: editor.isActive('blockquote'),
    canUndo: editor.can().undo(),
    canRedo: editor.can().redo(),
  };
}

// topAfter is the place after the top-level block the cursor is in: where a callout goes, as the help center
// reads a callout only at the top of the article.
const topAfter = (editor: Editor) => {
  const { $head } = editor.state.selection;
  return $head.depth === 0 ? $head.pos : $head.after(1);
};

// putNode puts a block after the top-level block the cursor is in, the cursor inside it.
function putNode(editor: Editor, json: object) {
  const at = topAfter(editor);
  const node = editor.schema.nodeFromJSON(json);
  const tr = editor.state.tr.insert(at, node);
  tr.setSelection(TextSelection.near(tr.doc.resolve(at + 1))).scrollIntoView();
  editor.view.dispatch(tr);
}

// visualTarget is what the toolbar does in the visual editor: it changes the document, which is saved as the
// Markdown it reads to. openLink asks for a link's address.
export function visualTarget(editor: Editor, hooks: { current: UploadHooks }, openLink: () => void): ToolTarget {
  const chain = () => editor.chain().focus();
  return {
    mark: (kind) => {
      const run = chain();
      if (kind === 'bold') run.toggleBold().run();
      else if (kind === 'italic') run.toggleItalic().run();
      else if (kind === 'strike') run.toggleStrike().run();
      else run.toggleCode().run();
    },
    link: openLink,
    block: (block) => (block === 'paragraph' ? chain().setParagraph().run() : chain().setHeading({ level: Number(block.slice(1)) as 1 | 2 | 3 }).run()),
    lines: (kind) => {
      const run = chain();
      if (kind === 'bullet') run.toggleBulletList().run();
      else if (kind === 'ordered') run.toggleOrderedList().run();
      else if (kind === 'task') run.toggleTaskList().run();
      else run.toggleBlockquote().run();
    },
    insert: (markdown) => insertBlocks(editor.view, markdown),
    callout: (type) => putNode(editor, { type: 'callout', attrs: { type }, content: [{ type: 'paragraph' }] }),
    codeBlock: (language) => putNode(editor, { type: 'codeBlock', attrs: { language: language || null } }),
    table: () => chain().insertTable({ rows: 2, cols: 2, withHeaderRow: true }).run(),
    rule: () => chain().setHorizontalRule().run(),
    articleLink: (article) => {
      const href = articleAddress(article.url);
      if (editor.state.selection.empty) chain().insertContent({ type: 'text', text: article.title, marks: [{ type: 'link', attrs: { href } }] }).unsetMark('link').run();
      else chain().setMark('link', { href }).run();
    },
    upload: (files) => startUpload(editor.view, files, editor.state.selection.head, hooks.current),
    undo: () => chain().undo().run(),
    redo: () => chain().redo().run(),
    focus: () => editor.commands.focus(),
  };
}
