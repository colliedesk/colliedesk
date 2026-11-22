import { useEffect, useState } from 'react';

export type SourceImage =
  | { state: 'loading' }
  | { state: 'failed' }
  | { state: 'ready'; image: HTMLImageElement; type: string };

// useSourceImage loads the image to draw on from its own bytes (an object URL, so the canvas is never tainted
// and can be saved), and names its type so the copy keeps JPEG for a photo.
export function useSourceImage(src: string): SourceImage {
  const [source, setSource] = useState<SourceImage>({ state: 'loading' });
  useEffect(() => {
    let alive = true;
    let url: string | undefined;
    setSource({ state: 'loading' });
    (async () => {
      const response = await fetch(src, { credentials: 'same-origin' });
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      const bytes = await response.blob();
      url = URL.createObjectURL(bytes);
      const image = new Image();
      image.src = url;
      await image.decode();
      if (alive) setSource({ state: 'ready', image, type: bytes.type });
    })().catch(() => { if (alive) setSource({ state: 'failed' }); });
    return () => {
      alive = false;
      if (url) URL.revokeObjectURL(url);
    };
  }, [src]);
  return source;
}
