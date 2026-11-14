import { fireEvent, render, screen } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { CancelDialog, LinkDialog } from './autofix-dialogs';
import { fixOf, withPR } from './autofix.test.fixture';

const cancel = (fix = fixOf()) => render(<CancelDialog fix={fix} busy={false} onCancel={vi.fn()} onClose={vi.fn()} />);

it('Cancel says in one line what happens to the issue and the customer, and keeps the details in a tip', () => {
  cancel(fixOf({ ...withPR }));
  const dialog = screen.getByRole('alertdialog', { name: 'Cancel this fix?' });
  expect(dialog).toHaveTextContent('Colliedesk closes issue #87.');
  expect(dialog).toHaveTextContent('Nothing is sent to the customer. You can retry later.');
  // The run that may go on and the pull request that stays open are said before the person confirms, in the tip of the line.
  expect(screen.getByRole('button', { name: 'About cancelling' })).toHaveAccessibleDescription(
    'If Claude has started, its run may keep going: stop it on GitHub in the Actions tab. The pull request stays open: Colliedesk can’t close pull requests. The text you reviewed is kept with the fix.');
});

it('Cancel names what a fix still sending or sent to Cursor does, with no GitHub advice', () => {
  const { unmount } = cancel(fixOf({ state: 'sending' }));
  expect(screen.getByRole('alertdialog')).toHaveTextContent('Nothing has left Colliedesk yet.');
  expect(screen.getByRole('button', { name: 'About cancelling' })).toHaveAccessibleDescription('The text you reviewed is kept with the fix.');
  unmount();
  cancel(fixOf({ connector: 'cursor_api', issue_number: null, issue_url: null }));
  expect(screen.getByRole('alertdialog')).toHaveTextContent('Colliedesk stops the Cursor run.');
  expect(screen.getByRole('button', { name: 'About cancelling' })).toHaveAccessibleDescription('The text you reviewed is kept with the fix.');
});

it('Link a pull request says in one line what it is for and where it must be', () => {
  render(<LinkDialog fix={fixOf()} busy={false} onLink={vi.fn()} onClose={vi.fn()} />);
  expect(screen.getByRole('dialog', { name: 'Link a pull request' })).toHaveTextContent('Colliedesk did not find it. It must be in northwind/web.');
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
});
