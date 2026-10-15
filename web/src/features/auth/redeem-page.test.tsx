import { fireEvent, render, screen } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { RedeemPage } from './redeem-page';

test('explains passwordless access without rendering a login form', () => {
	render(<RedeemPage token="" cloudEnabled={false} />);
	expect(screen.getByRole('heading', { name: /open your secure link/i })).toBeInTheDocument();
	expect(screen.getByText(/ask your workspace admin/i)).toBeInTheDocument();
	expect(screen.queryByLabelText(/password/i)).not.toBeInTheDocument();
	expect(screen.queryByText(/^sign in$/i)).not.toBeInTheDocument();
});

test('shows cloud sign-in only when public config enables it', () => {
	render(<RedeemPage token="" cloudEnabled cloudSignIn={<button type="button">Sign in</button>} />);
	expect(screen.getByRole('button', { name: /^sign in$/i })).toBeInTheDocument();
	expect(screen.getByText(/one-time link from your admin/i)).toBeInTheDocument();
});

test('cloud sign-in is titled as a sign-in with an emailed code, not as a link to open', () => {
	render(<RedeemPage token="" cloudEnabled cloudSignIn={<div>form</div>} />);
	expect(screen.getByRole('heading', { level: 1, name: 'Sign in to Colliedesk' })).toBeInTheDocument();
	expect(screen.getByText('Enter your work email and we will send you a one-time code.')).toBeInTheDocument();
	expect(screen.getByText('Got a one-time link from your admin? Open it in this browser instead.')).toBeInTheDocument();
	expect(screen.queryByText(/open your secure link/i)).not.toBeInTheDocument();
	expect(screen.queryByText(/one-time links instead of passwords/i)).not.toBeInTheDocument();
});

test('opening a link keeps the link wording even when cloud sign-in is on', () => {
	render(<RedeemPage token="abc" cloudEnabled cloudSignIn={<div>form</div>} />);
	expect(screen.getByRole('heading', { level: 1, name: /open your secure link/i })).toBeInTheDocument();
});

test('uses a cloud-specific failure message', () => {
	render(<RedeemPage token="" cloudEnabled failed cloudSignIn={<div>Sign in</div>} />);
	expect(screen.getByRole('alert')).toHaveTextContent(/cloud sign-in failed/i);
});

test('lets a cloud identity choose one of its workspaces', () => {
	const onSelectWorkspace = vi.fn();
	render(<RedeemPage token="" cloudEnabled failed workspaces={[
		{ slug: 'alpha', name: 'Alpha Support' },
		{ slug: 'beta', name: 'Beta Support' },
	]} onSelectWorkspace={onSelectWorkspace} />);

	fireEvent.click(screen.getByRole('button', { name: /beta support/i }));
	expect(onSelectWorkspace).toHaveBeenCalledWith('beta');
	expect(screen.queryByRole('alert')).not.toBeInTheDocument();
});

test('a refused link says what happened and what to do next', () => {
	render(<RedeemPage token="spent" failed />);
	expect(screen.getByRole('alert')).toHaveTextContent('This link is invalid, expired, or already used. Ask your workspace admin for a new link.');
});

test('a link refused by the plan says the link is fine and the workspace is full', () => {
	render(<RedeemPage token="valid" failed planLimited />);
	expect(screen.getByRole('alert')).toHaveTextContent(/agent limit/i);
	expect(screen.getByRole('alert')).toHaveTextContent(/ask a workspace admin to upgrade/i);
	expect(screen.getByRole('alert')).toHaveTextContent(/use this link again/i);
	expect(screen.getByRole('alert')).not.toHaveTextContent(/invalid/i);
});
