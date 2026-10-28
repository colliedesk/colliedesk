import { render, screen } from '@testing-library/react';
import { expect, test } from 'vitest';
import { InboundURLGuide } from './inbound-url-guide';

// A reader new to mail servers gets one plain action first; the words of whoever sets it up (forwarder,
// pipe, Email Worker) stay in a detail they open when they need them.
test('the inbound URL guide leads with one plain action and keeps the technical words in a detail', () => {
  render(<InboundURLGuide inboundURL="https://desk.example.com/api/v1/channels/email/inbound" supportAddress="help@example.com" />);
  const lead = screen.getByText(/ask whoever manages your email/i);
  expect(lead.textContent).not.toMatch(/forwarder|pipe|posts|Worker|SES|Postmark/i);
  expect(lead).toHaveTextContent(/not your email password/);
  const details = screen.getByText('Technical details').closest('details') as HTMLElement;
  expect(details).not.toHaveAttribute('open');
  expect(details).toHaveTextContent(/Cloudflare Email Worker, Postmark, Amazon SES, or a pipe on your mail server/);
  expect(screen.getByRole('link', { name: 'Read how mail reaches the inbound URL' })).toBeInTheDocument();
});
