import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render as renderPlain, screen, within } from '@testing-library/react';
import type { ReactElement, ReactNode } from 'react';
import { afterEach, expect, test, vi } from 'vitest';
import { SetupChecklist } from './setup-checklist';
import { quickStartCount } from './setup-steps';
import type { SetupProgress } from '../../api/schemas';
import { permissionsOf } from '../../test-roles';

// Who looks at the checklist: the built-in roles' permissions decide which steps they take.
const adminMe = { permissions: permissionsOf('admin') };
const agentMe = { permissions: permissionsOf('agent') };

const client = new QueryClient();
const render = (ui: ReactElement) => renderPlain(ui, { wrapper: ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider> });

const tracked = vi.hoisted(() => vi.fn());
vi.mock('../../lib/product-analytics', () => ({ track: tracked }));

afterEach(() => { tracked.mockReset(); });

// The setup as the server gives it: AI drafts and reply targets between the import and the invites.
const sixSteps = ['email', 'import', 'ai_drafts', 'reply_targets', 'invite', 'first_reply'];
function progress(states: Record<string, string> = {}, order = sixSteps, recommended: string | null = null): SetupProgress {
  const all = { email: 'none', import: 'none', ai_drafts: 'none', reply_targets: 'none', invite: 'alone', first_reply: 'none', ...states } as Record<string, string>;
  return { steps: order.map((key) => ({ key, state: all[key], detail: null })), complete: false, recommended } as unknown as SetupProgress;
}

// The rows of one group (Set up, Try more), by their step keys.
const keysOf = (name: string) => within(screen.getByRole('region', { name })).queryAllByRole('listitem').map((row) => row.getAttribute('data-step'));
const row = (key: string) => document.querySelector(`[data-step="${key}"]`) as HTMLElement;

test('Set up holds email, the import and the first reply; Try more the invite, AI drafts, reply targets and an AI agent', () => {
  render(<SetupChecklist progress={progress({ email: 'waiting' })} me={adminMe} workspaceName="Beta Desk" onSkip={vi.fn()} />);
  expect(screen.getByRole('heading', { level: 2, name: 'Get Beta Desk ready' })).toBeInTheDocument();
  expect(keysOf('Set up')).toEqual(['email', 'import', 'first_reply']);
  expect(keysOf('Try more')).toEqual(['ai_drafts', 'reply_targets', 'invite', 'mcp']);
  expect(screen.getByText('0 of 6')).toBeInTheDocument();
  expect(row('email')).toHaveTextContent('Waiting for the first email');
  expect(row('import')).toHaveTextContent('Moves your history over');
  expect(row('invite')).toHaveTextContent('Just you so far');
  expect(row('first_reply')).toHaveTextContent('Not yet');
  expect(row('mcp')).toHaveTextContent('Let Claude or Codex work the queue');
});

test('finished steps are ticked and struck through, and the bar has one segment per step, the done ones first', () => {
  render(<SetupChecklist progress={progress({ email: 'working', import: 'done' })} me={adminMe} workspaceName="Beta Desk" onSkip={vi.fn()} />);
  expect(screen.getByText('2 of 6')).toBeInTheDocument();
  const bar = screen.getByRole('progressbar', { name: 'Setup progress' });
  expect(bar).toHaveAttribute('aria-valuenow', '2');
  expect(bar).toHaveAttribute('aria-valuemax', '6');
  expect([...bar.children].map((segment) => segment.getAttribute('data-done'))).toEqual(['true', 'true', null, null, null, null]);
  expect(row('email')).toHaveAttribute('data-state', 'done');
  expect(row('email').querySelector('.setup-mark svg')).not.toBeNull();
  expect(row('email')).toHaveTextContent('Done');
  expect(row('import').querySelector('.setup-mark svg')).not.toBeNull();
  expect(row('first_reply').querySelector('.setup-mark svg')).toBeNull();
});

test('each open step shows its minutes; a done one does not', () => {
  render(<SetupChecklist progress={progress({ email: 'working' })} me={adminMe} workspaceName="Beta Desk" onSkip={vi.fn()} />);
  expect(row('email').querySelector('.setup-time')).toBeNull();
  expect(row('import')).toHaveTextContent('10 min');
  expect(row('first_reply')).toHaveTextContent('2 min');
  expect(row('invite')).toHaveTextContent('1 min');
  expect(row('mcp')).toHaveTextContent('3 min');
});

test('every step links to its screen of the setup flow, with plain links', () => {
  render(<SetupChecklist progress={progress()} me={adminMe} workspaceName="Beta Desk" onSkip={vi.fn()} />);
  expect(screen.getByRole('link', { name: /Receive customer email/ })).toHaveAttribute('href', '/setup/email');
  expect(screen.getByRole('link', { name: /Import from Freshdesk/ })).toHaveAttribute('href', '/setup/import');
  expect(screen.getByRole('link', { name: /Invite your team/ })).toHaveAttribute('href', '/setup/invite');
  expect(screen.getByRole('link', { name: /Answer your first email/ })).toHaveAttribute('href', '/setup/first_reply');
  expect(screen.getByRole('link', { name: /Connect an AI agent/ })).toHaveAttribute('href', 'https://colliedesk.com/docs/mcp');
});

test('only the next step to do is highlighted, Set up first', () => {
  render(<SetupChecklist progress={progress({ email: 'working' })} me={adminMe} workspaceName="Beta Desk" onSkip={vi.fn()} />);
  const links = screen.getAllByRole('link');
  expect(links.filter((link) => link.classList.contains('primary-action')).map((link) => link.textContent)).toEqual([expect.stringContaining('Import from Freshdesk')]);
});

test('an address waiting for its first email is still the highlighted step, and the empty inbox offers the same address', () => {
  render(<SetupChecklist progress={progress({ email: 'waiting' })} me={adminMe} workspaceName="Beta Desk" onSkip={vi.fn()} />);
  const highlighted = screen.getAllByRole('link').filter((link) => link.classList.contains('primary-action'));
  expect(highlighted).toHaveLength(1);
  expect(highlighted[0]).toHaveAttribute('href', '/setup/email');
});

test('with Set up done, the next step is the first of Try more', () => {
  render(<SetupChecklist progress={progress({ email: 'working', import: 'done', first_reply: 'sent' })} me={adminMe} workspaceName="Beta Desk" onSkip={vi.fn()} />);
  expect(screen.getAllByRole('link').filter((link) => link.classList.contains('primary-action')).map((link) => link.textContent)).toEqual([expect.stringContaining('Turn on AI drafts')]);
});

test('the recommended step from the sign-up goal is marked and listed first', () => {
  render(<SetupChecklist progress={progress({}, ['import', 'email', 'ai_drafts', 'reply_targets', 'invite', 'first_reply'], 'import')} me={adminMe} workspaceName="Beta Desk" onSkip={vi.fn()} />);
  expect(keysOf('Set up')[0]).toBe('import');
  expect(row('import')).toHaveTextContent('Recommended');
  expect(row('email')).not.toHaveTextContent('Recommended');
});

test('a goal of inviting the team puts the invite first in Try more', () => {
  render(<SetupChecklist progress={progress({}, ['invite', 'email', 'import', 'ai_drafts', 'reply_targets', 'first_reply'], 'invite')} me={adminMe} workspaceName="Beta Desk" onSkip={vi.fn()} />);
  expect(keysOf('Try more')[0]).toBe('invite');
  expect(row('invite')).toHaveTextContent('Recommended');
});

test('an agent who is not an admin sees only the steps they can take', () => {
  render(<SetupChecklist progress={progress()} me={agentMe} workspaceName="Beta Desk" onSkip={vi.fn()} />);
  expect(keysOf('Set up')).toEqual(['first_reply']);
  expect(keysOf('Try more')).toEqual(['mcp']);
  expect(screen.getByText('0 of 1')).toBeInTheDocument();
});

test('the goal ai_agents puts the AI agent guide first in Try more, and it is never counted', () => {
  render(<SetupChecklist progress={progress({ email: 'working' })} me={adminMe} goal="ai_agents" workspaceName="Beta Desk" onSkip={vi.fn()} />);
  expect(keysOf('Try more')[0]).toBe('mcp');
  expect(screen.getByText('1 of 6')).toBeInTheDocument();
});

test('Skip these asks to skip Try more; skipped, the group keeps its heading and offers Show these', () => {
  const onSkip = vi.fn();
  const { rerender } = render(<SetupChecklist progress={progress()} me={adminMe} workspaceName="Beta Desk" onSkip={onSkip} />);
  fireEvent.click(screen.getByRole('button', { name: 'Skip these' }));
  expect(onSkip).toHaveBeenLastCalledWith(true);
  rerender(<SetupChecklist progress={progress()} me={adminMe} workspaceName="Beta Desk" skipped onSkip={onSkip} />);
  expect(keysOf('Try more')).toEqual([]);
  expect(screen.getByText('0 of 3')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Show these' }));
  expect(onSkip).toHaveBeenLastCalledWith(false);
});

test('selecting a step reports it started, with the step key only', () => {
  render(<SetupChecklist progress={progress()} me={adminMe} workspaceName="Beta Desk" onSkip={vi.fn()} />);
  fireEvent.click(screen.getByRole('link', { name: /Invite your team/ }));
  expect(tracked).toHaveBeenCalledWith('onboarding_step_started', { step: 'invite' });
});

test('a step that turns done while the checklist is on screen is reported once, and the first render reports nothing', () => {
  const { rerender } = render(<SetupChecklist progress={progress({ email: 'waiting' })} me={adminMe} workspaceName="Beta Desk" onSkip={vi.fn()} />);
  expect(tracked).not.toHaveBeenCalled();
  rerender(<SetupChecklist progress={progress({ email: 'working' })} me={adminMe} workspaceName="Beta Desk" onSkip={vi.fn()} />);
  expect(tracked).toHaveBeenCalledTimes(1);
  expect(tracked).toHaveBeenCalledWith('onboarding_step_completed', { step: 'email' });
  rerender(<SetupChecklist progress={progress({ email: 'working' })} me={adminMe} workspaceName="Beta Desk" onSkip={vi.fn()} />);
  expect(tracked).toHaveBeenCalledTimes(1);
});

test('AI drafts and reply targets link to their screens and read Done once they are on', () => {
  render(<SetupChecklist progress={progress({ ai_drafts: 'nothing_to_read', reply_targets: 'on' })} me={adminMe} workspaceName="Beta Desk" onSkip={vi.fn()} />);
  expect(screen.getByRole('link', { name: /Turn on AI drafts/ })).toHaveAttribute('href', '/setup/ai_drafts');
  expect(screen.getByRole('link', { name: /Set reply targets/ })).toHaveAttribute('href', '/setup/reply_targets');
  expect(row('ai_drafts')).toHaveTextContent('Better after an import or an article');
  expect(row('reply_targets')).toHaveTextContent('Done');
  expect(row('reply_targets')).toHaveAttribute('data-state', 'done');
});

test('without built-in AI the drafts row has nothing to do and is not counted', () => {
  render(<SetupChecklist progress={progress({ email: 'working', import: 'done', ai_drafts: 'unavailable', reply_targets: 'on' })} me={adminMe} workspaceName="Beta Desk" onSkip={vi.fn()} />);
  expect(row('ai_drafts')).toHaveTextContent('Nothing to do: no built-in AI on this server');
  expect(row('ai_drafts')).toHaveAttribute('data-state', 'none');
  expect(row('ai_drafts').querySelector('.setup-time')).toBeNull();
  expect(screen.getByText('3 of 5')).toBeInTheDocument();
});

test('the count leaves out steps with nothing to do and, once skipped, Try more; the dot follows Set up only', () => {
  expect(quickStartCount(progress({ ai_drafts: 'unavailable' }), adminMe, false)).toEqual({ done: 0, total: 5, left: 5, setUpLeft: true });
  expect(quickStartCount(progress({ email: 'working', import: 'done', first_reply: 'sent' }), adminMe, false)).toEqual({ done: 3, total: 6, left: 3, setUpLeft: false });
  expect(quickStartCount(progress({ email: 'working', import: 'done', first_reply: 'sent' }), adminMe, true)).toEqual({ done: 3, total: 3, left: 0, setUpLeft: false });
});

// Each offer follows the permission its own call needs: turning drafts on is ai_settings:update, saving
// targets workspace:update; managing channels gives neither.
test('a person sees AI drafts only with ai_settings:update and ai_settings:read, and reply targets only with workspace:update', () => {
  const without = (name: string) => ({ permissions: permissionsOf('admin').filter((item) => item !== name) });
  const { unmount } = render(<SetupChecklist progress={progress()} me={without('ai_settings:update')} workspaceName="Beta Desk" onSkip={vi.fn()} />);
  expect(keysOf('Try more')).toEqual(['reply_targets', 'invite', 'mcp']);
  unmount();
  const again = render(<SetupChecklist progress={progress()} me={without('workspace:update')} workspaceName="Beta Desk" onSkip={vi.fn()} />);
  expect(keysOf('Try more')).toEqual(['ai_drafts', 'invite', 'mcp']);
  again.unmount();
  render(<SetupChecklist progress={progress()} me={without('ai_settings:read')} workspaceName="Beta Desk" onSkip={vi.fn()} />);
  expect(keysOf('Try more')).toEqual(['reply_targets', 'invite', 'mcp']);
});

test('an agent whose role may change AI settings and targets, but not channels, gets both offers', () => {
  render(<SetupChecklist progress={progress()} me={{ permissions: [...permissionsOf('agent'), 'ai_settings:update', 'workspace:update'] }} workspaceName="Beta Desk" onSkip={vi.fn()} />);
  expect(keysOf('Set up')).toEqual(['first_reply']);
  expect(keysOf('Try more')).toEqual(['ai_drafts', 'reply_targets', 'mcp']);
});
