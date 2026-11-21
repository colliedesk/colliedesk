import { useEffect, useMemo, useRef } from 'react';
import { InvalidRequestError } from '../../api/client';
import { annotatedName, libraryImageID } from './annotate/library-image';
import type { ImageAnnotation } from './editor/editor-types';
import type { KBArticle } from './kb-api';
import { kbMessages } from './kb-messages';
import { mediaApi } from './media-api';
import { fileProblem } from './media-uploads';

// The article as the editor holds it when an image is annotated.
export interface AnnotationState {
  // article is the saved article, undefined while a new one has not been saved yet.
  article: KBArticle | undefined;
  // blocked is true while the article is busy or changed elsewhere; unsaved while the text on screen is not
  // what is saved (edits, or a save on its way).
  blocked: boolean;
  unsaved: () => boolean;
  // reload reads the article back from the server and shows it, as a restored version is shown.
  reload: () => Promise<void>;
  notice: (text: string) => void;
}

// annotationRefusal says why an image cannot be annotated now, or undefined when it can. The copy takes the
// original's place by rewriting the saved text, so the text on screen must be what is saved.
export function annotationRefusal(state: AnnotationState) {
  if (!state.article) return kbMessages.annotateNeedsArticle;
  if (state.blocked || state.unsaved()) return kbMessages.annotateNeedsSave;
  return undefined;
}

// useImageAnnotation saves an annotated image as a new library item that replaces the original in this one
// article: the replacement keeps the original as the copy's earlier version, and the library can undo it.
export function useImageAnnotation(state: AnnotationState): ImageAnnotation {
  const now = useRef(state);
  useEffect(() => { now.current = state; });
  return useMemo<ImageAnnotation>(() => ({
    check: () => annotationRefusal(now.current),
    save: async (src, image) => {
      const refusal = annotationRefusal(now.current);
      const original = libraryImageID(src);
      const article = now.current.article;
      if (refusal) throw new InvalidRequestError(refusal);
      if (!original || !article) throw new InvalidRequestError(kbMessages.annotateNotLibrary);
      const { name } = await mediaApi.details(original);
      const copyName = annotatedName(name, image.type);
      if (fileProblem(new File([image], copyName, { type: image.type })) === 'size') throw new InvalidRequestError(kbMessages.imageTooLarge(copyName));
      const copy = await mediaApi.uploadImage(image, copyName, false);
      await mediaApi.replace(original, copy.id, article.id);
      await now.current.reload();
      now.current.notice(kbMessages.annotated);
    },
  }), []);
}
