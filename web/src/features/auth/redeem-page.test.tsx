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
	expect(screen.getByText(/one-time link instead/i)).toBeInTheDocument();
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
