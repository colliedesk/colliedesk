import { expect, test } from 'vitest';
import { renderSavedReply, savedReplyMatches } from './saved-reply-variables';

const who = { customerName: 'Mai Linh', agentName: 'Chinh' };

test('fills both variables, every time they appear', () => {
  expect(renderSavedReply('Hi {{customer.name}},\nThanks, {{customer.name}}.\n{{agent.name}}', who))
    .toBe('Hi Mai Linh,\nThanks, Mai Linh.\nChinh');
});

test('spaces inside the braces are allowed', () => {
  expect(renderSavedReply('Hi {{ customer.name }} from {{agent.name  }}', who)).toBe('Hi Mai Linh from Chinh');
});

test('an unknown variable stays as written for the agent to see', () => {
  expect(renderSavedReply('See {{ticket.url}} {{customer.name}} {{customer.name}', { customerName: 'An' }))
    .toBe('See {{ticket.url}} An {{customer.name}');
});

test('an unknown customer never leaves a raw variable and closes the gap before it', () => {
  expect(renderSavedReply('Hi {{customer.name}}, thanks.', { agentName: 'Chinh' })).toBe('Hi, thanks.');
  expect(renderSavedReply('Hello {{customer.name}} and welcome', { customerName: '   ' })).toBe('Hello and welcome');
  expect(renderSavedReply('{{customer.name}}', {})).toBe('');
});

test('an unknown agent name is dropped the same way', () => {
  expect(renderSavedReply('Regards,\n{{agent.name}}', { customerName: 'An' })).toBe('Regards,\n');
  expect(renderSavedReply('Sent by {{agent.name}}.', {})).toBe('Sent by.');
});

test('names are inserted as text: no replacement patterns, no second pass over the result', () => {
  expect(renderSavedReply('Hi {{customer.name}}', { customerName: 'A$&B$1', agentName: 'x' })).toBe('Hi A$&B$1');
  expect(renderSavedReply('Hi {{customer.name}} / {{agent.name}}', { customerName: '{{agent.name}}', agentName: 'Chinh' }))
    .toBe('Hi {{agent.name}} / Chinh');
});

test('names are trimmed and Vietnamese text is kept as it is', () => {
  expect(renderSavedReply('Chào {{customer.name}}', { customerName: '  Nguyễn Thị Ánh ' })).toBe('Chào Nguyễn Thị Ánh');
});

test('a title matches a search ignoring case and accents', () => {
  expect(savedReplyMatches('Chào hỏi khách', 'chao')).toBe(true);
  expect(savedReplyMatches('Refund steps', 'FUND')).toBe(true);
  expect(savedReplyMatches('Refund steps', '  ')).toBe(true);
  expect(savedReplyMatches('Refund steps', 'shipping')).toBe(false);
  expect(savedReplyMatches('Đơn hàng', 'don hang')).toBe(true);
});
