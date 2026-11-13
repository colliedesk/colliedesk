import { describe, expect, it } from 'vitest';
import { isNearBottom, newestOffset } from './newest-in-view';

describe('isNearBottom', () => {
  it('is true within 80 px of the end and false beyond it', () => {
    // 5000 px of content in a 500 px pane: the end is at an offset of 4500.
    expect(isNearBottom({ scrollTop: 4500, clientHeight: 500, scrollHeight: 5000 })).toBe(true);
    expect(isNearBottom({ scrollTop: 4420, clientHeight: 500, scrollHeight: 5000 })).toBe(true);
    expect(isNearBottom({ scrollTop: 4419, clientHeight: 500, scrollHeight: 5000 })).toBe(false);
    expect(isNearBottom({ scrollTop: 0, clientHeight: 500, scrollHeight: 5000 })).toBe(false);
  });

  it('is true when everything fits and there is nothing to scroll', () => {
    expect(isNearBottom({ scrollTop: 0, clientHeight: 500, scrollHeight: 300 })).toBe(true);
  });
});

describe('newestOffset', () => {
  const pane = { viewport: 500, padding: 16 };

  it('puts a newest message that fits at the end of the pane', () => {
    // The message starts at 4600 and is 300 px tall, in 5000 px of content: scrolled to 4500 its end is in view.
    expect(newestOffset({ ...pane, top: 4600, height: 300, content: 5000 })).toBe(4500);
  });

  it('puts the start of a newest message taller than the pane at the top, under the padding', () => {
    // 900 px tall in a 500 px pane (468 px inside the padding): its start at 4100 sits 16 px under the pane top.
    expect(newestOffset({ ...pane, top: 4100, height: 900, content: 5016 })).toBe(4084);
  });

  it('treats a message exactly as tall as the room inside the padding as fitting', () => {
    expect(newestOffset({ ...pane, top: 1000, height: 468, content: 1484 })).toBe(984);
  });

  it('stays at the top when the content is shorter than the pane', () => {
    expect(newestOffset({ ...pane, top: 16, height: 200, content: 232 })).toBe(0);
  });
});
