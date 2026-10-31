import { readFileSync } from 'node:fs';
import { fireEvent, render, screen } from '@testing-library/react';
import { expect, test } from 'vitest';
import type { Message } from '../../api/schemas';
import { MessageItem } from './message-item';

const base: Message = { reply_number: null,
  id: '22222222-2222-4222-8222-222222222222', conversation_id: '11111111-1111-4111-8111-111111111111', author_agent_id: null, author_name: 'Vic Customer',
  agent_token: null, ai_draft_id: null, ai_draft_outcome: null, agent_request_id: null, kind: 'incoming', body: 'Plain version of the newsletter', body_html: null, origin: null,
  attachments: [], cc: [], created_at: '2026-10-14T00:00:00Z', delivery: null,
  email: { from: 'vic@customer.example', authentication: 'pass', attachments: [], original_attachment_id: null, cc: [] },
};
const file = (id: string, filename: string, content_type: string, inline = false) => ({ id, message_id: base.id, filename, content_type, size: 2048, sha256: 'a'.repeat(64), inline, created_at: base.created_at });

test('incoming mail with HTML shows the rendered HTML by default, inline in the conversation', () => {
  const { container } = render(<MessageItem item={{ ...base, body_html: '<h1>Weekly report</h1><table><tr><td>42</td></tr></table>' }} timezone="UTC" />);
  const frame = screen.getByTitle('Original message');
  expect(frame.tagName).toBe('IFRAME');
  expect(frame.getAttribute('srcdoc')).toContain('<table>');
  expect(frame.getAttribute('srcdoc')).toContain('Weekly report');
  expect(container.querySelector('.plain-text-body')).toBeNull();
});

test('the frame keeps the sandbox: no scripts, no forms, no top navigation', () => {
  render(<MessageItem item={{ ...base, body_html: '<p>Hi</p>' }} timezone="UTC" />);
  const frame = screen.getByTitle('Original message');
  const tokens = (frame.getAttribute('sandbox') ?? '').split(/\s+/);
  expect(tokens).toContain('allow-same-origin');
  expect(tokens).toContain('allow-popups');
  for (const forbidden of ['allow-scripts', 'allow-forms', 'allow-top-navigation', 'allow-top-navigation-by-user-activation', 'allow-modals']) {
    expect(tokens).not.toContain(forbidden);
  }
  expect(frame.getAttribute('srcdoc')).toContain("script-src 'none'");
});

test('Show plain text swaps the frame for the stored plain text, and back', () => {
  const { container } = render(<MessageItem item={{ ...base, body_html: '<p>Hello <b>there</b></p>' }} timezone="UTC" />);
  fireEvent.click(screen.getByRole('button', { name: 'Show plain text' }));
  expect(container.querySelector('iframe')).toBeNull();
  expect(screen.getByText('Plain version of the newsletter')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Show formatted email' }));
  expect(screen.getByTitle('Original message')).toBeInTheDocument();
  expect(container.querySelector('.plain-text-body')).toBeNull();
});

test('a message without HTML keeps the plain-text view and offers no toggle', () => {
  const { container } = render(<MessageItem item={base} timezone="UTC" />);
  expect(container.querySelector('iframe')).toBeNull();
  expect(screen.getByText('Plain version of the newsletter')).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /plain text|formatted/i })).toBeNull();
});

test('replies and notes written in Colliedesk have no HTML view', () => {
  const { container } = render(<MessageItem item={{ ...base, kind: 'outgoing', body: 'On its way.', email: null }} timezone="UTC" />);
  expect(container.querySelector('iframe')).toBeNull();
  expect(screen.getByText('On its way.')).toBeInTheDocument();
});

test('a file the server marked as embedded in the HTML is not listed again', () => {
  const item = { ...base, body_html: '<img src="data:image/png;base64,AAAA">', attachments: [
    file('a1', 'logo.png', 'image/png', true),
    file('a2', 'invoice.pdf', 'application/pdf'),
  ] };
  render(<MessageItem item={item} timezone="UTC" />);
  const names = Array.from(screen.getByRole('list', { name: /attachments/i }).querySelectorAll('a')).map((link) => link.textContent);
  expect(names).toEqual(['invoice.pdf']);
});

test('the file name decides nothing: an image that was not embedded stays listed, however it is named', () => {
  const item = { ...base, body_html: '<p>Hi</p>', attachments: [
    file('a1', 'inline-image-1a2b3c4d5e6f7a8b.png', 'image/png', false), // over the embed budget, or a sender's choice of name
    file('a2', 'photo.png', 'image/png', true),
  ] };
  render(<MessageItem item={item} timezone="UTC" />);
  const names = Array.from(screen.getByRole('list', { name: /attachments/i }).querySelectorAll('a')).map((link) => link.textContent);
  expect(names).toEqual(['inline-image-1a2b3c4d5e6f7a8b.png']);
});

test('in the plain-text view an embedded file is a row again, since nothing else shows it', () => {
  const item = { ...base, body_html: '<img src="data:image/png;base64,AAAA">', attachments: [file('a1', 'logo.png', 'image/png', true)] };
  render(<MessageItem item={item} timezone="UTC" />);
  expect(screen.queryByRole('list', { name: /attachments/i })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Show plain text' }));
  expect(screen.getByRole('link', { name: 'logo.png' })).toBeInTheDocument();
});

test('the stored raw email stays out of the file list as before', () => {
  const item = { ...base, email: { ...base.email!, original_attachment_id: 'raw1' }, attachments: [file('raw1', 'original-message.eml.gz', 'application/gzip'), file('a2', 'invoice.pdf', 'application/pdf')] };
  render(<MessageItem item={item} timezone="UTC" />);
  const names = Array.from(screen.getByRole('list', { name: /attachments/i }).querySelectorAll('a')).map((link) => link.textContent);
  expect(names).toEqual(['invoice.pdf']);
});

// A messenger or bot acknowledgement flagged by the source is an event, not a conversation turn: one line
// with a chevron, open on demand.
const automated = { ...base, body: 'We received your message and will reply soon.', body_html: '<p>We received your message</p>',
  origin: { system: 'freshdesk', external_id: '7101', kind: 'automated_response' as const, from: 'bot@sample.example', to: ['support@acme.example'], cc: [], bcc: [] } };

test('an automated response is one collapsed line that names its sender, with the message behind it', () => {
  const { container } = render(<MessageItem item={automated} timezone="UTC" />);
  const row = screen.getByRole('button', { name: /bot@sample.example sent an automated response/ });
  expect(row).toHaveAttribute('aria-expanded', 'false');
  expect(container.querySelector('iframe')).toBeNull();
  expect(screen.queryByText(/We received your message/)).toBeNull();
  expect(screen.queryByRole('button', { name: 'Show plain text' })).toBeNull();
  fireEvent.click(row);
  expect(row).toHaveAttribute('aria-expanded', 'true');
  expect(screen.getByTitle('Original message')).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Show plain text' })).toBeInTheDocument();
  fireEvent.click(row);
  expect(row).toHaveAttribute('aria-expanded', 'false');
  expect(container.querySelector('iframe')).toBeNull();
});

test('an ordinary incoming message with the same body is not folded', () => {
  render(<MessageItem item={{ ...automated, origin: { ...automated.origin, kind: 'incoming' as const } }} timezone="UTC" />);
  expect(screen.queryByRole('button', { name: /automated response/ })).toBeNull();
  expect(screen.getByTitle('Original message')).toBeInTheDocument();
});

test('a failed delivery says why in text, not only on hover', () => {
  const reason = 'Google no longer accepts the Gmail connection. Reconnect Gmail in Settings, then retry the reply.';
  render(<MessageItem item={{ ...base, kind: 'outgoing', email: null, delivery: { status: 'failed', attempts: 1, last_error: reason, sent_at: null } }} timezone="UTC" />);
  expect(screen.getByText(reason)).toBeVisible();
  expect(screen.getByRole('button', { name: 'Retry delivery' })).toBeInTheDocument();
});

test('a reply sent from an AI draft says so, and whether it was edited', () => {
  const reply = { ...base, kind: 'outgoing' as const, email: null, author_name: 'Mai', ai_draft_id: '33333333-3333-4333-8333-333333333333' };
  const { rerender } = render(<MessageItem item={{ ...reply, ai_draft_outcome: 'sent_edited' }} timezone="UTC" />);
  expect(screen.getByText('from an AI draft, edited')).toBeInTheDocument();
  rerender(<MessageItem item={{ ...reply, ai_draft_outcome: 'sent_unchanged' }} timezone="UTC" />);
  expect(screen.getByText('from an AI draft, sent as written')).toBeInTheDocument();
});

// jsdom draws no list markers and orders its cascade by source alone, so this reads the shipped stylesheet: the last
// rule in components.css that matches the rendered element and sets the property is the one that applies.
function declared(element: Element, property: 'list-style-type' | 'padding-left') {
  const style = document.createElement('style');
  style.textContent = readFileSync('src/styles/components.css', 'utf8');
  document.head.append(style);
  const rules = [...style.sheet!.cssRules].filter((rule): rule is CSSStyleRule => rule instanceof CSSStyleRule);
  style.remove();
  return rules.filter((rule) => element.matches(rule.selectorText) && rule.style.getPropertyValue(property)).at(-1)?.style.getPropertyValue(property);
}

test('a reply or note written as a Markdown list keeps its numbers and bullets on the timeline', () => {
  for (const kind of ['outgoing', 'note'] as const) {
    const { container, unmount } = render(<MessageItem item={{ ...base, kind, email: null, body: 'Steps:\n\n1. Open settings\n2. Turn it on\n   - first\n   - second\n\nThanks' }} timezone="UTC" />);
    const ol = container.querySelector('.markdown-body ol')!;
    expect(declared(ol, 'list-style-type')).toBe('decimal');
    expect(declared(ol, 'padding-left')).toBe('var(--space-6)');
    expect(declared(ol.querySelector('ul')!, 'list-style-type')).toBe('circle');
    unmount();
  }
  const { container } = render(<MessageItem item={{ ...base, kind: 'outgoing', email: null, body: '- one\n- two' }} timezone="UTC" />);
  expect(declared(container.querySelector('.markdown-body ul')!, 'list-style-type')).toBe('disc');
});

test('a reply saved while its channel was off reads as not sent yet, not as a failure, and can be retried', () => {
  render(<MessageItem item={{ ...base, kind: 'outgoing', email: null, delivery: { status: 'failed', attempts: 0, last_error: 'Email is off.', sent_at: null, reason_code: 'channel_off' } }} timezone="UTC" />);
  expect(screen.getByText('Saved, not sent')).toHaveClass('badge-delivery-queued');
  expect(screen.queryByText('Delivery failed')).toBeNull();
  expect(screen.getByRole('button', { name: 'Retry delivery' })).toBeInTheDocument();
});

test('a reply whose channel was removed, or whose customer has no address on it, offers no retry', () => {
  for (const code of ['channel_removed', 'no_recipient']) {
    const { unmount } = render(<MessageItem item={{ ...base, kind: 'outgoing', email: null, delivery: { status: 'failed', attempts: 0, last_error: 'Cannot be sent.', sent_at: null, reason_code: code } }} timezone="UTC" />);
    expect(screen.getByText('Not sent')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Retry delivery' })).toBeNull();
    unmount();
  }
});

test('a reply that would fail the same way again shows why and offers no retry', () => {
  render(<MessageItem item={{ ...base, kind: 'outgoing', email: null, delivery: { status: 'failed', attempts: 1, last_error: "moving.gif: GIF isn't supported yet. Send a JPEG or PNG picture instead.", sent_at: null, reason_code: 'zalo_no_files', retryable: false } }} timezone="UTC" />);
  expect(screen.getByText('Delivery failed')).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Retry delivery' })).toBeNull();
});

test('a reply saved before any channel could carry it reads as not sent yet, with Retry for once it is connected', () => {
  render(<MessageItem item={{ ...base, kind: 'outgoing', email: null, delivery: { status: 'failed', attempts: 0, last_error: 'Email is not connected: this reply is saved but not sent. Connect it, then retry.', sent_at: null, reason_code: 'channel_not_connected' } }} timezone="UTC" />);
  expect(screen.getByText('Saved, not sent')).toHaveClass('badge-delivery-queued');
  expect(screen.getByText(/Connect it, then retry/)).toBeVisible();
  expect(screen.getByRole('button', { name: 'Retry delivery' })).toBeInTheDocument();
});

test('a reply waiting because the server\'s Microsoft credentials were refused says why on the timeline, not only in a tooltip', () => {
  const waiting = 'Waiting: Microsoft refused this server\'s app credentials, so whoever runs this server must replace its Microsoft app secret; the reply is sent once it works. Reconnecting does not help.';
  render(<MessageItem item={{ ...base, kind: 'outgoing', email: null, delivery: { status: 'queued', attempts: 0, last_error: waiting, sent_at: null, reason_code: 'mailbox_server_config' } }} timezone="UTC" />);
  expect(screen.getByText(waiting)).toBeVisible();
});

test('a reply waiting because Gmail is switched off on the server says why on the timeline', () => {
  const waiting = 'Gmail sending is turned off on this server: this reply waits and is sent once it is on again.';
  render(<MessageItem item={{ ...base, kind: 'outgoing', email: null, delivery: { status: 'queued', attempts: 0, last_error: waiting, sent_at: null, reason_code: 'mailbox_switched_off' } }} timezone="UTC" />);
  expect(screen.getByText(waiting)).toBeVisible();
});

test('a reply waiting because the server lost a Gmail setting says why on the timeline', () => {
  const waiting = 'Gmail sending is not set up on this server: this reply waits and is sent once whoever runs it sets it up again.';
  render(<MessageItem item={{ ...base, kind: 'outgoing', email: null, delivery: { status: 'queued', attempts: 0, last_error: waiting, sent_at: null, reason_code: 'mailbox_not_configured' } }} timezone="UTC" />);
  expect(screen.getByText(waiting)).toBeVisible();
});

test('the time names its timezone in its title, and on screen when it is not the browser\'s clock', () => {
  render(<MessageItem item={{ ...base, created_at: '2026-10-28T05:42:00Z' }} timezone="Pacific/Kiritimati" />);
  const time = document.querySelector('time') as HTMLElement;
  expect(time).toHaveAttribute('title', 'Oct 28, 2026, 7:42 PM GMT+14');
  expect(time).toHaveTextContent(/7:42\sPM GMT\+14$/);
});
