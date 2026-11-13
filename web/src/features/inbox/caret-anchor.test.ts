import { expect, test } from 'vitest';
import { placeAtCaret } from './caret-anchor';

// A laptop screen and a 280 x 100 list; the @ is drawn on a 21 px line. Expected places are worked out by hand: 4 px
// between the line and the list, 8 px kept free at each edge of the screen.
const laptop = { width: 1440, height: 900 };
const list = { width: 280, height: 100 };

test('the list opens under the line of the @, its left edge at the @', () => {
  expect(placeAtCaret({ left: 726, top: 752, bottom: 773 }, list, laptop)).toEqual({ left: 726, top: 777 });
});

test('with no room under the line before the foot of the screen, the list opens above it', () => {
  // Under it would end at 845 + 100 = 945, past 900 - 8.
  expect(placeAtCaret({ left: 726, top: 820, bottom: 841 }, list, laptop)).toEqual({ left: 726, top: 716 });
});

test('an @ near the right edge of a phone moves the list left so it ends 8 px inside the screen', () => {
  expect(placeAtCaret({ left: 300, top: 400, bottom: 424 }, list, { width: 390, height: 844 })).toEqual({ left: 102, top: 428 });
});

test('a list wider than the screen starts 8 px from its left edge', () => {
  expect(placeAtCaret({ left: 150, top: 400, bottom: 424 }, { width: 400, height: 100 }, { width: 320, height: 844 })).toEqual({ left: 8, top: 428 });
});

test('when neither side has room, the list takes the roomier side and stays inside the screen', () => {
  // Under: 300 - 8 - 145 = 147 px of room; above: 120 - 4 - 8 = 108 px. Under wins, pulled up to end at 292.
  expect(placeAtCaret({ left: 40, top: 120, bottom: 141 }, { width: 280, height: 250 }, { width: 1024, height: 300 })).toEqual({ left: 40, top: 42 });
});
