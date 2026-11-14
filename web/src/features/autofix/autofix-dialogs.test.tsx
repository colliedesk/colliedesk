import { render, screen } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { CancelDialog, LinkDialog } from './autofix-dialogs';
import { fixOf, withPR } from './autofix.test.fixture';

const cancel = (fix = fixOf()) => render(<CancelDialog fix={fix} busy={false} onCancel={vi.fn()} onClose={vi.fn()} />);

it('Cancel says before the person presses it what stays on GitHub, in one visible line, and keeps the rest in a tip', () => {
  cancel(fixOf({ ...withPR }));
  const dialog = screen.getByRole('alertdialog', { name: 'Cancel this fix?' });
  expect(dialog).toHaveTextContent('Colliedesk closes issue #87.');
  expect(dialog).toHaveTextContent('Nothing is sent to the customer. You can retry later.');
  const consequence = dialog.querySelector('[data-quiet="consequence"]');
  expect(consequence).toHaveTextContent('If Claude has started, its run may keep going: stop it on GitHub in the Actions tab. The pull request stays open: Colliedesk can’t close pull requests.');
  expect(screen.getByRole('button', { name: 'About cancelling' })).toHaveAccessibleDescription('The text you reviewed is kept with the fix.');
});

it('Cancel names only the consequence that applies: no pull request, no run to stop for Cursor or a fix still sending', () => {
  const { unmount } = cancel(fixOf());
  expect(screen.getByRole('alertdialog').querySelector('[data-quiet="consequence"]')).toHaveTextContent(/^If Claude has started, its run may keep going: stop it on GitHub in the Actions tab\.$/);
  unmount();
  const sending = cancel(fixOf({ state: 'sending' }));
  expect(screen.getByRole('alertdialog')).toHaveTextContent('Nothing has left Colliedesk yet.');
  expect(screen.getByRole('alertdialog').querySelector('[data-quiet="consequence"]')).toBeNull();
  sending.unmount();
  cancel(fixOf({ connector: 'cursor_api', issue_number: null, issue_url: null }));
  expect(screen.getByRole('alertdialog')).toHaveTextContent('Colliedesk stops the Cursor run.');
  expect(screen.getByRole('alertdialog').querySelector('[data-quiet="consequence"]')).toBeNull();
});

it('Link a pull request says where the pull request must be, true whether Colliedesk found one or not', () => {
  render(<LinkDialog fix={fixOf()} busy={false} onLink={vi.fn()} onClose={vi.fn()} />);
  expect(screen.getByRole('dialog', { name: 'Link a pull request' })).toHaveTextContent('It must be a pull request in northwind/web.');
});
