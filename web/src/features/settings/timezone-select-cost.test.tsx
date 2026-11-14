import { render } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { TimezoneSelect } from './timezone-select';

// The hours card renders on every keystroke in a time field, and its zone list has about 400 entries: a render
// must not build a formatter per listed zone, which cost tens of milliseconds each time.
afterEach(() => vi.restoreAllMocks());

test('a render that changes nothing about the zones builds no new formatter, and the list is still complete', () => {
  const props = { label: 'Time zone', value: '', emptyOption: 'The workspace’s (UTC)', suggested: { zone: 'Asia/Ho_Chi_Minh', label: 'Your time zone (Asia/Ho_Chi_Minh)' }, onChange: () => undefined };
  const view = render(<TimezoneSelect {...props} />);
  const real = Intl.DateTimeFormat;
  let built = 0;
  vi.spyOn(Intl, 'DateTimeFormat').mockImplementation(function (this: unknown, ...args: ConstructorParameters<typeof Intl.DateTimeFormat>) { built += 1; return new real(...args); } as unknown as typeof Intl.DateTimeFormat);
  for (let keystroke = 0; keystroke < 5; keystroke += 1) view.rerender(<TimezoneSelect {...props} suggested={{ ...props.suggested }} />);
  expect(built).toBe(0);
  const options = Array.from(view.container.querySelectorAll('option'));
  expect(options.length).toBeGreaterThan(300);
  expect(options.slice(0, 2).map((option) => option.textContent)).toEqual(['The workspace’s (UTC)', 'Your time zone (Asia/Ho_Chi_Minh)']);
});
