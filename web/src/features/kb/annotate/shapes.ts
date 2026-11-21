import { clampArea, pixelate } from './pixelate';

export type Tool = 'arrow' | 'box' | 'text' | 'blur';
export const tools: readonly Tool[] = ['arrow', 'box', 'text', 'blur'];

export interface Point { x: number; y: number }

// A Shape is one thing drawn on the image, in the image's own pixels.
export type Shape =
  | { tool: 'arrow' | 'box' | 'blur'; from: Point; to: Point }
  | { tool: 'text'; at: Point; words: string };

// The marks are red with a white edge, so they read on a light and on a dark screenshot alike.
const ink = '#e5484d';
const edge = '#ffffff';

const sizes = (width: number, height: number) => {
  const short = Math.min(width, height);
  return { stroke: Math.max(2, Math.round(short / 150)), block: Math.max(8, Math.round(short / 40)), font: Math.max(14, Math.round(short / 25)) };
};

// A shape smaller than this in both directions is a stray click, not a mark.
export const big = (shape: Shape) => shape.tool === 'text' || Math.abs(shape.to.x - shape.from.x) >= 4 || Math.abs(shape.to.y - shape.from.y) >= 4;

// draw paints the image and then every shape on the canvas. Blur goes first and works on the pixels
// themselves, so the saved bytes hold the squares and not what was under them.
export function draw(context: CanvasRenderingContext2D, image: CanvasImageSource, shapes: readonly Shape[]) {
  const { width, height } = context.canvas;
  const size = sizes(width, height);
  context.clearRect(0, 0, width, height);
  context.drawImage(image, 0, 0, width, height);
  for (const shape of shapes) {
    if (shape.tool !== 'blur') continue;
    const area = clampArea({ x: shape.from.x, y: shape.from.y, width: shape.to.x - shape.from.x, height: shape.to.y - shape.from.y }, width, height);
    const w = area.right - area.left;
    const h = area.bottom - area.top;
    if (w < 1 || h < 1) continue;
    const pixels = context.getImageData(area.left, area.top, w, h);
    pixelate(pixels, { x: 0, y: 0, width: w, height: h }, size.block);
    context.putImageData(pixels, area.left, area.top);
  }
  context.lineCap = 'round';
  context.lineJoin = 'round';
  for (const shape of shapes) {
    if (shape.tool === 'text') words(context, shape.at, shape.words, size.font, size.stroke);
    else if (shape.tool === 'box') stroke(context, size.stroke, () => boxPath(context, shape.from, shape.to));
    else if (shape.tool === 'arrow') stroke(context, size.stroke, () => arrowPath(context, shape.from, shape.to, size.stroke * 5));
  }
}

// stroke draws a path twice: a wider white edge, then the red mark on it.
function stroke(context: CanvasRenderingContext2D, width: number, path: () => void) {
  for (const [colour, line] of [[edge, width + 2], [ink, width]] as const) {
    context.strokeStyle = colour;
    context.lineWidth = line;
    context.beginPath();
    path();
    context.stroke();
  }
}

function boxPath(context: CanvasRenderingContext2D, from: Point, to: Point) {
  context.rect(Math.min(from.x, to.x), Math.min(from.y, to.y), Math.abs(to.x - from.x), Math.abs(to.y - from.y));
}

function arrowPath(context: CanvasRenderingContext2D, from: Point, to: Point, head: number) {
  const angle = Math.atan2(to.y - from.y, to.x - from.x);
  context.moveTo(from.x, from.y);
  context.lineTo(to.x, to.y);
  for (const side of [-1, 1]) {
    context.moveTo(to.x, to.y);
    context.lineTo(to.x - head * Math.cos(angle + side * Math.PI / 7), to.y - head * Math.sin(angle + side * Math.PI / 7));
  }
}

// words writes red text with a white outline.
function words(context: CanvasRenderingContext2D, at: Point, text: string, font: number, line: number) {
  context.font = `600 ${font}px system-ui, sans-serif`;
  context.textBaseline = 'middle';
  context.lineWidth = Math.max(3, line + 1);
  context.strokeStyle = edge;
  context.fillStyle = ink;
  context.strokeText(text, at.x, at.y);
  context.fillText(text, at.x, at.y);
}
