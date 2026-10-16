import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { NotificationToggle } from './notification-toggle';

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

test('rendering never asks for permission; the click does, and the button then shows it is on', async () => {
  request.mockImplementation(async () => { FakeNotification.permission = 'granted'; return 'granted'; });
  render(<NotificationToggle />);
  expect(request).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Enable desktop notifications' }));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Desktop notifications are on. Turn off' })).toBeInTheDocument());
  expect(request).toHaveBeenCalledTimes(1);
  expect(localStorage.getItem('colliedesk.desktop-notifications')).toBe('on');
});

test('turning off keeps the browser permission and the button offers to enable again', async () => {
  FakeNotification.permission = 'granted';
  localStorage.setItem('colliedesk.desktop-notifications', 'on');
  render(<NotificationToggle />);
  fireEvent.click(screen.getByRole('button', { name: 'Desktop notifications are on. Turn off' }));
  expect(screen.getByRole('button', { name: 'Enable desktop notifications' })).toBeInTheDocument();
  expect(request).not.toHaveBeenCalled();
});

test('a browser that blocked notifications is shown as blocked and is not asked again', () => {
  FakeNotification.permission = 'denied';
  render(<NotificationToggle />);
  const button = screen.getByRole('button', { name: 'Desktop notifications are blocked in this browser' });
  expect(button).toBeDisabled();
});

test('a declined prompt leaves notifications off', async () => {
  request.mockResolvedValue('denied');
  render(<NotificationToggle />);
  fireEvent.click(screen.getByRole('button', { name: 'Enable desktop notifications' }));
  await waitFor(() => expect(request).toHaveBeenCalled());
  expect(localStorage.getItem('colliedesk.desktop-notifications')).toBeNull();
});

test('renders nothing where the browser has no Notification support', () => {
  vi.stubGlobal('Notification', undefined);
  const { container } = render(<NotificationToggle />);
  expect(container).toBeEmptyDOMElement();
});
