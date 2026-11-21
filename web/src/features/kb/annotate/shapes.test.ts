import { expect, test } from 'vitest';
import { blurBlock } from './shapes';

// The squares are large next to the area blurred, so a short word or a few digits under it cannot be
// guessed back from the average colours.
test('a blurred area is cut into squares a third of its short side, never under 16 pixels', () => {
  expect(blurBlock(30, 200)).toBe(16);
  expect(blurBlock(120, 400)).toBe(40);
  expect(blurBlock(400, 121)).toBe(41);
});
