import type { Attachment } from '../../api/schemas';

export type MediaKind = 'image' | 'video' | 'audio';

// One picture, video or voice clip of a conversation, as the gallery shows it.
export interface GalleryItem {
  file: Attachment;
  kind: MediaKind;
  sender: string;
  at: string;
}

// Raster pictures every browser draws. SVG is left out: it is a document, and the download route serves no
// file as one. HEIC and TIFF are left out too: most browsers cannot draw them, so they stay a download row.
const IMAGES = new Set(['image/png', 'image/jpeg', 'image/jpg', 'image/pjpeg', 'image/gif', 'image/webp', 'image/avif', 'image/bmp']);

// mediaKind reads the media type the server stored with the file, never the name: a chat app names a photo
// "image-1". A video or audio type a browser cannot play still gets a player, which falls back to the row.
export function mediaKind(contentType: string): MediaKind | null {
  const type = contentType.split(';')[0].trim().toLowerCase();
  if (IMAGES.has(type)) return 'image';
  if (type.startsWith('video/')) return 'video';
  if (type.startsWith('audio/')) return 'audio';
  return null;
}

// The one address of a file's bytes: the workspace-checked download route (a download, never a page).
export const attachmentURL = (id: string) => `/api/v1/attachments/${id}`;

// The server's small picture of an image (at most 320 px): what the timeline and the gallery's strip show. A
// file the server cannot read answers 404, and the original is shown instead.
export const thumbnailURL = (id: string) => `/api/v1/attachments/${id}/thumbnail`;
