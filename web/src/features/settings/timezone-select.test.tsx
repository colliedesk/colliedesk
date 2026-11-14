import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { TimezoneSelect } from './timezone-select';

afterEach(cleanup);

// A list inside a form (the business hours card) must not submit that form on Enter: browsers differ on whether
// Enter on a closed list sends it, and a pick that is only handed over must never save the card as a side effect.
test('Enter on the list hands over the pick and does not let the browser submit the form around it', () => {
  const onChange = vi.fn();
  render(<form><TimezoneSelect label="Time zone" value="" emptyOption="Workspace" onChange={onChange} /></form>);
  const list = screen.getByLabelText('Time zone');
  fireEvent.change(list, { target: { value: 'Asia/Tokyo' } });
  // fireEvent returns false when the event's default action (the implicit submit) was prevented.
  expect(fireEvent.keyDown(list, { key: 'Enter' })).toBe(false);
  expect(onChange).toHaveBeenCalledExactlyOnceWith('Asia/Tokyo');
});

test('other keys keep their default action', () => {
  render(<form><TimezoneSelect label="Time zone" value="" emptyOption="Workspace" onChange={vi.fn()} /></form>);
  expect(fireEvent.keyDown(screen.getByLabelText('Time zone'), { key: 'ArrowDown' })).toBe(true);
});
