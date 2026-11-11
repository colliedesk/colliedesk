import { useState } from 'react';
import type { Attachment } from '../../api/schemas';
import { formatBytes } from '../../lib/format-bytes';
import { messages } from '../../messages';
import { DownloadIcon, PaperclipIcon } from '../../ui/icons';
import { attachmentURL, mediaKind, thumbnailURL } from './media-kind';
import type { MediaKind } from './media-kind';
import { mediaViewerPart } from './media-part';

// AttachmentList shows each file the way it is read: a picture as a thumbnail, a video or a voice clip in a
// player, by the media type the server stored (never the name). Every other file, and media a browser cannot
// draw or play, is a row to download. Each keeps a link to the original file.
// sourceOf reads a file that is not an attachment from elsewhere (a file on an intake card); a file without a
// thumbnail shows its original.
export interface AttachmentListProps {
  items: Attachment[];
  onOpenMedia?: (attachmentId: string) => void;
  sourceOf?: (file: Attachment) => { src: string; thumb?: string };
  label?: string;
}

export function AttachmentList({ items, onOpenMedia, sourceOf, label = messages.attachments }: AttachmentListProps) {
  return (
    <ul className="message-attachments" aria-label={label}>
      {items.map((file) => <AttachmentRow key={file.id} file={file} onOpenMedia={onOpenMedia} source={sourceOf?.(file)} />)}
    </ul>
  );
}

function AttachmentRow({ file, onOpenMedia, source }: { file: Attachment; onOpenMedia?: (attachmentId: string) => void; source?: { src: string; thumb?: string } }) {
  const kind = mediaKind(file.content_type);
  const [broken, setBroken] = useState(false);
  // A picture shows its thumbnail; without one (a type the server cannot read) the original; then the row.
  const [original, setOriginal] = useState(Boolean(source && !source.thumb));
  const url = source?.src ?? attachmentURL(file.id);
  const thumb = source ? source.thumb ?? url : thumbnailURL(file.id);
  if (!kind || broken) {
    return (
      <li>
        <PaperclipIcon /><a href={url} download={file.filename} title={messages.downloadAttachment(file.filename)}>{file.filename}</a>
        <span className="attachment-size">{formatBytes(file.size)}</span>
        {broken && kind !== 'image' && <span className="attachment-note">{messages.mediaNotPlayable}</span>}
      </li>
    );
  }
  const open = onOpenMedia ? () => onOpenMedia(file.id) : undefined;
  const picture = <img src={original ? url : thumb} alt="" loading="lazy" decoding="async" onError={() => (original ? setBroken(true) : setOriginal(true))} />;
  return (
    <li className={`media-tile media-tile-${kind}`}>
      {kind === 'image'
        ? open ? <button type="button" className="media-thumb" aria-label={messages.openMedia(file.filename)} data-media-open={file.id} onClick={open}>{picture}</button>
          : <span className="media-thumb">{picture}</span>
        : <InlinePlayer kind={kind} src={url} title={file.filename} openerId={file.id} onExpand={open} onError={() => setBroken(true)} />}
      <span className="media-caption">
        <a href={url} download={file.filename} title={messages.downloadAttachment(file.filename)}><DownloadIcon />{file.filename}</a>
        <span className="attachment-size">{formatBytes(file.size)}</span>
      </span>
    </li>
  );
}

// The player is in its own chunk; until it is there an empty frame of the player's size holds its place, so the
// timeline does not jump. A chunk that cannot load leaves the download row.
function InlinePlayer({ kind, src, title, openerId, onExpand, onError }: { kind: Exclude<MediaKind, 'image'>; src: string; title: string; openerId: string; onExpand?: () => void; onError: () => void }) {
  const part = mediaViewerPart.use();
  if (part.status === 'error') return null;
  if (part.status === 'loading') return <div className={`media-slot media-slot-${kind}`} aria-busy="true" />;
  const { MediaPlayer } = part.module;
  return <MediaPlayer kind={kind} src={src} title={title} lazy openerId={openerId} onExpand={onExpand} onError={onError} />;
}
