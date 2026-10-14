import { render, screen, within } from '@testing-library/react';
import { expect, test } from 'vitest';
import { AppShell } from './app-shell';

const me = {
  agent: { id: '11111111-1111-4111-8111-111111111111', email: 'sam@example.com', display_name: '', role: 'agent' as const, timezone_override: null },
  workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'beta-desk', name: 'Beta Desk', timezone: 'UTC' },
  timezone: 'UTC',
};

test('names the workspace and the signed-in person in the main navigation', () => {
  render(<AppShell me={me}><p>inbox</p></AppShell>);
  const nav = screen.getAllByRole('navigation')[0];
  expect(within(nav).getByText('Beta Desk')).toBeInTheDocument();
  expect(within(nav).getByText('sam@example.com')).toBeInTheDocument();
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
