// Pixels is the shape of a canvas ImageData: four bytes (red, green, blue, alpha) per pixel, row by row.
export interface Pixels {
  width: number;
  height: number;
  data: Uint8ClampedArray;
}

export interface Area {
  x: number;
  y: number;
  width: number;
  height: number;
}

// clampArea puts an area drawn in any direction the right way round and inside the picture.
export function clampArea(area: Area, width: number, height: number) {
  const left = Math.max(0, Math.round(Math.min(area.x, area.x + area.width)));
  const top = Math.max(0, Math.round(Math.min(area.y, area.y + area.height)));
  const right = Math.min(width, Math.round(Math.max(area.x, area.x + area.width)));
  const bottom = Math.min(height, Math.round(Math.max(area.y, area.y + area.height)));
  return { left, top, right: Math.max(left, right), bottom: Math.max(top, bottom) };
}

// pixelate turns the area into squares of block pixels, each the average colour of the pixels it covers,
// so what was under it cannot be read back from the saved image.
export function pixelate(picture: Pixels, area: Area, block: number) {
  const { width, data } = picture;
  const { left, top, right, bottom } = clampArea(area, picture.width, picture.height);
  for (let y = top; y < bottom; y += block) {
    for (let x = left; x < right; x += block) {
      const endX = Math.min(x + block, right);
      const endY = Math.min(y + block, bottom);
      const sum = [0, 0, 0, 0];
      for (let row = y; row < endY; row += 1) {
        for (let column = x; column < endX; column += 1) {
          const at = (row * width + column) * 4;
          for (let channel = 0; channel < 4; channel += 1) sum[channel]! += data[at + channel]!;
        }
      }
      const count = (endX - x) * (endY - y);
      const average = sum.map((total) => Math.round(total / count));
      for (let row = y; row < endY; row += 1) {
        for (let column = x; column < endX; column += 1) data.set(average, (row * width + column) * 4);
      }
    }
  }
}
