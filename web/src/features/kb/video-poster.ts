import { mediaApi } from './media-api';
import type { KBMedia } from './media-api';

// A video's poster is the still the help center and the library show before it plays. The person picks
// one in the media panel; until then, an upload made from this browser draws one from the file itself.

// posterFrom draws the frame the player shows into a JPEG of at most 1280 pixels on a side.
export async function posterFrom(player: HTMLVideoElement): Promise<Blob> {
  const scale = Math.min(1, 1280 / Math.max(player.videoWidth, player.videoHeight));
  const canvas = document.createElement('canvas');
  canvas.width = Math.max(1, Math.round(player.videoWidth * scale));
  canvas.height = Math.max(1, Math.round(player.videoHeight * scale));
  const context = canvas.getContext('2d');
  if (!context || player.videoWidth === 0) throw new Error('no frame');
  context.drawImage(player, 0, 0, canvas.width, canvas.height);
  return new Promise((resolve, reject) => canvas.toBlob((blob) => (blob ? resolve(blob) : reject(new Error('no frame'))), 'image/jpeg', 0.82));
}

export type PosterCapture = (file: File) => Promise<Blob | undefined>;

// framePoster opens the local file in a hidden player and draws a frame a little into it (the first one is
// often black); undefined when this browser cannot decode the file (a QuickTime HEVC, say) or it takes too long.
export const framePoster: PosterCapture = (file) => new Promise((resolve) => {
  if (typeof URL.createObjectURL !== 'function') { resolve(undefined); return; }
  const url = URL.createObjectURL(file);
  const player = document.createElement('video');
  let settled = false;
  const done = (blob: Blob | undefined) => {
    if (settled) return;
    settled = true;
    window.clearTimeout(timer);
    player.removeAttribute('src');
    player.load();
    URL.revokeObjectURL(url);
    resolve(blob);
  };
  const timer = window.setTimeout(() => done(undefined), 10_000);
  player.muted = true;
  player.playsInline = true;
  player.preload = 'auto';
  player.addEventListener('error', () => done(undefined), { once: true });
  player.addEventListener('loadedmetadata', () => {
    player.currentTime = Number.isFinite(player.duration) && player.duration > 0 ? Math.min(1, player.duration / 10) : 0;
  }, { once: true });
  player.addEventListener('seeked', () => { posterFrom(player).then(done, () => done(undefined)); }, { once: true });
  player.src = url;
});

// addPoster gives a stored video without a poster one drawn from its file. It never fails the upload: a
// browser that cannot draw a frame, or a refused poster, leaves the video as it is.
export async function addPoster(file: File, item: KBMedia, capture: PosterCapture = framePoster): Promise<KBMedia | undefined> {
  if (item.kind !== 'video' || item.poster_url) return undefined;
  try {
    const image = await capture(file);
    return image ? await mediaApi.setPoster(item.id, image) : undefined;
  } catch {
    return undefined;
  }
}
