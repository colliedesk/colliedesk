import { useEffect, useRef, useState } from 'react';
import { attachmentMessages } from './attachment-messages';
import { Icon } from './editor/icons';
import { fileAccept, isAttachableName, maxFileBytes } from './kb-file-types';
import { reason } from './kb-tree';
import { mediaApi } from './media-api';
import type { KBMedia, KBMediaReplacement } from './media-api';
import { mediaMessages } from './media-messages';
import { uploadVideo } from './media-uploads';

// FileReplace takes the new version of a file from disk. It goes up as a file no article lists, then
// replaces this one in place: the file keeps its id and address, and the earlier version waits in the trash
// for Undo.
export function FileReplace({ item, onReplaced, onProblem }: { item: KBMedia; onReplaced: (done: KBMediaReplacement) => void; onProblem: (text: string | undefined) => void }) {
  const input = useRef<HTMLInputElement>(null);
  const [percent, setPercent] = useState<number>();
  const stop = useRef(new AbortController());
  useEffect(() => {
    const controller = new AbortController();
    stop.current = controller;
    return () => controller.abort();
  }, []);
  const send = async (file: File) => {
    onProblem(undefined);
    if (!isAttachableName(file.name)) { onProblem(attachmentMessages.type(file.name)); return; }
    if (file.size > maxFileBytes) { onProblem(attachmentMessages.tooLarge(file.name, file.size)); return; }
    setPercent(0);
    try {
      const uploaded = await uploadVideo(file, { kind: 'file', keep: false, signal: stop.current.signal,
        onProgress: (sent) => setPercent(Math.min(100, Math.round((sent / Math.max(1, file.size)) * 100))) });
      if (uploaded) onReplaced(await mediaApi.replace(item.id, uploaded.id));
    } catch (error) {
      if (!stop.current.signal.aborted) onProblem(reason(error, mediaMessages.actionFailed));
    } finally {
      setPercent(undefined);
    }
  };
  return (
    <>
      <button type="button" disabled={percent !== undefined} data-tip={mediaMessages.replaceFileTip} onClick={() => input.current?.click()}><Icon name="replace" />{mediaMessages.replace}</button>
      <input ref={input} type="file" hidden accept={fileAccept} aria-label={mediaMessages.replaceFileLabel}
        onChange={(event) => { const chosen = event.target.files?.[0]; event.target.value = ''; if (chosen) void send(chosen); }} />
      {percent !== undefined && <span role="status" className="kb-hint">{mediaMessages.uploadingVersion(percent)}</span>}
    </>
  );
}
