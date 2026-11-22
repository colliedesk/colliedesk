import { useState } from 'react';
import { AnnotateDialog } from '../../annotate/annotate-dialog';
import { reason } from '../../kb-tree';
import type { ImageAnnotation } from '../editor-types';
import { imageBarMessages as m } from './image-bar-messages';

// AnnotateButton opens the drawing tools on the picked image. When the article cannot take a copy yet (it has
// unsaved changes, say), it says why instead and opens nothing.
export function AnnotateButton({ src, annotate }: { src: string; annotate: ImageAnnotation }) {
  const [open, setOpen] = useState(false);
  const [refusal, setRefusal] = useState<string>();
  const start = () => {
    const why = annotate.check();
    setRefusal(why);
    if (!why) setOpen(true);
  };
  return <>
    <button type="button" onClick={start}>{m.annotate}</button>
    {refusal && <small role="alert" className="error-copy">{refusal}</small>}
    {open && <AnnotateDialog src={src} describe={reason} onClose={() => setOpen(false)}
      onSave={async (image) => { await annotate.save(src, image); setOpen(false); }} />}
  </>;
}
