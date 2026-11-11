import { useState } from 'react';
import type { IntakeGathering } from '../../api/intake';
import type { Attachment } from '../../api/schemas';
import { AttachmentList } from '../inbox/attachment-list';
import { intakeFileURL, mediaKind } from '../inbox/media-kind';
import type { GalleryItem } from '../inbox/media-kind';
import { mediaViewerPart } from '../inbox/media-part';
import { Part } from '../../ui/lazy-part';
import { reviewMessages as m } from './review-messages';

// One file a customer sent on a card: index is its place among the files of the item that carried it.
export type IntakeCardFile = IntakeGathering['files'][number];

const noHash = '0'.repeat(64);

// IntakeFiles shows what a customer sent on a card before it is decided, the way a conversation shows its
// attachments: a picture as a picture, a video or a voice clip in its player, any other file as a row to
// download, each opening the gallery. A file the channel could not fetch says why. The bytes come from the
// card's own download route (docs/user/review.md).
export function IntakeFiles({ gatheringId, files, sender, at, timezone }: { gatheringId: string; files: IntakeCardFile[]; sender: string; at: string; timezone: string }) {
  const [opened, setOpened] = useState<string | null>(null);
  const stored = files.filter((file) => file.status === 'stored');
  const missing = files.filter((file) => file.status !== 'stored');
  const idOf = (file: IntakeCardFile) => `${file.item_id}-${file.index}`;
  const sources = new Map(stored.map((file) => [idOf(file), intakeFileURL(gatheringId, file.item_id, file.index)]));
  const items: Attachment[] = stored.map((file) => ({ id: idOf(file), message_id: file.item_id, filename: file.name, content_type: file.content_type,
    size: file.size ?? 0, sha256: noHash, inline: false, created_at: at }));
  const media: GalleryItem[] = items.flatMap((file) => {
    const kind = mediaKind(file.content_type);
    const src = sources.get(file.id);
    return kind && src ? [{ file, kind, sender, at, src }] : [];
  });
  const start = media.findIndex((entry) => entry.file.id === opened);
  return (
    <div className="intake-files">
      {items.length > 0 && <AttachmentList items={items} label={m.files} sourceOf={(file) => ({ src: sources.get(file.id) ?? '' })} onOpenMedia={setOpened} />}
      {missing.length > 0 && <ul className="intake-files-missing">
        {missing.map((file) => <li key={idOf(file)}>{file.status === 'failed' && file.error ? m.fileNotReceived(file.name, file.error) : m.fileNotReceivedYet(file.name)}</li>)}
      </ul>}
      {opened && start !== -1 && <Part part={mediaViewerPart} render={(viewer) => <viewer.MediaGallery items={media} start={start} timezone={timezone}
        onClose={() => setOpened(null)} opener={`[data-media-open="${opened}"]`} />} />}
    </div>
  );
}
