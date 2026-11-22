// @vitest-environment node
import { expect, test } from 'vitest';
import { diffFraction, layoutScore, shiftDown } from './page-diff.mjs';

const WHITE = [255, 255, 255];

function image(width, height, fill = WHITE) {
  const data = new Uint8ClampedArray(width * height * 4);
  for (let i = 0; i < width * height; i++) data.set([...fill, 255], i * 4);
  return { width, height, data };
}

function paint(img, x, y, w, h, color) {
  for (let row = y; row < y + h; row++) {
    for (let col = x; col < x + w; col++) img.data.set([...color, 255], (row * img.width + col) * 4);
  }
  return img;
}

test('identical images differ in no pixel', () => {
  expect(diffFraction(image(100, 100), image(100, 100))).toEqual({ changed: 0, total: 10000, fraction: 0 });
});

test('a 10×10 red block on a 100×100 white page is 1% of the page', () => {
  const changed = paint(image(100, 100), 40, 40, 10, 10, [255, 0, 0]);
  expect(diffFraction(image(100, 100), changed).fraction).toBe(0.01);
});

test('a colour within the fuzz counts as the same, one past it does not', () => {
  const near = paint(image(10, 10), 0, 0, 10, 10, [250, 250, 250]);
  const far = paint(image(10, 10), 0, 0, 10, 10, [200, 200, 200]);
  expect(diffFraction(image(10, 10), near, { fuzz: 0.1 }).changed).toBe(0);
  expect(diffFraction(image(10, 10), far, { fuzz: 0.1 }).changed).toBe(100);
});

test('rows only one image has count as changed', () => {
  expect(diffFraction(image(100, 100), image(100, 110))).toEqual({ changed: 1000, total: 11000, fraction: 1000 / 11000 });
});

test('a block moved down whole is the same layout though most pixels moved', () => {
  const before = paint(image(100, 100), 0, 0, 100, 20, [30, 30, 30]);
  const after = paint(image(100, 100), 0, 20, 100, 20, [30, 30, 30]);
  // rows 0–20 lose the band and rows 20–40 gain it: 40 of 100 rows differ
  expect(diffFraction(before, after).fraction).toBe(0.4);
  const score = layoutScore(before, after, { header: { x: 0, y: 0, width: 100, height: 20 } }, { header: { x: 0, y: 20, width: 100, height: 20 } });
  expect(score.fraction).toBe(0);
  expect(score.geometry).toBe(0);
  expect(score.blocks).toEqual([{ name: 'header', changed: 0, total: 2000, geometry: 0, moved: { dx: 0, dy: 20, dw: 0, dh: 0 } }]);
});

test('a block the new page lacks counts as changed over its whole area', () => {
  const page = paint(image(100, 100), 70, 0, 30, 50, [200, 0, 0]);
  const score = layoutScore(page, image(100, 100), { header: { x: 0, y: 0, width: 70, height: 10 }, sidebar: { x: 70, y: 0, width: 30, height: 50 } }, { header: { x: 0, y: 0, width: 70, height: 10 } });
  expect(score.missing).toEqual(['sidebar']);
  expect(score.fraction).toBe(1500 / 2200);
  expect(score.geometry).toBe(1500 / 2200);
});

test('a block of another size is compared over the larger box', () => {
  const score = layoutScore(image(100, 100), image(100, 100), { content: { x: 0, y: 0, width: 50, height: 50 } }, { content: { x: 0, y: 0, width: 50, height: 60 } });
  expect(score.blocks[0]).toMatchObject({ changed: 500, total: 3000 });
});

test('geometry ignores where the page flow puts a block but counts a sideways move', () => {
  const page = image(300, 300);
  // half the old box overlaps the new one: intersection 5000, union 15000
  const score = layoutScore(page, page, { content: { x: 0, y: 0, width: 100, height: 100 } }, { content: { x: 50, y: 120, width: 100, height: 100 } });
  expect(score.blocks[0].geometry).toBeCloseTo(2 / 3, 12);
  expect(score.geometry).toBeCloseTo(2 / 3, 12);
});

test('a block that runs past the screenshot is reported, not scored', () => {
  const score = layoutScore(image(100, 100), image(100, 100), { header: { x: 0, y: 0, width: 100, height: 10 }, footer: { x: 0, y: 95, width: 100, height: 20 } }, { header: { x: 0, y: 0, width: 100, height: 10 }, footer: { x: 0, y: 90, width: 100, height: 20 } });
  expect(score.uncaptured).toEqual(['footer']);
  expect(score.blocks.map((b) => b.name)).toEqual(['header']);
  expect(score.total).toBe(1000);
});

test('blocks only the new page has are listed', () => {
  const score = layoutScore(image(100, 100), image(100, 100), { header: { x: 0, y: 0, width: 100, height: 10 } }, { header: { x: 0, y: 0, width: 100, height: 10 }, chat: { x: 0, y: 50, width: 10, height: 10 } });
  expect(score.extra).toEqual(['chat']);
});

test('shiftDown moves every row down and fills the top with white', () => {
  const img = paint(image(2, 3), 0, 0, 2, 1, [0, 0, 0]);
  const shifted = shiftDown(img, 1);
  expect([...shifted.data.subarray(0, 8)]).toEqual([255, 255, 255, 255, 255, 255, 255, 255]);
  expect([...shifted.data.subarray(8, 16)]).toEqual([0, 0, 0, 255, 0, 0, 0, 255]);
  expect([...shifted.data.subarray(16, 24)]).toEqual([255, 255, 255, 255, 255, 255, 255, 255]);
});
