import { render, screen } from '@testing-library/react';
import { expect, test } from 'vitest';
import type { Message } from '../../api/schemas';
import { TimelinePanel } from './timeline-panel';

const typed: Message = {
  id: '77777777-7777-4777-8777-777777777777', conversation_id: '11111111-1111-4111-8111-111111111111', author_agent_id: null, author_name: null,
  kind: 'incoming', body: 'Keep me posted.', body_html: null, origin: null, attachments: [], created_at: '2026-10-14T00:00:00Z', delivery: null,
  email: { from: 'reader@example.com', authentication: 'none', via: 'web_form', attachments: [], original_attachment_id: null },
};

test('labels a web form message as an unverified sender', () => {
  render(<TimelinePanel items={[typed]} timezone="UTC" />);
  expect(screen.getByText('reader@example.com')).toBeInTheDocument();
  expect(screen.getByText('Unverified sender (web form)')).toBeInTheDocument();
  expect(screen.queryByText('Sender not verified')).not.toBeInTheDocument();
});
