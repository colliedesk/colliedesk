import { expect, test } from 'vitest';
import { unwrapHardWraps } from './plain-text-body';

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
