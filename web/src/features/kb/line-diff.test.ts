import { expect, test } from 'vitest';
import { lineDiff } from './line-diff';

test('a changed line shows as removed then added, between the lines both texts keep', () => {
  expect(lineDiff('Open Settings.\nChoose Security.\nSave.', 'Open Settings.\nChoose Password.\nSave.')).toEqual([
    { change: 'same', text: 'Open Settings.' },
    { change: 'removed', text: 'Choose Security.' },
    { change: 'added', text: 'Choose Password.' },
    { change: 'same', text: 'Save.' },
  ]);
});

test('lines added at the end and removed from the start are each named once', () => {
  expect(lineDiff('Intro\nStep one\nStep two', 'Step one\nStep two\nDone')).toEqual([
    { change: 'removed', text: 'Intro' },
    { change: 'same', text: 'Step one' },
    { change: 'same', text: 'Step two' },
    { change: 'added', text: 'Done' },
  ]);
});

test('a moved line is removed where it was and added where it is', () => {
  expect(lineDiff('a\nb\nc', 'b\nc\na')).toEqual([
    { change: 'removed', text: 'a' },
    { change: 'same', text: 'b' },
    { change: 'same', text: 'c' },
    { change: 'added', text: 'a' },
  ]);
});

test('an empty text has no lines, and Windows line endings are no change', () => {
  expect(lineDiff('', 'One\nTwo')).toEqual([{ change: 'added', text: 'One' }, { change: 'added', text: 'Two' }]);
  expect(lineDiff('One', '')).toEqual([{ change: 'removed', text: 'One' }]);
  expect(lineDiff('One\r\nTwo\r\n', 'One\nTwo\n')).toEqual([{ change: 'same', text: 'One' }, { change: 'same', text: 'Two' }, { change: 'same', text: '' }]);
});
