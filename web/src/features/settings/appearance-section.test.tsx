import { fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, test } from 'vitest';
import { SettingsPage } from './settings-page';

afterEach(() => { localStorage.clear(); document.documentElement.removeAttribute('data-theme'); });

test('settings offers light and dark, light chosen by default', () => {
  render(<SettingsPage sessions={[]} invites={[]} timezone="UTC" />);
  expect(screen.getByRole('radio', { name: 'Light' })).toBeChecked();
  expect(screen.getByRole('radio', { name: 'Dark' })).not.toBeChecked();
});

test('choosing dark switches the page and remembers it', () => {
  render(<SettingsPage sessions={[]} invites={[]} timezone="UTC" />);
  fireEvent.click(screen.getByRole('radio', { name: 'Dark' }));
  expect(document.documentElement.dataset.theme).toBe('dark');
  expect(localStorage.getItem('colliedesk.theme')).toBe('dark');
  fireEvent.click(screen.getByRole('radio', { name: 'Light' }));
  expect(document.documentElement.dataset.theme).toBe('light');
  expect(localStorage.getItem('colliedesk.theme')).toBe('light');
});
