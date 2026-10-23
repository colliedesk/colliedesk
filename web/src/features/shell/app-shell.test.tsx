import { fireEvent, render, screen } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { AppShell } from './app-shell';

const me = {
  agent: { id: '11111111-1111-4111-8111-111111111111', email: 'sam@example.com', display_name: '', role: 'agent' as const, timezone_override: null },
  workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'beta-desk', name: 'Beta Desk', timezone: 'UTC' },
  timezone: 'UTC',
  scope: null,
  plan: null,
  onboarding_goal: null,
};

test('shows the page content next to the rail', () => {
  render(<AppShell me={me}><p>inbox</p></AppShell>);
  expect(screen.getByText('inbox')).toBeInTheDocument();
});

test('a signed-out visitor is told how to get back in instead of seeing an empty workspace', () => {
  render(<AppShell signedOut><p>inbox</p></AppShell>);
  expect(screen.getByRole('alert')).toHaveTextContent('You are signed out. Ask your workspace admin for a new link.');
  expect(screen.queryByText('inbox')).not.toBeInTheDocument();
});

test('a signed-out visitor of a self-hosted instance keeps the pair hint', () => {
  render(<AppShell signedOut><p>inbox</p></AppShell>);
  expect(screen.getByText(/collie pair/)).toBeInTheDocument();
});

test('the workspace and the signed-in address sit in an always-visible bar, not in a tooltip', () => {
  render(<AppShell me={me}><p>inbox</p></AppShell>);
  const bar = screen.getByRole('region', { name: 'Signed in as' });
  expect(bar).toHaveTextContent('Beta Desk');
  expect(bar).toHaveTextContent('sam@example.com');
  expect(bar.closest('[data-tip]')).toBeNull();
});

test('Escape dismisses open tooltips and the next pointer move brings them back', () => {
  render(<AppShell me={me}><p>inbox</p></AppShell>);
  fireEvent.keyDown(document.body, { key: 'Escape' });
  expect(document.documentElement).toHaveAttribute('data-tips-off');
  fireEvent.pointerMove(document.body);
  expect(document.documentElement).not.toHaveAttribute('data-tips-off');
});

test('a signed-out visitor gets no navigation and no account menu that lead nowhere', () => {
  render(<AppShell signedOut links={<a href="/">Inbox</a>}><p>inbox</p></AppShell>);
  expect(screen.queryByRole('link', { name: 'Inbox' })).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Account' })).not.toBeInTheDocument();
});

test('the avatar opens an account menu with who is signed in and Sign out', () => {
  const signOut = vi.fn();
  render(<AppShell me={me} onSignOut={signOut}><p>inbox</p></AppShell>);
  const button = screen.getByRole('button', { name: 'Account' });
  expect(button).toHaveAttribute('aria-expanded', 'false');
  fireEvent.click(button);
  expect(button).toHaveAttribute('aria-expanded', 'true');
  const menu = screen.getByRole('group', { name: 'Account' });
  expect(menu).toHaveTextContent('sam@example.com');
  expect(menu).toHaveTextContent('Beta Desk');
  fireEvent.click(screen.getByRole('button', { name: 'Sign out' }));
  expect(signOut).toHaveBeenCalledTimes(1);
});

test('Escape closes the account menu and returns focus to the avatar', () => {
  render(<AppShell me={me} onSignOut={() => {}}><p>inbox</p></AppShell>);
  const button = screen.getByRole('button', { name: 'Account' });
  fireEvent.click(button);
  fireEvent.keyDown(screen.getByRole('button', { name: 'Sign out' }), { key: 'Escape' });
  expect(screen.queryByRole('button', { name: 'Sign out' })).not.toBeInTheDocument();
  expect(button).toHaveFocus();
});

test('a failed sign out says so in the menu', () => {
  render(<AppShell me={me} onSignOut={() => {}} signOutFailed><p>inbox</p></AppShell>);
  fireEvent.click(screen.getByRole('button', { name: 'Account' }));
  expect(screen.getByRole('alert')).toHaveTextContent('Sign out failed. Try again.');
});

test('the account menu closes when focus moves out of it', () => {
  render(<AppShell me={me} onSignOut={() => {}}><button type="button">elsewhere</button></AppShell>);
  fireEvent.click(screen.getByRole('button', { name: 'Account' }));
  fireEvent.blur(screen.getByRole('button', { name: 'Sign out' }), { relatedTarget: screen.getByRole('button', { name: 'elsewhere' }) });
  expect(screen.queryByRole('button', { name: 'Sign out' })).not.toBeInTheDocument();
});

test('Sign out cannot be sent twice while the first request runs', () => {
  render(<AppShell me={me} onSignOut={() => {}} signingOut><p>inbox</p></AppShell>);
  fireEvent.click(screen.getByRole('button', { name: 'Account' }));
  expect(screen.getByRole('button', { name: 'Sign out' })).toBeDisabled();
});

test('Report a problem in the account menu opens the dialog, and closing it returns focus to the avatar', async () => {
  render(<AppShell me={me} appVersion="0.0.41" onSignOut={() => {}}><p>inbox</p></AppShell>);
  const avatar = screen.getByRole('button', { name: 'Account' });
  fireEvent.click(avatar);
  fireEvent.click(screen.getByRole('button', { name: 'Report a problem' }));
  const dialog = await screen.findByRole('dialog', { name: 'Report a problem' });
  expect(screen.queryByRole('group', { name: 'Account' })).not.toBeInTheDocument();
  fireEvent.click(screen.getByText('What will be sent'));
  expect(dialog).toHaveTextContent('0.0.41');
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  expect(avatar).toHaveFocus();
});
