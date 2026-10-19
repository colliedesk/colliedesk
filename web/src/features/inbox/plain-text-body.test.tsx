import { expect, test } from 'vitest';
import { render } from '@testing-library/react';
import { PlainTextBody, tidyPlainText, unwrapHardWraps } from './plain-text-body';

// The acceptance-test email of 2026-10-15 as Gmail sent it: text/plain hard-wrapped near 72 characters.
const gmail = 'Hi,\n\n'
  + 'This is an automated acceptance test of Colliedesk Cloud (internal, HC\n'
  + 'Labs), now through the Colliedesk MX. Order 4417 arrived with a cracked\n'
  + 'lid. Can you send a replacement?\n\n'
  + 'Thanks,\nQA customer';

test('a paragraph the sender\'s client wrapped reads as one paragraph', () => {
  expect(unwrapHardWraps(gmail)).toBe('Hi,\n\n'
    + 'This is an automated acceptance test of Colliedesk Cloud (internal, HC Labs), now through the '
    + 'Colliedesk MX. Order 4417 arrived with a cracked lid. Can you send a replacement?\n\n'
    + 'Thanks,\nQA customer');
});

test('short lines the customer typed on purpose keep their breaks', () => {
  const address = 'Ship to:\nAda Lovelace\n12 Analytical Street\nLondon';
  expect(unwrapHardWraps(address)).toBe(address);
});

test('lists, quotes and signatures are never joined to the line above', () => {
  const long = 'Here is what went wrong with the order that arrived at our office this week:';
  for (const next of ['- the lid was cracked', '* the box was wet', '1. no invoice', '> earlier reply', '-- ', '--\nAda']) {
    expect(unwrapHardWraps(`${long}\n${next}`)).toBe(`${long}\n${next}`);
  }
});

test('a wrapped quote stays a quote line by line', () => {
  const quoted = '> On Monday you wrote that the replacement would ship on the same day as my\n> order was confirmed';
  expect(unwrapHardWraps(quoted)).toBe(quoted);
});

test('Windows line endings unwrap the same way', () => {
  expect(unwrapHardWraps(gmail.replaceAll('\n', '\r\n'))).toBe(unwrapHardWraps(gmail));
});

// Plain bodies made from HTML mail hold lines of nothing but spaces (up to 105 in the wild), which
// pre-wrap rendered as gaps of several hundred pixels.
test('runs of whitespace-only lines collapse into one blank line', () => {
  const gappy = `Weekly report\n${' '.repeat(105)}\n${' '.repeat(40)}\n\t\n${' '.repeat(7)}\nErrors: 3\n\n\n\nDone`;
  expect(tidyPlainText(gappy)).toBe('Weekly report\n\nErrors: 3\n\nDone');
});

test('trailing spaces are trimmed and a single blank line between paragraphs stays', () => {
  expect(tidyPlainText('Hi Ada,   \n\nYour order shipped.  \nThanks  \n')).toBe('Hi Ada,\n\nYour order shipped.\nThanks');
});

test('leading blank lines go, leading spaces of text lines stay (signatures, indented lists)', () => {
  expect(tidyPlainText('   \n\n  indented line\n    - item')).toBe('  indented line\n    - item');
});

test('quote markers alone on a line are kept; Windows line endings are handled', () => {
  expect(tidyPlainText('> one\r\n>\r\n> two\r\n   \r\n\r\nend')).toBe('> one\n>\n> two\n\nend');
});

test('a body that is only whitespace becomes empty', () => {
  expect(tidyPlainText(`${' '.repeat(50)}\n\n   `)).toBe('');
});

test('the rendered plain text never contains a run of blank lines', () => {
  const { container } = render(<PlainTextBody text={`Title\n${' '.repeat(105)}\n${' '.repeat(105)}\n${' '.repeat(105)}\nBody`} />);
  expect(container.textContent).toBe('Title\n\nBody');
});
