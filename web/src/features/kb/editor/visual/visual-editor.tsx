import { useEffect, useRef, useState } from 'react';
import { Editor, Extension } from '@tiptap/core';
import type { Node as PMNode } from '@tiptap/pm/model';
import { EditorState, NodeSelection } from '@tiptap/pm/state';
import { UndoRedo } from '@tiptap/extensions';
import { EditorFrame } from '../editor-frame';
import type { Active, EditorProps, ToolTarget, UploadHooks } from '../editor-types';
import { sameActive } from '../editor-types';
import { pickedImage, samePicked } from './figure-commands';
import type { Picked } from './figure-commands';
import { ImageBar } from './image-bar';
import { AnnotateButton } from './annotate-button';
import { libraryImageID } from '../../annotate/library-image';
import { LinkDialog } from './link-dialog';
import { articleContent } from './schema';
import { docToMarkdown } from './to-markdown';
import { insertBlocks, read } from './visual-insert';
import { pasteEvent } from './visual-paste';
import { activeOf, visualTarget } from './visual-target';
import { startUpload, uploadPlaces, uploadsPending } from './visual-upload';
import '../formatting.css';
import './visual-editor.css';

// VisualEditor shows the article as it reads (Tiptap on ProseMirror), holding only what an article can store.
// What it saves is the Markdown the document reads to, except that an article nobody changed is saved exactly as
// it was written: opening and saving an article never rewrites its text.
export default function VisualEditor({ initial, uploads, label, describedBy, invalid, mode, articleId, preview, annotate, onChange, onReady }: EditorProps) {
  const host = useRef<HTMLDivElement>(null);
  const editorRef = useRef<Editor | undefined>(undefined);
  const [target, setTarget] = useState<ToolTarget>();
  const [active, setActive] = useState<Active>();
  // linking is the address of the link the cursor is in while the link dialog is open ('' for a new one).
  const [linking, setLinking] = useState<string>();
  // picked is the image the image bar works on.
  const [picked, setPicked] = useState<Picked>();
  // The editor is made once; what its callbacks call reads the latest props through this.
  const latest = useRef({ onChange, onReady, initial, label, describedBy });
  useEffect(() => { latest.current = { onChange, onReady, initial, label, describedBy }; });
  const hooks = useRef<UploadHooks>(uploads);
  useEffect(() => { hooks.current = uploads; }, [uploads]);
  useEffect(() => {
    const dom = editorRef.current?.view.dom;
    if (invalid) dom?.setAttribute('aria-invalid', 'true');
    else dom?.removeAttribute('aria-invalid');
  }, [invalid, active]);

  useEffect(() => {
    let alive = true;
    let timer: number | undefined;
    // source is the text as it was loaded and baseline the document it reads as: while the document is the
    // baseline, the text is the source, byte for byte.
    let source = latest.current.initial;
    let baseline: PMNode;
    // emitted is the text the page last heard of, so a load or an undo back to it is not sent as a change.
    let emitted = source;
    const openLink = () => setLinking((editorRef.current?.getAttributes('link').href as string | undefined) ?? '');
    const behaviour = Extension.create({
      name: 'kbEditing',
      addProseMirrorPlugins: () => [uploadPlaces],
      addKeyboardShortcuts: () => ({ 'Mod-k': () => (openLink(), true) }),
    });
    const editor = new Editor({
      element: host.current!,
      extensions: [...articleContent, UndoRedo, behaviour],
      // The text goes in with load, below, as a document of the editor's own schema.
      injectCSS: false,
      editorProps: {
        attributes: {
          role: 'textbox', 'aria-label': latest.current.label, 'aria-describedby': latest.current.describedBy, 'aria-multiline': 'true',
          spellcheck: 'true', class: 'markdown-body kb-preview kb-visual',
        },
        handlePaste: (view, event) => pasteEvent(view, event, hooks.current),
        // A click on a figure's image picks the figure; one in its caption puts the cursor there.
        handleClickOn: (view, _pos, node, nodePos, event) => {
          if (node.type.name !== 'articleFigure' || (event.target as Element).closest('figcaption')) return false;
          view.dispatch(view.state.tr.setSelection(NodeSelection.create(view.state.doc, nodePos)));
          return true;
        },
        handleDrop: (view, event, _slice, moved) => {
          const files = [...(event.dataTransfer?.files ?? [])];
          if (moved || files.length === 0) return false;
          const at = view.posAtCoords({ left: event.clientX, top: event.clientY })?.pos ?? view.state.selection.head;
          startUpload(view, files, at, hooks.current);
          return true;
        },
      },
    });
    editorRef.current = editor;
    const markdown = () => (editor.state.doc.eq(baseline) ? source : docToMarkdown(editor.state.doc));
    const emit = () => {
      window.clearTimeout(timer);
      const text = markdown();
      if (text !== emitted) { emitted = text; latest.current.onChange(text); }
      return text;
    };
    const show = () => {
      const next = activeOf(editor);
      setActive((current) => (sameActive(current, next) ? current : next));
      const image = pickedImage(editor.state);
      setPicked((current) => (samePicked(current, image) ? current : image));
    };
    // load shows other text, with a fresh undo history and no upload places (an upload still on its way ends
    // without going in).
    const load = (text: string) => {
      window.clearTimeout(timer);
      editor.view.updateState(EditorState.create({ doc: read(editor.schema, text), plugins: editor.state.plugins }));
      source = text;
      baseline = editor.state.doc;
      emitted = text;
      show();
    };
    load(source);
    editor.on('transaction', ({ transaction }) => {
      if (!alive) return;
      show();
      if (!transaction.docChanged) return;
      window.clearTimeout(timer);
      timer = window.setTimeout(() => { if (alive) emit(); }, 120);
    });
    setTarget(visualTarget(editor, hooks, openLink));
    latest.current.onReady({
      markdown: emit,
      load,
      uploading: () => uploadsPending(editor.view),
      focus: () => editor.commands.focus(),
      replace: (find, by) => {
        const text = markdown();
        if (!text.includes(find)) return false;
        const unchanged = editor.state.doc.eq(baseline);
        const next = text.replace(find, by);
        const doc = read(editor.schema, next);
        editor.view.dispatch(editor.state.tr.replaceWith(0, editor.state.doc.content.size, doc.content));
        // An article nobody changed keeps the rest of its text as written.
        if (unchanged) { source = next; baseline = editor.state.doc; }
        emit();
        return true;
      },
      insert: (text) => {
        insertBlocks(editor.view, text.replace(/\n+$/, ''));
        editor.commands.focus();
        emit();
      },
    });
    return () => {
      alive = false;
      window.clearTimeout(timer);
      editorRef.current = undefined;
      editor.destroy();
    };
  }, []);

  const editor = editorRef.current;
  const closeLink = () => { setLinking(undefined); editor?.commands.focus(); };
  return (
    <EditorFrame mode={mode} articleId={articleId} preview={preview} target={target} active={active}
      overlay={picked && editor && <ImageBar key={picked.pos} editor={editor} picked={picked}
      extra={annotate && libraryImageID(picked.attrs.src) ? <AnnotateButton src={picked.attrs.src} annotate={annotate} /> : undefined} />}>
      <div ref={host} className="kb-visual-host" hidden={mode === 'preview'} />
      {linking !== undefined && editor && <LinkDialog current={linking} onClose={closeLink}
        onRemove={() => { setLinking(undefined); editor.chain().focus().extendMarkRange('link').unsetMark('link').run(); }}
        onSet={(href) => {
          setLinking(undefined);
          const chain = editor.chain().focus();
          if (editor.isActive('link')) chain.extendMarkRange('link').setMark('link', { href }).run();
          else if (editor.state.selection.empty) chain.insertContent({ type: 'text', text: href, marks: [{ type: 'link', attrs: { href } }] }).unsetMark('link').run();
          else chain.setMark('link', { href }).run();
        }} />}
    </EditorFrame>
  );
}
