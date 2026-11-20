import { useEffect, useId, useRef, useState } from 'react';
import type { DragEvent, MutableRefObject } from 'react';
import { formatBytes } from '../../lib/format-bytes';
import { HelpTip } from '../../ui/help-tip';
import { attachmentMessages as m } from './attachment-messages';
import { Icon } from './editor/icons';
import type { KBAttachment } from './kb-api';
import { fileAccept, fileFormat, isAttachableName, maxArticleFileBytes, maxFileBytes } from './kb-file-types';
import { reason } from './kb-tree';
import { FileGlyph } from './media-grid';
import { uploadVideo } from './media-uploads';
import { OrderAnnouncement, OrderButtons } from './order-buttons';

export interface ArticleAttachmentsProps {
  files: KBAttachment[];
  // onSet saves the whole list, in order, and answers it as saved; without it (nothing saved yet) the strip
  // says to save first.
  onSet?: (ids: string[]) => Promise<KBAttachment[]>;
  saveFirst?: string;
  // note shows while the list is empty.
  note?: string;
  // incoming takes the files pasted or dropped into the text, which belong in this list.
  incoming?: MutableRefObject<((files: File[]) => void) | undefined>;
}

// ArticleAttachments lists the files readers download under the text. A file goes into the media library
// first (kept there, so removing it from the list never deletes it), then onto the list.
export function ArticleAttachments({ files, onSet, saveFirst = m.saveFirst, note, incoming }: ArticleAttachmentsProps) {
  const id = useId();
  const [problems, setProblems] = useState<string[]>([]);
  const [sending, setSending] = useState<{ name: string; percent: number }>();
  const [busy, setBusy] = useState(false);
  const [announcement, setAnnouncement] = useState('');
  const [dragging, setDragging] = useState(false);
  // The list and save as last answered: a second file of one choice goes on the list the first one made.
  const current = useRef(files);
  const save = useRef(onSet);
  useEffect(() => { current.current = files; }, [files]);
  useEffect(() => { save.current = onSet; }, [onSet]);
  const queue = useRef(Promise.resolve());
  const stop = useRef(new AbortController());
  useEffect(() => {
    const controller = new AbortController();
    stop.current = controller;
    return () => controller.abort();
  }, []);

  const change = async (ids: string[]) => {
    if (!save.current) return undefined;
    current.current = await save.current(ids);
    return current.current.map((file) => file.id);
  };
  const send = async (file: File) => {
    setSending({ name: file.name, percent: 0 });
    try {
      const item = await uploadVideo(file, { kind: 'file', keep: true, signal: stop.current.signal,
        onProgress: (sent) => setSending({ name: file.name, percent: Math.min(100, Math.round((sent / Math.max(1, file.size)) * 100)) }) });
      if (item) await change([...current.current.map((each) => each.id), item.id]);
    } catch (error) {
      if (!stop.current.signal.aborted) setProblems((list) => [...list, reason(error, m.failed)]);
    } finally {
      setSending(undefined);
    }
  };
  const add = (chosen: File[]) => {
    if (!save.current) {
      setProblems([saveFirst]);
      return;
    }
    const found: string[] = [];
    let total = current.current.reduce((sum, file) => sum + file.size, 0);
    const accepted = chosen.filter((file) => {
      if (!isAttachableName(file.name)) found.push(m.type(file.name));
      else if (file.size > maxFileBytes) found.push(m.tooLarge(file.name, file.size));
      else if (total + file.size > maxArticleFileBytes) found.push(m.tooMuch(file.name, total + file.size));
      else {
        total += file.size;
        return true;
      }
      return false;
    });
    setProblems(found);
    // One file at a time, in the order they came, each added to the list the one before it saved.
    queue.current = queue.current.then(async () => { for (const file of accepted) await send(file); });
  };
  useEffect(() => {
    if (!incoming) return undefined;
    incoming.current = add;
    return () => { incoming.current = undefined; };
  });
  const remove = async (file: KBAttachment) => {
    setBusy(true);
    setProblems([]);
    try {
      await change(current.current.filter((each) => each.id !== file.id).map((each) => each.id));
      setAnnouncement(m.removed(file.name));
    } catch (error) {
      setProblems([reason(error, m.failed)]);
    } finally {
      setBusy(false);
    }
  };
  const drop = (event: DragEvent) => {
    event.preventDefault();
    setDragging(false);
    add([...event.dataTransfer.files]);
  };
  const ids = files.map((file) => file.id);
  const held = busy || sending !== undefined;

  return (
    <section className="kb-attachments" aria-labelledby={`${id}-heading`} data-dragging={dragging || undefined}
      onDragOver={(event) => {
        if (!onSet || !event.dataTransfer.types.includes('Files')) return;
        event.preventDefault();
        setDragging(true);
      }}
      onDragLeave={() => setDragging(false)} onDrop={drop}
      onPaste={(event) => {
        const pasted = [...event.clipboardData.files];
        if (!pasted.length) return;
        event.preventDefault();
        add(pasted);
      }}>
      <div className="kb-attachments-head">
        <h2 id={`${id}-heading`}>{m.heading}</h2>
        {onSet && <label className="button-link kb-attachments-choose"><Icon name="upload" />{m.choose}
          <input type="file" multiple accept={fileAccept} className="sr-only" onChange={(event) => {
            add([...(event.target.files ?? [])]);
            event.target.value = '';
          }} /></label>}
      </div>
      <p className="kb-hint">{onSet ? (dragging ? m.dropHere : <>{m.hint} <HelpTip label={m.heading} text={m.hintTip} /></>) : saveFirst}</p>
      {note && files.length === 0 && <p className="muted-copy">{note}</p>}
      {files.length > 0 && <ul className="kb-attachment-list">
        {files.map((file) => (
          <li key={file.id}>
            <FileGlyph name={file.name} />
            <span className="kb-attachment-text"><a href={file.url}>{file.name}</a><small>{`${fileFormat(file.name)} · ${formatBytes(file.size)}`}</small></span>
            {onSet && <OrderButtons ids={ids} id={file.id} name={file.name} busy={held} onMove={async (next) => change(next)} onAnnounce={setAnnouncement} />}
            {onSet && <button type="button" className="icon-button" aria-label={m.remove(file.name)} data-tip={m.removeTip} aria-disabled={held || undefined}
              onClick={() => { if (!held) void remove(file); }}><Icon name="close" /></button>}
          </li>
        ))}
      </ul>}
      {sending && <p role="status" className="kb-hint">{m.uploading(sending.name, sending.percent)}</p>}
      {problems.map((text) => <p key={text} role="alert" className="error-copy">{text}</p>)}
      <OrderAnnouncement message={announcement} />
    </section>
  );
}
