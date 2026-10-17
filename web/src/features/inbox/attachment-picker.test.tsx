import { fireEvent, render, screen, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { AttachmentPicker, attachmentProblem } from './attachment-picker';

const MiB = 1024 * 1024;
const limits = { bytes: 10 * MiB, files: 10 };
const file = (name: string, size: number) => new File([new Uint8Array(size)], name, { type: 'application/octet-stream' });

test('choosing files hands them to the owner and the picker accepts several at once', () => {
  const onChange = vi.fn();
  render(<AttachmentPicker files={[]} onChange={onChange} limits={limits} />);
  const input = screen.getByLabelText('Attach files') as HTMLInputElement;
  expect(input.type).toBe('file');
  expect(input.multiple).toBe(true);
  const first = file('invoice.pdf', 2048);
  const second = file('shot.png', 10);
  fireEvent.change(input, { target: { files: [first, second] } });
  expect(onChange).toHaveBeenCalledWith([first, second]);
});

test('files chosen later are added to the ones already chosen', () => {
  const onChange = vi.fn();
  const kept = file('a.txt', 1);
  const added = file('b.txt', 1);
  render(<AttachmentPicker files={[kept]} onChange={onChange} limits={limits} />);
  fireEvent.change(screen.getByLabelText('Attach files'), { target: { files: [added] } });
  expect(onChange).toHaveBeenCalledWith([kept, added]);
});

test('lists each chosen file with its size and removes only the one asked for', () => {
  const onChange = vi.fn();
  const pdf = file('invoice.pdf', 2048);
  const png = file('shot.png', 10);
  render(<AttachmentPicker files={[pdf, png]} onChange={onChange} limits={limits} />);
  const list = screen.getByRole('list', { name: 'Files to send' });
  const items = within(list).getAllByRole('listitem');
  expect(items).toHaveLength(2);
  expect(items[0]).toHaveTextContent('invoice.pdf');
  expect(items[0]).toHaveTextContent('2.0 KiB');
  expect(items[1]).toHaveTextContent('10 B');
  fireEvent.click(within(items[0]).getByRole('button', { name: 'Remove invoice.pdf' }));
  expect(onChange).toHaveBeenCalledWith([png]);
});

test('warns before upload when the total is over the limit and names the limit', () => {
  render(<AttachmentPicker files={[file('big.bin', 6 * MiB), file('more.bin', 5 * MiB)]} onChange={vi.fn()} limits={limits} />);
  expect(screen.getByRole('alert')).toHaveTextContent('10.0 MiB');
  expect(screen.getByRole('alert')).toHaveTextContent('11.0 MiB');
});

test('warns when more files than allowed are chosen', () => {
  const files = Array.from({ length: 11 }, (_, index) => file(`f${index}.txt`, 1));
  render(<AttachmentPicker files={files} onChange={vi.fn()} limits={limits} />);
  expect(screen.getByRole('alert')).toHaveTextContent('at most 10 files');
});

test('no warning while the files fit exactly', () => {
  render(<AttachmentPicker files={[file('exact.bin', 10 * MiB)]} onChange={vi.fn()} limits={limits} />);
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
});

test('attachmentProblem is empty without files or limits and set when over', () => {
  expect(attachmentProblem([], limits)).toBeUndefined();
  expect(attachmentProblem([file('x', 11 * MiB)])).toBeUndefined();
  expect(attachmentProblem([file('x', 11 * MiB)], limits)).toMatch(/10\.0 MiB/);
});
