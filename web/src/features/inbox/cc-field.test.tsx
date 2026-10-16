import { fireEvent, render, screen, within } from '@testing-library/react';
import { useState } from 'react';
import { expect, test, vi } from 'vitest';
import { CcField } from './cc-field';

function Harness({ initial = [], onChange, disabled }: { initial?: string[]; onChange?: (value: string[]) => void; disabled?: boolean }) {
  const [value, setValue] = useState(initial);
  return <CcField value={value} disabled={disabled} onChange={(next) => { setValue(next); onChange?.(next); }} />;
}

const input = () => screen.getByLabelText('Add Cc address');

test('Enter turns the typed address into a chip and empties the box', () => {
  const onChange = vi.fn();
  render(<Harness onChange={onChange} />);
  fireEvent.change(input(), { target: { value: 'Boss@Customer.example' } });
  fireEvent.keyDown(input(), { key: 'Enter' });
  expect(onChange).toHaveBeenLastCalledWith(['boss@customer.example']);
  expect(input()).toHaveValue('');
  expect(within(screen.getByRole('list', { name: 'Cc' })).getByText('boss@customer.example')).toBeInTheDocument();
});

test.each([',', ';', ' '])('typing %j after an address commits it', (key) => {
  const onChange = vi.fn();
  render(<Harness onChange={onChange} />);
  fireEvent.change(input(), { target: { value: 'ops@customer.example' } });
  fireEvent.keyDown(input(), { key });
  expect(onChange).toHaveBeenLastCalledWith(['ops@customer.example']);
});

test('leaving the box commits what was typed, and a pasted list becomes one chip per address', () => {
  const onChange = vi.fn();
  render(<Harness onChange={onChange} />);
  fireEvent.change(input(), { target: { value: 'a@customer.example' } });
  fireEvent.blur(input());
  expect(onChange).toHaveBeenLastCalledWith(['a@customer.example']);
  fireEvent.paste(input(), { clipboardData: { getData: () => 'b@customer.example, c@customer.example;\nd@customer.example' } });
  expect(onChange).toHaveBeenLastCalledWith(['a@customer.example', 'b@customer.example', 'c@customer.example', 'd@customer.example']);
});

test('an address that is not plain stays in the box with the reason, and nothing is added', () => {
  const onChange = vi.fn();
  render(<Harness onChange={onChange} />);
  for (const bad of ['not-an-address', 'Boss <boss@customer.example>', 'a@b@c']) {
    fireEvent.change(input(), { target: { value: bad } });
    fireEvent.keyDown(input(), { key: 'Enter' });
    expect(screen.getByRole('alert')).toHaveTextContent(/plain email address/i);
    expect(input()).toHaveValue(bad);
  }
  expect(onChange).not.toHaveBeenCalled();
  expect(input()).toHaveAttribute('aria-invalid', 'true');
});

test('typing again clears the complaint', () => {
  render(<Harness />);
  fireEvent.change(input(), { target: { value: 'nope' } });
  fireEvent.keyDown(input(), { key: 'Enter' });
  fireEvent.change(input(), { target: { value: 'nope@' } });
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
});

test('an address already copied is not added twice, whatever its case', () => {
  const onChange = vi.fn();
  render(<Harness initial={['boss@customer.example']} onChange={onChange} />);
  fireEvent.change(input(), { target: { value: 'BOSS@customer.example' } });
  fireEvent.keyDown(input(), { key: 'Enter' });
  expect(onChange).not.toHaveBeenCalled();
  expect(input()).toHaveValue('');
  expect(screen.getAllByRole('listitem')).toHaveLength(1);
});

test('the eleventh address is refused with the limit', () => {
  const ten = Array.from({ length: 10 }, (_, index) => `p${index}@customer.example`);
  const onChange = vi.fn();
  render(<Harness initial={ten} onChange={onChange} />);
  fireEvent.change(input(), { target: { value: 'more@customer.example' } });
  fireEvent.keyDown(input(), { key: 'Enter' });
  expect(screen.getByRole('alert')).toHaveTextContent('at most 10');
  expect(onChange).not.toHaveBeenCalled();
});

test('each chip has a labelled remove button that gives focus back to the box', () => {
  const onChange = vi.fn();
  render(<Harness initial={['a@customer.example', 'b@customer.example']} onChange={onChange} />);
  fireEvent.click(screen.getByRole('button', { name: 'Remove a@customer.example' }));
  expect(onChange).toHaveBeenLastCalledWith(['b@customer.example']);
  expect(input()).toHaveFocus();
});

test('Backspace in an empty box removes the last chip; with text it only edits the text', () => {
  const onChange = vi.fn();
  render(<Harness initial={['a@customer.example', 'b@customer.example']} onChange={onChange} />);
  fireEvent.change(input(), { target: { value: 'x' } });
  fireEvent.keyDown(input(), { key: 'Backspace' });
  expect(onChange).not.toHaveBeenCalled();
  fireEvent.change(input(), { target: { value: '' } });
  fireEvent.keyDown(input(), { key: 'Backspace' });
  expect(onChange).toHaveBeenLastCalledWith(['a@customer.example']);
});

test('Enter in an empty box does nothing, so it never swallows the page', () => {
  const onChange = vi.fn();
  render(<Harness onChange={onChange} />);
  const notPrevented = fireEvent.keyDown(input(), { key: 'Enter' });
  expect(notPrevented).toBe(true);
  expect(onChange).not.toHaveBeenCalled();
});

test('Ctrl+Enter finishes a typed address first, and sends from an empty box', () => {
  const onSend = vi.fn();
  const onChange = vi.fn();
  render(<><CcField value={[]} onChange={onChange} onSend={onSend} /></>);
  fireEvent.change(input(), { target: { value: 'a@customer.example' } });
  fireEvent.keyDown(input(), { key: 'Enter', ctrlKey: true });
  expect(onChange).toHaveBeenLastCalledWith(['a@customer.example']);
  expect(onSend).not.toHaveBeenCalled();
  fireEvent.change(input(), { target: { value: '' } });
  fireEvent.keyDown(input(), { key: 'Enter', metaKey: true });
  expect(onSend).toHaveBeenCalledTimes(1);
});

test('while sending, the box and the chips cannot be changed', () => {
  render(<Harness initial={['a@customer.example']} disabled />);
  expect(input()).toBeDisabled();
  expect(screen.getByRole('button', { name: 'Remove a@customer.example' })).toBeDisabled();
});
