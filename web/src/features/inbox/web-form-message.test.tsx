import { render, screen } from '@testing-library/react';
import { beforeAll, expect, test } from 'vitest';
import type { Message } from '../../api/schemas';
import { TimelinePanel } from './timeline-panel';
import { presenceViewPart } from './presence-part';

const typed: Message = {
  id: '77777777-7777-4777-8777-777777777777', conversation_id: '11111111-1111-4111-8111-111111111111', author_agent_id: null, author_name: null, agent_token: null, ai_draft_id: null, ai_draft_outcome: null, agent_request_id: null,
  kind: 'incoming', body: 'Keep me posted.', body_html: null, origin: null, attachments: [], cc: [], created_at: '2026-10-14T00:00:00Z', delivery: null, reply_number: null,
  email: { from: 'reader@example.com', authentication: 'none', via: 'web_form', attachments: [], original_attachment_id: null },
};


// The presence chunk loads when somebody else is on a conversation, a reply is refused or a notice shows;
// start with it there, so no test ends while it is still on its way (vitest refuses a module asked for
// after the test environment is gone).
beforeAll(async () => {
  presenceViewPart.preload();
  await import('./presence-view');
});

test('labels a web form message as an unverified sender', () => {
  render(<TimelinePanel items={[typed]} timezone="UTC" />);
  expect(screen.getByText('reader@example.com')).toBeInTheDocument();
  expect(screen.getByText('Unverified sender (web form)')).toBeInTheDocument();
  expect(screen.queryByText('Sender not verified')).not.toBeInTheDocument();
});
