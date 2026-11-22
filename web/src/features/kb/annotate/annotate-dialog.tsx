import { useEffect, useId, useRef, useState } from 'react';
import type { PointerEvent } from 'react';
import { KBDialog } from '../kb-ui';
import { annotateMessages as m } from './annotate-messages';
import { big, draw, tools } from './shapes';
import type { Point, Shape, Tool } from './shapes';
import { useSourceImage } from './use-source-image';
import './annotate.css';

// AnnotateDialog draws arrows, boxes, text and blurred areas on a copy of an image at its own size. Save copy
// hands over the drawn image (JPEG for a JPEG, PNG for the rest); the original is never changed here.
// onSave rejects with a reason to show, and the dialog stays open with what was drawn.
export function AnnotateDialog({ src, onSave, onClose, describe }: {
  src: string;
  onSave: (image: Blob) => Promise<void>;
  onClose: () => void;
  // describe turns an error from onSave into the words to show.
  describe: (error: unknown, fallback: string) => string;
}) {
  const id = useId();
  const canvas = useRef<HTMLCanvasElement>(null);
  const wordsField = useRef<HTMLInputElement>(null);
  const source = useSourceImage(src);
  const [tool, setTool] = useState<Tool>('arrow');
  const [shapes, setShapes] = useState<Shape[]>([]);
  const [drawing, setDrawing] = useState<Shape>();
  const [words, setWords] = useState('');
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string>();

  useEffect(() => {
    const box = canvas.current;
    if (!box || source.state !== 'ready') return;
    if (box.width !== source.image.naturalWidth) { box.width = source.image.naturalWidth; box.height = source.image.naturalHeight; }
    const context = box.getContext('2d');
    if (context) draw(context, source.image, drawing ? [...shapes, drawing] : shapes);
  }, [source, shapes, drawing]);

  // at maps the pointer to the image's own pixels: the canvas shows smaller than the image on a narrow screen.
  const at = (event: PointerEvent<HTMLCanvasElement>): Point => {
    const box = event.currentTarget;
    const rect = box.getBoundingClientRect();
    return { x: (event.clientX - rect.left) * (box.width / (rect.width || 1)), y: (event.clientY - rect.top) * (box.height / (rect.height || 1)) };
  };
  const down = (event: PointerEvent<HTMLCanvasElement>) => {
    if (busy || source.state !== 'ready') return;
    const point = at(event);
    if (tool === 'text') {
      if (words.trim()) setShapes((all) => [...all, { tool: 'text', at: point, words: words.trim() }]);
      else wordsField.current?.focus();
      return;
    }
    event.currentTarget.setPointerCapture?.(event.pointerId);
    setDrawing({ tool, from: point, to: point });
  };
  const move = (event: PointerEvent<HTMLCanvasElement>) => {
    if (drawing && drawing.tool !== 'text') setDrawing({ ...drawing, to: at(event) });
  };
  const up = (event: PointerEvent<HTMLCanvasElement>) => {
    if (!drawing || drawing.tool === 'text') return;
    const done = { ...drawing, to: at(event) };
    setDrawing(undefined);
    if (big(done)) setShapes((all) => [...all, done]);
  };

  const save = async () => {
    const box = canvas.current;
    if (!box || source.state !== 'ready' || !shapes.length) return;
    setBusy(true);
    setProblem(undefined);
    try {
      const type = source.type === 'image/jpeg' ? 'image/jpeg' : 'image/png';
      const image = await new Promise<Blob>((done, fail) => box.toBlob((blob) => (blob ? done(blob) : fail(new Error('toBlob'))), type, 0.92));
      await onSave(image);
    } catch (error) {
      setProblem(describe(error, m.saveFailed));
      setBusy(false);
    }
  };

  return (
    <KBDialog title={m.title} className="kb-annotate" closeLabel={m.close} returnFocus={false} fieldFirst={false} onClose={onClose}
      describedBy={`${id}-note`} footer={<>
        <button type="button" onClick={() => setShapes((all) => all.slice(0, -1))} disabled={busy || !shapes.length}>{m.undo}</button>
        <button type="button" onClick={onClose}>{m.cancel}</button>
        <button type="button" className="primary-action" onClick={() => void save()} disabled={busy || !shapes.length}>{busy ? m.saving : m.save}</button>
      </>}>
      <div className="kb-annotate-tools" role="group" aria-label={m.toolsLabel}>
        {tools.map((name) => (
          <button key={name} type="button" aria-pressed={tool === name} onClick={() => setTool(name)}>{m.tools[name]}</button>
        ))}
        {tool === 'text' && <>
          <label htmlFor={`${id}-words`} className="sr-only">{m.words}</label>
          <input ref={wordsField} id={`${id}-words`} type="text" maxLength={80} placeholder={m.words} value={words} onChange={(event) => setWords(event.target.value)} />
        </>}
      </div>
      <p id={`${id}-note`} className="kb-hint">{m.hints[tool]} {m.copyNote}</p>
      {problem && <p className="error-copy" role="alert">{problem}</p>}
      {source.state === 'loading' && <p className="kb-hint" role="status">{m.loading}</p>}
      {source.state === 'failed' && <p className="error-copy" role="alert">{m.loadFailed}</p>}
      <div className="kb-annotate-stage" hidden={source.state !== 'ready'}>
        <canvas ref={canvas} aria-label={m.canvas} data-tool={tool} onPointerDown={down} onPointerMove={move} onPointerUp={up}
          onPointerCancel={() => setDrawing(undefined)} />
      </div>
    </KBDialog>
  );
}
