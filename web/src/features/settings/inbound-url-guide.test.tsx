import { fireEvent, render, screen } from '@testing-library/react';
import { expect, test } from 'vitest';
import { InboundURLGuide } from './inbound-url-guide';

// A reader new to mail servers gets one plain action first; the words of whoever sets it up (forwarder,
// pipe, Email Worker) wait in a help tip they open when they need them.
test('the inbound URL guide leads with one plain action and keeps the technical words in a help tip', () => {
  render(<InboundURLGuide inboundURL="https://desk.example.com/api/v1/channels/email/inbound" supportAddress="help@example.com" />);
  const lead = screen.getByText('Have a forwarder send your mail to the inbound URL.');
  const own = Array.from(lead.childNodes).filter((node) => node.nodeType === Node.TEXT_NODE).map((node) => node.textContent).join('').trim();
  expect(own).not.toMatch(/pipe|posts|Worker|SES|Postmark|header/i);
  expect(own.length).toBeLessThanOrEqual(60);
  const tip = screen.getByRole('button', { name: 'About Inbound URL' });
  expect(tip).toHaveAccessibleDescription(/ask whoever manages your email/i);
  expect(tip).toHaveAccessibleDescription(/not your email password/);
  expect(tip).toHaveAccessibleDescription(/Cloudflare Email Worker, Postmark, Amazon SES, or a pipe on your mail server/);
  fireEvent.click(tip);
  expect(screen.getByRole('link', { name: 'Read how mail reaches the inbound URL' })).toBeInTheDocument();
});
