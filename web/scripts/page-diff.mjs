// Scores how far a rebuilt help center page is from the page it replaces.
// Images are { width, height, data: RGBA bytes }; boxes are { x, y, width, height } in image pixels.

const MAX_RGB_DISTANCE = 255 * Math.sqrt(3);

function pixelDiffers(a, ai, b, bi, limit) {
  const dr = a.data[ai] - b.data[bi];
  const dg = a.data[ai + 1] - b.data[bi + 1];
  const db = a.data[ai + 2] - b.data[bi + 2];
  return Math.sqrt(dr * dr + dg * dg + db * db) > limit;
}

// Share of pixels that differ by more than `fuzz` of the largest RGB distance.
// Where the images differ in size, pixels only one image has count as changed.
export function diffFraction(a, b, { fuzz = 0.1 } = {}) {
  const limit = fuzz * MAX_RGB_DISTANCE;
  const width = Math.max(a.width, b.width);
  const height = Math.max(a.height, b.height);
  const total = width * height;
  const overlapW = Math.min(a.width, b.width);
  const overlapH = Math.min(a.height, b.height);
  let changed = total - overlapW * overlapH;
  for (let y = 0; y < overlapH; y++) {
    for (let x = 0; x < overlapW; x++) {
      if (pixelDiffers(a, (y * a.width + x) * 4, b, (y * b.width + x) * 4, limit)) changed++;
    }
  }
  return { changed, total, fraction: total === 0 ? 0 : changed / total };
}

// Copies the part of `img` under `box`; the box is clipped to the image.
export function crop(img, box) {
  const x0 = Math.max(0, Math.round(box.x));
  const y0 = Math.max(0, Math.round(box.y));
  const width = Math.max(0, Math.min(img.width, Math.round(box.x + box.width)) - x0);
  const height = Math.max(0, Math.min(img.height, Math.round(box.y + box.height)) - y0);
  const data = new Uint8ClampedArray(width * height * 4);
  for (let row = 0; row < height; row++) {
    const start = ((y0 + row) * img.width + x0) * 4;
    data.set(img.data.subarray(start, start + width * 4), row * width * 4);
  }
  return { width, height, data };
}

// Moves every row of img down by px rows; the rows that open at the top are white.
export function shiftDown(img, px) {
  const data = new Uint8ClampedArray(img.data.length).fill(255);
  data.set(img.data.subarray(0, Math.max(0, img.height - px) * img.width * 4), px * img.width * 4);
  return { width: img.width, height: img.height, data };
}

function captured(img, box) {
  return box.x >= 0 && box.y >= 0 && Math.round(box.x + box.width) <= img.width && Math.round(box.y + box.height) <= img.height;
}

// Share of the two boxes' union they do not share, with both moved to the same top: a block lower on the
// page because what sits above it is taller is not a change of its own, a block moved sideways is.
function boxGeometry(a, b) {
  const overlapW = Math.max(0, Math.min(a.x + a.width, b.x + b.width) - Math.max(a.x, b.x));
  const overlapH = Math.min(a.height, b.height);
  const intersection = overlapW * overlapH;
  const union = a.width * a.height + b.width * b.height - intersection;
  return union === 0 ? 0 : 1 - intersection / union;
}

const area = (box) => Math.round(box.width) * Math.round(box.height);

// Compares each named block of the old page with the same block of the new page, wherever it sits.
// Two numbers per block: `geometry` (0 when the boxes match in size and column) and the share of pixels
// that differ inside it (`changed` / `total`). A block the new page lacks counts in full in both.
// Blocks that run past either screenshot are listed as `uncaptured` and left out of both numbers.
export function layoutScore(oldImg, newImg, oldBoxes, newBoxes, options) {
  const blocks = [];
  const missing = [];
  const uncaptured = [];
  let changed = 0;
  let total = 0;
  let geometryWeighted = 0;
  let geometryArea = 0;
  for (const [name, oldBox] of Object.entries(oldBoxes)) {
    const newBox = newBoxes[name];
    if (!newBox) {
      missing.push(name);
      changed += area(oldBox);
      total += area(oldBox);
      geometryWeighted += area(oldBox);
      geometryArea += area(oldBox);
      continue;
    }
    if (!captured(oldImg, oldBox) || !captured(newImg, newBox)) {
      uncaptured.push(name);
      continue;
    }
    const diff = diffFraction(crop(oldImg, oldBox), crop(newImg, newBox), options);
    const geometry = boxGeometry(oldBox, newBox);
    blocks.push({
      name,
      changed: diff.changed,
      total: diff.total,
      geometry,
      moved: {
        dx: Math.round(newBox.x - oldBox.x),
        dy: Math.round(newBox.y - oldBox.y),
        dw: Math.round(newBox.width - oldBox.width),
        dh: Math.round(newBox.height - oldBox.height),
      },
    });
    changed += diff.changed;
    total += diff.total;
    geometryWeighted += geometry * area(oldBox);
    geometryArea += area(oldBox);
  }
  const extra = Object.keys(newBoxes).filter((name) => !(name in oldBoxes));
  return {
    blocks,
    missing,
    uncaptured,
    extra,
    changed,
    total,
    fraction: total === 0 ? 0 : changed / total,
    geometry: geometryArea === 0 ? 0 : geometryWeighted / geometryArea,
  };
}
