import { fireEvent, render, screen } from '@testing-library/react';
import { expect, test } from 'vitest';
import { AppShell } from './app-shell';

const me = {
  agent: { id: '11111111-1111-4111-8111-111111111111', email: 'sam@example.com', display_name: '', role: 'agent' as const, timezone_override: null },
  workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'beta-desk', name: 'Beta Desk', timezone: 'UTC' },
  timezone: 'UTC',
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

test('a signed-out visitor of a cloud instance gets a sign-in link instead of the self-host hint', () => {
  render(<AppShell signedOut signIn={<a href="/redeem">Sign in</a>}><p>inbox</p></AppShell>);
  expect(screen.getByRole('link', { name: 'Sign in' })).toHaveAttribute('href', '/redeem');
  expect(screen.queryByText(/colliedesk pair/)).not.toBeInTheDocument();
});

test('a signed-out visitor of a self-hosted instance keeps the pair hint', () => {
  render(<AppShell signedOut><p>inbox</p></AppShell>);
  expect(screen.getByText(/colliedesk pair/)).toBeInTheDocument();
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
