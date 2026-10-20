import { useEffect, useRef, useState } from 'react';
import type { ClipboardEvent } from 'react';
import type { CodeMirrorEditorProps } from './codemirror-editor';

// FakeEditor stands in for the editor in the article page's tests (jsdom types into a field better than into
// CodeMirror): a field with the editor's contract. It tells the page of typing 120 ms after it, as the editor
// does, pastes images through the upload hooks, each as a paragraph at the end, and shows the preview it is
// given. codemirror-editor.test.tsx tests the real editor.
export default function FakeEditor({ initial, uploads, label, describedBy, invalid, mode, preview, onChange, onReady }: CodeMirrorEditorProps) {
  const [value, setValue] = useState(initial);
  const text = useRef(value);
  const timer = useRef<number | undefined>(undefined);
  const emitted = useRef(value);
  const pending = useRef(0);
  const props = useRef({ onChange, onReady, uploads });
  useEffect(() => { props.current = { onChange, onReady, uploads }; });
  const put = (next: string) => { text.current = next; setValue(next); };
  const emit = () => {
    window.clearTimeout(timer.current);
    if (text.current !== emitted.current) { emitted.current = text.current; props.current.onChange(text.current); }
    return text.current;
  };
  useEffect(() => {
    props.current.onReady({
      markdown: emit,
      load: (markdown) => { put(markdown); emitted.current = markdown; },
      uploading: () => pending.current > 0,
      focus: () => {},
    });
    return () => window.clearTimeout(timer.current);
    // Made once, as the real editor is.
  }, []);
  const onPaste = (event: ClipboardEvent<HTMLTextAreaElement>) => {
    const files = [...event.clipboardData.files];
    if (files.length === 0) return;
    event.preventDefault();
    for (const file of props.current.uploads.accept(files)) {
      pending.current += 1;
      props.current.uploads.onStart(file);
      props.current.uploads.upload(file).then(({ url, alt }) => {
        pending.current -= 1;
        put(`${text.current}${text.current ? '\n\n' : ''}![${alt}](${url})`);
        timer.current = window.setTimeout(emit, 120);
        props.current.uploads.onDone(file);
      }, (error: unknown) => { pending.current -= 1; props.current.uploads.onError(file, error); });
    }
  };
  return <>{mode !== 'preview' && <textarea aria-label={label} aria-describedby={describedBy} aria-invalid={invalid || undefined} data-editor="formatted" value={value} onPaste={onPaste}
    onChange={(event) => { put(event.target.value); window.clearTimeout(timer.current); timer.current = window.setTimeout(emit, 120); }} />}
    {mode !== 'edit' && <section aria-label="Preview">{preview}</section>}</>;
}
