import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { MigrationDialog } from './migration-dialog';

const receipt = { plan: 'founding' as const, summary: 'Founding · $99 every 3 months · price locked for life', nextCharge: 'Next charge: $99 on Jan 28, 2027' };
type Props = Partial<Parameters<typeof MigrationDialog>[0]>;
function open(props: Props = {}) {
  const onSend = vi.fn().mockResolvedValue({ freshdesk_address: 'northwind.freshdesk.com', times: 'Tue after 2 pm', requested_at: '2026-10-30T09:00:00Z' });
  const onDismiss = vi.fn();
  render(<MigrationDialog welcome={receipt} onSend={onSend} onDismiss={onDismiss} {...props} />);
  return { onSend, onDismiss, dialog: screen.getByRole('dialog') };
}
const fill = (address: string, times = '') => {
  fireEvent.change(screen.getByLabelText('Freshdesk address'), { target: { value: address } });
  if (times) fireEvent.change(screen.getByLabelText(/times that suit you/i), { target: { value: times } });
};

test('the welcome of a Founding buyer shows the receipt and exactly one job: send the Freshdesk address', () => {
  const { dialog } = open();
  expect(within(dialog).getByRole('heading', { name: 'Welcome to Founding' })).toBeInTheDocument();
  expect(within(dialog).getByText('Founding · $99 every 3 months · price locked for life')).toBeInTheDocument();
  expect(within(dialog).getByText('Next charge: $99 on Jan 28, 2027')).toBeInTheDocument();
  expect(within(dialog).getByRole('heading', { name: 'Next: move your Freshdesk data' })).toBeInTheDocument();
  expect(within(dialog).getByText('We move your tickets, replies, attachments and time entries for you, then send a report that proves nothing was lost.')).toBeInTheDocument();
  expect(within(dialog).getByLabelText('Times that suit you (optional)')).toBeInTheDocument();
  expect(within(dialog).getByRole('button', { name: 'Send to Colliedesk' })).toHaveClass('primary-action');
  expect(dialog.querySelectorAll('.primary-action')).toHaveLength(1);
  expect(within(dialog).getByRole('link', { name: 'Import it myself' })).toHaveAttribute('href', '/settings/import-freshdesk');
  expect(within(dialog).getByRole('button', { name: 'Later' })).toBeInTheDocument();
  expect(within(dialog).queryByText(/receipt sent to/i)).not.toBeInTheDocument();
});

test('a Team 25 welcome says we run the import with the buyer on a call, and its times field is still optional', () => {
  open({ welcome: { plan: 'team25', summary: 'Team 25', nextCharge: 'Next charge: Nov 30, 2026' } });
  expect(screen.getByRole('heading', { name: 'Welcome to Team 25' })).toBeInTheDocument();
  expect(screen.getByText('We run the import with you on a short call. Send your Freshdesk address and times that suit you.')).toBeInTheDocument();
  expect(screen.getByLabelText('Times that suit you (optional)')).toBeInTheDocument();
});

test('sending posts the address and the times, then says when to expect an answer and offers Done', async () => {
  const { onSend, onDismiss } = open();
  fill('northwind.freshdesk.com', 'Tue after 2 pm');
  fireEvent.click(screen.getByRole('button', { name: 'Send to Colliedesk' }));
  expect(onSend).toHaveBeenCalledWith({ freshdesk_address: 'northwind.freshdesk.com', times: 'Tue after 2 pm' });
  expect(await screen.findByText('Sent. We reply by email within one working day.')).toBeInTheDocument();
  expect(screen.getByText('northwind.freshdesk.com')).toBeInTheDocument();
  expect(screen.queryByLabelText('Freshdesk address')).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Done' }));
  expect(onDismiss).toHaveBeenCalledTimes(1);
});

test('while it sends, the fields are read only and the button says Sending… and cannot be pressed twice', async () => {
  let finish: (value: unknown) => void = () => undefined;
  const onSend = vi.fn().mockReturnValue(new Promise((resolve) => { finish = resolve; }));
  open({ onSend });
  fill('northwind.freshdesk.com');
  fireEvent.click(screen.getByRole('button', { name: 'Send to Colliedesk' }));
  const sending = screen.getByRole('button', { name: 'Sending…' });
  expect(sending).toBeDisabled();
  expect(sending).toHaveAttribute('aria-busy', 'true');
  expect(screen.getByLabelText('Freshdesk address')).toHaveAttribute('readonly');
  fireEvent.click(sending);
  expect(onSend).toHaveBeenCalledTimes(1);
  await act(async () => { finish({ freshdesk_address: 'northwind.freshdesk.com', times: '', requested_at: '2026-10-30T09:00:00Z' }); });
});

test('a failed send keeps what was typed, says so with the support address, and the same button tries again', async () => {
  const onSend = vi.fn().mockRejectedValueOnce(new Error('network')).mockResolvedValueOnce({ freshdesk_address: 'northwind.freshdesk.com', times: '', requested_at: '2026-10-30T09:00:00Z' });
  open({ onSend });
  fill('northwind.freshdesk.com', 'Tue after 2 pm');
  fireEvent.click(screen.getByRole('button', { name: 'Send to Colliedesk' }));
  const alert = await screen.findByRole('alert');
  expect(alert).toHaveTextContent('Could not send. Your details are still here. Try again, or email them to support@colliedesk.com.');
  expect(screen.getByLabelText('Freshdesk address')).toHaveValue('northwind.freshdesk.com');
  expect(screen.getByLabelText(/times that suit you/i)).toHaveValue('Tue after 2 pm');
  fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
  expect(await screen.findByText('Sent. We reply by email within one working day.')).toBeInTheDocument();
  expect(onSend).toHaveBeenCalledTimes(2);
});

test('an address the server refuses is shown under its field, and an empty one is refused before anything is sent', async () => {
  const { InvalidRequestError } = await import('../../api/client');
  const onSend = vi.fn().mockRejectedValue(new InvalidRequestError('Check the highlighted fields.', { code: 'validation_failed', fields: { freshdesk_address: 'Use your Freshdesk address, like yourcompany.freshdesk.com.' } }));
  open({ onSend });
  fireEvent.click(screen.getByRole('button', { name: 'Send to Colliedesk' }));
  expect(screen.getByText('Enter your Freshdesk address.')).toBeInTheDocument();
  expect(onSend).not.toHaveBeenCalled();
  fill('acme.example.com');
  fireEvent.click(screen.getByRole('button', { name: 'Send to Colliedesk' }));
  expect(await screen.findByText('Use your Freshdesk address, like yourcompany.freshdesk.com.')).toBeInTheDocument();
  expect(screen.queryByText(/could not send/i)).not.toBeInTheDocument();
  await waitFor(() => expect(screen.getByLabelText('Freshdesk address')).toHaveAttribute('aria-invalid', 'true'));
});

test('Later, the close button, Escape and Import it myself all dismiss the welcome', () => {
  const closers: [string, (dialog: HTMLElement) => void][] = [
    ['Later', () => fireEvent.click(screen.getByRole('button', { name: 'Later' }))],
    ['Close', () => fireEvent.click(screen.getByRole('button', { name: 'Close' }))],
    ['Escape', (dialog) => fireEvent.keyDown(dialog, { key: 'Escape' })],
    ['Import it myself', () => fireEvent.click(screen.getByRole('link', { name: 'Import it myself' }))],
  ];
  for (const [, close] of closers) {
    const { onDismiss, dialog } = open();
    close(dialog);
    expect(onDismiss).toHaveBeenCalledTimes(1);
    document.body.innerHTML = '';
  }
});

test('asked for from the billing page the same form has no welcome or receipt', () => {
  open({ welcome: undefined, plan: 'team25' });
  expect(screen.getByRole('heading', { name: 'Move your Freshdesk data' })).toBeInTheDocument();
  expect(screen.queryByText(/welcome to/i)).not.toBeInTheDocument();
  expect(screen.queryByText(/next charge/i)).not.toBeInTheDocument();
  expect(screen.getByText('We run the import with you on a short call. Send your Freshdesk address and times that suit you.')).toBeInTheDocument();
});
