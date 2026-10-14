import { render, screen } from '@testing-library/react';
import { expect, test } from 'vitest';
import { RedeemPage } from './redeem-page';

test('explains passwordless access without rendering a login form', () => {
  render(<RedeemPage token="" />);
  expect(screen.getByRole('heading', { name: /open your secure link/i })).toBeInTheDocument();
  expect(screen.getByText(/ask your workspace admin/i)).toBeInTheDocument();
  expect(screen.queryByLabelText(/password/i)).not.toBeInTheDocument();
});
