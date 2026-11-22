import { expect, test } from 'vitest';
import { pixelate } from './pixelate';

// A 4 × 2 image, row by row, four bytes (red, green, blue, alpha) per pixel.
const image = () => ({
  width: 4,
  height: 2,
  data: new Uint8ClampedArray([
    0, 0, 0, 255, 100, 0, 0, 255, 10, 20, 30, 255, 30, 40, 50, 255,
    0, 100, 0, 255, 0, 0, 100, 255, 50, 60, 70, 255, 70, 80, 90, 255,
  ]),
});

test('each square of the blurred area becomes the average of its pixels', () => {
  const picture = image();
  pixelate(picture, { x: 0, y: 0, width: 4, height: 2 }, 2);
  expect([...picture.data]).toEqual([
    25, 25, 25, 255, 25, 25, 25, 255, 40, 50, 60, 255, 40, 50, 60, 255,
    25, 25, 25, 255, 25, 25, 25, 255, 40, 50, 60, 255, 40, 50, 60, 255,
  ]);
});

test('pixels outside the blurred area keep their colour, and an area past the edge stops at it', () => {
  const picture = image();
  pixelate(picture, { x: 2, y: 0, width: 10, height: 10 }, 2);
  expect([...picture.data]).toEqual([
    0, 0, 0, 255, 100, 0, 0, 255, 40, 50, 60, 255, 40, 50, 60, 255,
    0, 100, 0, 255, 0, 0, 100, 255, 40, 50, 60, 255, 40, 50, 60, 255,
  ]);
});

test('an area drawn from right to left blurs the same pixels', () => {
  const picture = image();
  pixelate(picture, { x: 4, y: 2, width: -2, height: -2 }, 2);
  expect([...picture.data].slice(8, 16)).toEqual([40, 50, 60, 255, 40, 50, 60, 255]);
  expect([...picture.data].slice(0, 8)).toEqual([0, 0, 0, 255, 100, 0, 0, 255]);
});
