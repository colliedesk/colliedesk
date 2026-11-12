import { fireEvent, render, screen, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import type { InvitePreview } from '../../api/schemas';
import { InvitePage } from './invite-page';

const invite: InvitePreview = {
  state: 'pending', workspace_name: 'Northwind Support', invited_by: 'Mai Tran', email: 'paul@droptrack.io',
  roles: ['Agent'], expires_at: '2026-11-12T14:30:00Z',
};
const now = new Date('2026-11-12T14:20:00Z');

test('A2-a names the workspace, the inviter, the email, the role and the expiry before the person joins', () => {
  const onJoin = vi.fn();
  render(<InvitePage preview={invite} cloud timeZone="UTC" now={now} onJoin={onJoin} />);
  expect(screen.getByRole('heading', { level: 1, name: 'Join Northwind Support' })).toBeInTheDocument();
  expect(screen.getByText('Mai Tran invited you to the support team.')).toBeInTheDocument();
  const facts = screen.getByRole('list', { name: 'Invite' });
  expect(within(facts).getByText('Your email').nextSibling).toHaveTextContent('paul@droptrack.io');
  expect(within(facts).getByText('Role').nextSibling).toHaveTextContent('Agent');
  expect(within(facts).getByText('Expires').nextSibling).toHaveTextContent('Nov 12, 2:30 PM');
  expect(screen.getByText('No password. Next time, sign in with a code we email to paul@droptrack.io. Not you? Ask Mai for an invite to your own address.')).toBeInTheDocument();
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Join Northwind Support' }));
  expect(onJoin).toHaveBeenCalledOnce();
});

test('A2-a lists every role an invite gives, and says Joining… while it signs in', () => {
  render(<InvitePage preview={{ ...invite, roles: ['Admin', 'Billing'] }} cloud timeZone="UTC" now={now} pending />);
  expect(screen.getByText('Roles').nextSibling).toHaveTextContent('Admin, Billing');
  expect(screen.getByRole('button', { name: 'Joining…' })).toBeDisabled();
});

test('A2-a on a server without cloud sign-in does not promise an emailed code', () => {
  render(<InvitePage preview={invite} cloud={false} timeZone="UTC" now={now} />);
  expect(screen.getByText('No password. This browser stays signed in for 30 days; after that, ask Mai for a new link. Not you? Ask Mai for an invite to your own address.')).toBeInTheDocument();
  expect(screen.queryByText(/code we email/)).not.toBeInTheDocument();
});

test('A2-b says the workspace has no free seat, whom to ask, that the link keeps working, and retries', () => {
  const onJoin = vi.fn();
  render(<InvitePage preview={invite} cloud timeZone="UTC" now={now} planLimited onJoin={onJoin} />);
  expect(screen.getByRole('heading', { level: 1, name: 'Join Northwind Support' })).toBeInTheDocument();
  expect(screen.getByText('Mai Tran invited you to the support team.')).toBeInTheDocument();
  const warning = screen.getByRole('status');
  expect(warning).toHaveTextContent('Northwind Support has used every seat on its plan, so you cannot join yet. Ask Mai Tran to free a seat or change the plan. Your link keeps working until Nov 12, 2:30 PM.');
  expect(warning).toHaveClass('warn-copy');
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  expect(screen.queryByRole('list', { name: 'Invite' })).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
  expect(onJoin).toHaveBeenCalledOnce();
});

test('A2-c says an expired invite expired, when, and whom to ask; Sign in stays a secondary action', () => {
  render(<InvitePage preview={{ ...invite, state: 'expired', expires_at: '2026-11-10T14:30:00Z' }} cloud timeZone="UTC" now={now} />);
  expect(screen.getByRole('heading', { level: 1, name: 'This invite no longer works' })).toBeInTheDocument();
  expect(screen.getByText('Your invite to Northwind Support expired on Nov 10. Ask Mai Tran to send you a new one.')).toBeInTheDocument();
  const signIn = screen.getByRole('link', { name: 'Already joined? Sign in' });
  expect(signIn).toHaveClass('secondary-action');
  expect(signIn).toHaveAttribute('href', '/');
  expect(screen.queryByRole('button', { name: /join/i })).not.toBeInTheDocument();
});

test('A2-c says a revoked invite was cancelled and whom to ask', () => {
  render(<InvitePage preview={{ ...invite, state: 'revoked' }} cloud timeZone="UTC" now={now} />);
  expect(screen.getByRole('heading', { level: 1, name: 'This invite no longer works' })).toBeInTheDocument();
  expect(screen.getByText('This invite to Northwind Support was cancelled. Ask Mai Tran if you still need access.')).toBeInTheDocument();
  expect(screen.getByRole('link', { name: 'Already joined? Sign in' })).toHaveClass('secondary-action');
});

test('A2-c makes Sign in the primary action for an invite already used', () => {
  render(<InvitePage preview={{ ...invite, state: 'used' }} cloud timeZone="UTC" now={now} />);
  expect(screen.getByRole('heading', { level: 1, name: 'This invite no longer works' })).toBeInTheDocument();
  expect(screen.getByText('This invite was used to join Northwind Support. Sign in with your email instead.')).toBeInTheDocument();
  const signIn = screen.getByRole('link', { name: 'Sign in' });
  expect(signIn).toHaveClass('primary-action');
  expect(signIn).toHaveAttribute('href', '/');
});

test('A2-c for a used invite without cloud sign-in says to ask the inviter for a new link', () => {
  render(<InvitePage preview={{ ...invite, state: 'used' }} cloud={false} timeZone="UTC" now={now} />);
  expect(screen.getByText('This invite was used to join Northwind Support. Sign in from a browser that joined, or ask Mai Tran for a new link.')).toBeInTheDocument();
});

test('a pending invite that redeem refuses for another reason shows the generic link error', () => {
  render(<InvitePage preview={invite} cloud timeZone="UTC" now={now} failed />);
  expect(screen.getByRole('alert')).toHaveTextContent('This link is invalid, expired, or already used. Ask your workspace admin for a new link.');
});
