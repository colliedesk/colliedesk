import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { expect, test } from 'vitest';
import { HelpTip, LockedReason } from './help-tip';

const text = 'Customers see it as the help center title.';
const tip = () => screen.queryByRole('tooltip');

test('the ⓘ button is named after its label, and the tip text is its accessible description while closed', () => {
  render(<HelpTip label="Workspace name" text={text} />);
  const button = screen.getByRole('button', { name: 'About Workspace name' });
  expect(button).toHaveAccessibleDescription(text);
  expect(button).toHaveAttribute('aria-expanded', 'false');
  expect(tip()).not.toBeInTheDocument();
});

test('a mouse over the button opens the tip, and leaving closes it', async () => {
  render(<HelpTip label="Workspace name" text={text} />);
  const button = screen.getByRole('button', { name: 'About Workspace name' });
  fireEvent.pointerEnter(button, { pointerType: 'mouse' });
  expect(screen.getByRole('tooltip')).toHaveTextContent(text);
  expect(button).toHaveAttribute('aria-expanded', 'true');
  fireEvent.pointerLeave(button, { pointerType: 'mouse' });
  await waitFor(() => expect(tip()).not.toBeInTheDocument());
});

test('keyboard focus opens the tip, and moving focus away closes it', () => {
  render(<><HelpTip label="Workspace name" text={text} /><button type="button">Next</button></>);
  const button = screen.getByRole('button', { name: 'About Workspace name' });
  act(() => button.focus());
  expect(screen.getByRole('tooltip')).toHaveTextContent(text);
  act(() => screen.getByRole('button', { name: 'Next' }).focus());
  expect(tip()).not.toBeInTheDocument();
});

test('a tap opens the tip and it stays open; a second tap closes it', () => {
  render(<HelpTip label="Workspace name" text={text} />);
  const button = screen.getByRole('button', { name: 'About Workspace name' });
  // A finger reports pointerenter too; it must not count as hover, or the tip would close when the finger lifts.
  fireEvent.pointerEnter(button, { pointerType: 'touch' });
  fireEvent.pointerDown(button, { pointerType: 'touch' });
  fireEvent.click(button);
  fireEvent.pointerLeave(button, { pointerType: 'touch' });
  expect(screen.getByRole('tooltip')).toHaveTextContent(text);
  fireEvent.click(button);
  expect(tip()).not.toBeInTheDocument();
});

test('Escape closes an open tip and leaves focus on its button', () => {
  render(<HelpTip label="Workspace name" text={text} />);
  const button = screen.getByRole('button', { name: 'About Workspace name' });
  act(() => button.focus());
  fireEvent.click(button);
  expect(screen.getByRole('tooltip')).toBeInTheDocument();
  fireEvent.keyDown(button, { key: 'Escape' });
  expect(tip()).not.toBeInTheDocument();
  expect(button).toHaveFocus();
});

test('Escape closes a tip opened by hover while focus is elsewhere', () => {
  render(<HelpTip label="Workspace name" text={text} />);
  fireEvent.pointerEnter(screen.getByRole('button', { name: 'About Workspace name' }), { pointerType: 'mouse' });
  fireEvent.keyDown(document.body, { key: 'Escape' });
  expect(tip()).not.toBeInTheDocument();
});

test('a click outside closes a tip opened by a click', () => {
  render(<><HelpTip label="Workspace name" text={text} /><p>Elsewhere</p></>);
  fireEvent.click(screen.getByRole('button', { name: 'About Workspace name' }));
  expect(screen.getByRole('tooltip')).toBeInTheDocument();
  fireEvent.pointerDown(screen.getByText('Elsewhere'));
  expect(tip()).not.toBeInTheDocument();
});

test('a tip with a Learn more link stays open while the pointer or focus is inside it', async () => {
  render(<><HelpTip label="Response targets" text="Targets count business hours." link={{ href: '/docs/targets', label: 'Learn more' }} />
    <button type="button">Next</button></>);
  const button = screen.getByRole('button', { name: 'About Response targets' });
  expect(button).toHaveAccessibleDescription('Targets count business hours.');
  fireEvent.pointerEnter(button, { pointerType: 'mouse' });
  const layer = screen.getByRole('tooltip');
  fireEvent.pointerLeave(button, { pointerType: 'mouse' });
  fireEvent.pointerEnter(layer, { pointerType: 'mouse' });
  await new Promise((resolve) => setTimeout(resolve, 250));
  expect(screen.getByRole('tooltip')).toBeInTheDocument();
  const link = screen.getByRole('link', { name: 'Learn more' });
  expect(link).toHaveAttribute('href', '/docs/targets');
  act(() => link.focus());
  fireEvent.pointerLeave(layer, { pointerType: 'mouse' });
  await new Promise((resolve) => setTimeout(resolve, 250));
  expect(screen.getByRole('tooltip')).toBeInTheDocument();
  act(() => screen.getByRole('button', { name: 'Next' }).focus());
  expect(tip()).not.toBeInTheDocument();
});

test('a locked reason is a lock button whose tip says why, and can describe the locked control too', () => {
  render(<><input type="checkbox" aria-label="Admin" aria-disabled="true" aria-describedby="admin-why" />
    <LockedReason id="admin-why" label="Admin" text="The only active admin keeps the Admin role." /></>);
  const button = screen.getByRole('button', { name: 'Why Admin is locked' });
  expect(button).toHaveAccessibleDescription('The only active admin keeps the Admin role.');
  expect(screen.getByRole('checkbox', { name: 'Admin' })).toHaveAccessibleDescription('The only active admin keeps the Admin role.');
  fireEvent.click(button);
  expect(screen.getByRole('tooltip')).toHaveTextContent('The only active admin keeps the Admin role.');
});
