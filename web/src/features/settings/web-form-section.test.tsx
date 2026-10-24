import { fireEvent, render, screen } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import type { WebFormSettings } from '../../api/schemas';
import { WebFormSection } from './web-form-section';

const off: WebFormSettings = { enabled: false, allowed_origins: [], submit_path: '/api/v1/public/forms/acme' };
const on: WebFormSettings = { ...off, enabled: true, allowed_origins: ['https://colliedesk.com', 'http://localhost:3000'] };

test('is off by default, shows the state and the address a site posts to', () => {
  render(<WebFormSection settings={off} canManage onSave={vi.fn()} />);
  expect(screen.getByRole('checkbox', { name: /accept web form submissions/i })).not.toBeChecked();
  expect(screen.getByText(/the web form is off/i)).toBeInTheDocument();
  expect(screen.getByRole('textbox', { name: /submit url/i })).toHaveValue(`${window.location.origin}/api/v1/public/forms/acme`);
});

test('turns the form on with its allowed origins, one per line', () => {
  const onSave = vi.fn();
  render(<WebFormSection settings={off} canManage onSave={onSave} />);
  fireEvent.click(screen.getByRole('checkbox', { name: /accept web form submissions/i }));
  fireEvent.change(screen.getByRole('textbox', { name: /allowed origins/i }), { target: { value: ' https://colliedesk.com \n\nhttp://localhost:3000\n' } });
  fireEvent.click(screen.getByRole('button', { name: /save web form/i }));
  expect(onSave).toHaveBeenCalledWith(true, ['https://colliedesk.com', 'http://localhost:3000']);
});

test('turns the form off again and keeps the origins', () => {
  const onSave = vi.fn();
  render(<WebFormSection settings={on} canManage onSave={onSave} />);
  expect(screen.getByRole('checkbox', { name: /accept web form submissions/i })).toBeChecked();
  expect(screen.getByText(/the web form is on/i)).toBeInTheDocument();
  expect(screen.getByRole('textbox', { name: /allowed origins/i })).toHaveValue('https://colliedesk.com\nhttp://localhost:3000');
  fireEvent.click(screen.getByRole('checkbox', { name: /accept web form submissions/i }));
  fireEvent.click(screen.getByRole('button', { name: /save web form/i }));
  expect(onSave).toHaveBeenCalledWith(false, ['https://colliedesk.com', 'http://localhost:3000']);
});

test('agents see the state read-only', () => {
  render(<WebFormSection settings={on} canManage={false} />);
  expect(screen.getByText(/the web form is on/i)).toBeInTheDocument();
  expect(screen.getByText('https://colliedesk.com')).toBeInTheDocument();
  expect(screen.queryByRole('checkbox')).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /save web form/i })).not.toBeInTheDocument();
});

test('the save shows Saving…, refuses a second submit, then Saved until edited', () => {
  const onSave = vi.fn();
  const { rerender } = render(<WebFormSection settings={off} canManage onSave={onSave} save={{ pending: true }} />);
  expect(screen.getByRole('button', { name: 'Saving…' })).toBeDisabled();
  fireEvent.submit(screen.getByRole('textbox', { name: /allowed origins/i }));
  expect(onSave).not.toHaveBeenCalled();
  rerender(<WebFormSection settings={off} canManage onSave={onSave} save={{ saved: true }} />);
  expect(screen.getByText('Saved')).toHaveAttribute('role', 'status');
  fireEvent.click(screen.getByRole('checkbox', { name: /accept web form submissions/i }));
  expect(screen.queryByText('Saved')).not.toBeInTheDocument();
});

test('a refused save shows the message', () => {
  render(<WebFormSection settings={off} canManage save={{ error: 'Invalid origin.' }} />);
  expect(screen.getByRole('alert')).toHaveTextContent('Invalid origin.');
});

test('the Embed tab copies the submit URL and shows the code a page posts with, ready to copy', async () => {
  const writeText = vi.fn(() => Promise.resolve());
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } });
  render(<WebFormSection part="embed" settings={on} canManage onSave={vi.fn()} />);
  const url = `${window.location.origin}/api/v1/public/forms/acme`;
  fireEvent.click(screen.getByRole('button', { name: 'Copy submit URL' }));
  expect(writeText).toHaveBeenLastCalledWith(url);
  expect(await screen.findByText('URL copied')).toHaveAttribute('role', 'status');
  const code = screen.getByRole('region', { name: 'Code for your page' });
  expect(code).toHaveTextContent(`fetch('${url}'`);
  expect(code).toHaveTextContent("website: ''");
  fireEvent.click(screen.getByRole('button', { name: 'Copy code' }));
  expect(writeText).toHaveBeenLastCalledWith(expect.stringContaining(`fetch('${url}'`));
});
