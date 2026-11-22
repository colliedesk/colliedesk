const ownImage = /^\/api\/v1\/kb\/images\/([0-9a-f-]{36})$/;

// libraryImageID is the media library item an image's address names, or undefined for an image from another
// site (which has no item to keep the original in, so it cannot be annotated).
export function libraryImageID(src: string | null | undefined) {
  return ownImage.exec(src ?? '')?.[1];
}

// annotatedName names the copy after the original: "Settings page.png" becomes "Settings page (annotated).png",
// with the extension of the type the copy is saved as.
export function annotatedName(original: string, type: string) {
  const extension = type === 'image/jpeg' ? '.jpg' : '.png';
  const stem = original.replace(/\.[a-z0-9]{1,5}$/i, '').trim() || 'Image';
  return `${stem} (annotated)${extension}`;
}
