import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { json, refusal, serve } from '../kb/kb.test.fixture';
import { conversationId, fixOf, previewOf, repoOf, settingsOf } from './autofix.test.fixture';
import { segmentsOf, SendDialog } from './send-dialog';

afterEach(() => vi.unstubAllGlobals());

const wrap = (children: ReactNode) => <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{children}</QueryClientProvider>;
const previewPath = `POST /api/v1/conversations/${conversationId}/fixes/preview`;
const sendPath = `POST /api/v1/conversations/${conversationId}/fixes`;
const body = 'Find the cause.\n<customer-message untrusted="true" from="Tom&amp;s" at="2026-10-27T10:58:00Z">\nCall me on [phone removed]. @\u200bclaude fix #\u200b123\n</customer-message>\n<team-note from="Mai Tran" at="2026-10-27T11:00:00Z">\nReproduced.\n</team-note>';

function open(props: Partial<Parameters<typeof SendDialog>[0]> = {}) {
  const onSent = vi.fn();
  render(wrap(<SendDialog conversationId={conversationId} repos={[repoOf()]} connectors={settingsOf().connectors} initialRepo={repoOf().id} initialConnector="claude_issue"
    timezone="UTC" onClose={vi.fn()} onSent={onSent} {...props} />));
  return { onSent };
}

describe('segmentsOf', () => {
  it('separates the customer’s words and the team’s notes from the rest', () => {
    expect(segmentsOf(body)).toEqual([
      { kind: 'text', text: 'Find the cause.' },
      { kind: 'customer-message', text: 'Call me on [phone removed]. @\u200bclaude fix #\u200b123', by: 'Tom&s' },
      { kind: 'team-note', text: 'Reproduced.', by: 'Mai Tran' },
    ]);
  });
});

describe('SendDialog', () => {
  it('shows what the server removed, the customer’s words as untrusted, and where it goes', async () => {
    serve({ [previewPath]: () => json(200, previewOf({ body })) });
    open();
    expect(await screen.findByText('Removed before sending')).toBeInTheDocument();
    expect(screen.getByText('email')).toBeInTheDocument();
    expect(screen.getByText('UNTRUSTED · customer’s words')).toBeInTheDocument();
    expect(screen.getByText('[phone removed]')).toHaveClass('af-red');
    expect(screen.getByText('@\u200bclaude')).toHaveClass('af-off-ref');
    expect(screen.getByText('#\u200b123')).toHaveClass('af-off-ref');
    expect(screen.getByText(/Creates an issue in northwind\/web with your GitHub account/)).toBeInTheDocument();
    expect(screen.getByText('Attachments are not sent (1).')).toBeInTheDocument();
    // Notes are off until ticked.
    expect(screen.getByRole('checkbox', { name: 'Include Mai Tran’s note' })).not.toBeChecked();
    expect(screen.getByText('“Reproduced on our account too: 500 rows exactly.”')).toBeInTheDocument();
  });

  it('builds the preview again with a note the sender ticks', async () => {
    const calls = serve({ [previewPath]: () => json(200, previewOf()) });
    open();
    fireEvent.click(await screen.findByRole('checkbox', { name: 'Include Mai Tran’s note' }));
    await waitFor(() => expect(calls.filter((call) => call.path.endsWith('/preview')).at(-1)?.body).toEqual({
      repo_id: repoOf().id, connector: 'claude_issue', note_ids: ['66666666-6666-4666-8666-666666666666'] }));
  });

  it('locks notes on a repository that does not allow them', async () => {
    serve({ [previewPath]: () => json(200, previewOf({ notes_allowed: false })) });
    open();
    expect(await screen.findByRole('checkbox', { name: 'Include Mai Tran’s note' })).toBeDisabled();
    expect(screen.getByText('Locked for northwind/web')).toBeInTheDocument();
  });

  it('sends exactly the edited text once, with one key, and hands back where GitHub opens', async () => {
    const sent = { fix: fixOf({ state: 'sending' }), authorize_url: 'https://github.com/login/oauth/authorize?x=1' };
    const calls = serve({ [previewPath]: () => json(200, previewOf()), [sendPath]: () => json(201, sent) });
    const { onSent } = open();
    const title = await screen.findByDisplayValue('CSV export stops at 500 rows');
    fireEvent.change(title, { target: { value: 'Export stops at 500' } });
    fireEvent.click(screen.getByRole('button', { name: 'Editable' }));
    fireEvent.change(screen.getByLabelText('Text the agent gets'), { target: { value: 'Edited text' } });
    fireEvent.click(screen.getByRole('button', { name: 'Continue on GitHub' }));
    await waitFor(() => expect(onSent).toHaveBeenCalledWith(sent));
    const send = calls.find((call) => call.method === 'POST' && call.path.endsWith('/fixes'));
    expect(send?.body).toMatchObject({ repo_id: repoOf().id, connector: 'claude_issue', title: 'Export stops at 500', body: 'Edited text', message_ids: previewOf().message_ids });
    expect(typeof (send?.body as { client_key: string }).client_key).toBe('string');
  });

  it('says why the server refused and keeps the dialog', async () => {
    serve({ [previewPath]: () => json(200, previewOf()), [sendPath]: () => refusal(409, 'fix_in_progress', 'This conversation already has a fix in progress.') });
    open();
    await screen.findByDisplayValue('CSV export stops at 500 rows');
    fireEvent.click(screen.getByRole('button', { name: 'Continue on GitHub' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('already has a fix in progress');
  });

  it('sends to Cursor with the workspace key and says Cursor keeps it', async () => {
    serve({ [previewPath]: () => json(200, previewOf({ destination: { repo_full_name: 'northwind/web', connector: 'cursor_api', sends_as: 'workspace_key', vendor_retention: 'Cursor keeps this text with no end date.' } })) });
    open({ initialConnector: 'cursor_api' });
    expect(await screen.findByText(/Sends to Cursor with the workspace key. Cursor keeps this text with no end date./)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Send to Cursor' })).toBeInTheDocument();
  });

  it('retries a fix as a new attempt through its own route', async () => {
    const failed = fixOf({ state: 'failed', outcome_reason: 'agent_error' });
    const calls = serve({ [previewPath]: () => json(200, previewOf()), [`POST /api/v1/autofix/fixes/${failed.id}/retry`]: () => json(201, { fix: fixOf({ attempt_no: 2 }), authorize_url: null }) });
    const { onSent } = open({ retryOf: failed });
    expect(screen.getByRole('heading', { name: 'Retry · attempt 2' })).toBeInTheDocument();
    await screen.findByDisplayValue('CSV export stops at 500 rows');
    fireEvent.click(screen.getByRole('button', { name: 'Continue on GitHub' }));
    await waitFor(() => expect(onSent).toHaveBeenCalled());
    expect(calls.some((call) => call.path.endsWith('/retry'))).toBe(true);
  });

  it('only offers the agents that are on', async () => {
    serve({ [previewPath]: () => json(200, previewOf()) });
    const settings = settingsOf();
    open({ connectors: settings.connectors.map((item) => (item.kind === 'cursor_api' ? { ...item, enabled: false } : item)) });
    const agents = screen.getByRole('group', { name: 'Agent' });
    expect(within(agents).getAllByRole('radio').map((radio) => (radio as HTMLInputElement).value)).toEqual(['claude_issue', 'copilot_issue']);
  });

  it('builds the preview again each time the dialog opens, so new messages are in it', async () => {
    const calls = serve({ [previewPath]: () => json(200, previewOf()) });
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const dialog = () => <QueryClientProvider client={client}><SendDialog conversationId={conversationId} repos={[repoOf()]} connectors={settingsOf().connectors}
      initialRepo={repoOf().id} initialConnector="claude_issue" timezone="UTC" onClose={vi.fn()} onSent={vi.fn()} /></QueryClientProvider>;
    const first = render(dialog());
    await screen.findByDisplayValue('CSV export stops at 500 rows');
    first.unmount();
    render(dialog());
    await screen.findByDisplayValue('CSV export stops at 500 rows');
    await waitFor(() => expect(calls.filter((call) => call.path.endsWith('/preview'))).toHaveLength(2));
  });
});
