import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { NotificationsSetting } from './notifications-setting';

const request = vi.fn();
class FakeNotification {
  static permission: NotificationPermission = 'default';
  static requestPermission = request;
}

beforeEach(() => {
  localStorage.clear();
  request.mockReset();
  FakeNotification.permission = 'default';
  vi.stubGlobal('Notification', FakeNotification);
});
afterEach(() => vi.unstubAllGlobals());

const box = () => screen.getByRole('checkbox', { name: /desktop notifications/i });

test('off by default; ticking it asks the browser only on the click and stays ticked once granted', async () => {
  request.mockImplementation(async () => { FakeNotification.permission = 'granted'; return 'granted'; });
  render(<NotificationsSetting />);
  expect(box()).not.toBeChecked();
  expect(request).not.toHaveBeenCalled();
  fireEvent.click(box());
  await waitFor(() => expect(box()).toBeChecked());
  expect(request).toHaveBeenCalledTimes(1);
});

test('unticking keeps the browser permission and does not ask again', () => {
  FakeNotification.permission = 'granted';
  localStorage.setItem('colliedesk.desktop-notifications', 'on');
  render(<NotificationsSetting />);
  expect(box()).toBeChecked();
  fireEvent.click(box());
  expect(box()).not.toBeChecked();
  expect(request).not.toHaveBeenCalled();
});

test('a declined prompt leaves it unticked', async () => {
  request.mockResolvedValue('denied');
  render(<NotificationsSetting />);
  fireEvent.click(box());
  await waitFor(() => expect(request).toHaveBeenCalled());
  expect(box()).not.toBeChecked();
});

test('a blocked browser disables the box and says how to unblock', () => {
  FakeNotification.permission = 'denied';
  render(<NotificationsSetting />);
  expect(box()).toBeDisabled();
  expect(screen.getByText(/site settings/)).toBeInTheDocument();
});

test('a browser without notifications shows nothing', () => {
  vi.stubGlobal('Notification', undefined);
  const { container } = render(<NotificationsSetting />);
  expect(container).toBeEmptyDOMElement();
});
