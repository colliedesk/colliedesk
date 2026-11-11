import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { expect, test } from 'vitest';
import { HelpTip, LockedReason } from './help-tip';

const text = 'Customers see it as the help center title.';
const tip = () => screen.queryByRole('tooltip');

test('the ⓘ button is named after its label, and the tip text is its accessible description while closed', () => {
  render(<HelpTip label="Workspace name" text={text} />);
  const button = screen.getByRole('button', { name: 'About Workspace name' });
  expect(button).toHaveAccessibleDescription(text);
  // A tooltip is not a disclosure: its button never says expanded or collapsed.
  expect(button).not.toHaveAttribute('aria-expanded');
  expect(tip()).not.toBeInTheDocument();
});

test('a mouse over the button opens the tip, and leaving closes it', async () => {
  render(<HelpTip label="Workspace name" text={text} />);
  const button = screen.getByRole('button', { name: 'About Workspace name' });
  fireEvent.pointerEnter(button, { pointerType: 'mouse' });
  expect(screen.getByRole('tooltip')).toHaveTextContent(text);
  expect(button).not.toHaveAttribute('aria-expanded');
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

const linkTip = <HelpTip label="Response targets" text="Targets count business hours." link={{ href: '/docs/targets', label: 'Learn more' }} />;
const layerOf = (button: HTMLElement) => document.getElementById(button.getAttribute('aria-controls') ?? '');

test('a tip with a link is a disclosure: its button says expanded and controls the layer, which is not a tooltip', () => {
  render(linkTip);
  const button = screen.getByRole('button', { name: 'About Response targets' });
  expect(button).toHaveAccessibleDescription('Targets count business hours.');
  expect(button).toHaveAttribute('aria-expanded', 'false');
  expect(layerOf(button)).not.toBeVisible();
  fireEvent.click(button);
  expect(button).toHaveAttribute('aria-expanded', 'true');
  expect(layerOf(button)).toBeVisible();
  expect(layerOf(button)).toContainElement(screen.getByRole('link', { name: 'Learn more' }));
  expect(tip()).not.toBeInTheDocument();
});

test('a tip with a Learn more link stays open while the pointer or focus is inside it', async () => {
  render(<>{linkTip}<button type="button">Next</button></>);
  const button = screen.getByRole('button', { name: 'About Response targets' });
  fireEvent.pointerEnter(button, { pointerType: 'mouse' });
  const layer = layerOf(button)!;
  fireEvent.pointerLeave(button, { pointerType: 'mouse' });
  fireEvent.pointerEnter(layer, { pointerType: 'mouse' });
  await new Promise((resolve) => setTimeout(resolve, 250));
  expect(layer).toBeVisible();
  const link = screen.getByRole('link', { name: 'Learn more' });
  expect(link).toHaveAttribute('href', '/docs/targets');
  act(() => link.focus());
  fireEvent.pointerLeave(layer, { pointerType: 'mouse' });
  await new Promise((resolve) => setTimeout(resolve, 250));
  expect(layer).toBeVisible();
  act(() => screen.getByRole('button', { name: 'Next' }).focus());
  expect(layer).not.toBeVisible();
});

test('Escape on the tip\'s link puts focus back on its button and closes the tip', () => {
  render(linkTip);
  const button = screen.getByRole('button', { name: 'About Response targets' });
  act(() => button.focus());
  const link = screen.getByRole('link', { name: 'Learn more' });
  act(() => link.focus());
  fireEvent.keyDown(link, { key: 'Escape' });
  expect(button).toHaveFocus();
  expect(layerOf(button)).not.toBeVisible();
});

// press does what a browser does on a press: focus goes to the nearest focusable element around the target, or
// leaves the focused one when there is none.
const press = (target: HTMLElement) => {
  fireEvent.pointerDown(target, { pointerType: 'mouse' });
  const focusable = target.closest<HTMLElement>('a[href], button, input, [tabindex]');
  act(() => (focusable ? focusable.focus() : (document.activeElement as HTMLElement | null)?.blur()));
  fireEvent.click(target);
};

test('pressing the text of an open tip keeps it open', () => {
  render(<HelpTip label="Workspace name" text={text} />);
  const button = screen.getByRole('button', { name: 'About Workspace name' });
  act(() => button.focus());
  fireEvent.click(button);
  press(screen.getByText(text));
  expect(screen.getByRole('tooltip')).toHaveTextContent(text);
});

test('Escape on an open tip closes only the tip: an editor around it that cancels on Escape stays open', () => {
  const cancels: string[] = [];
  const onDocument = (event: KeyboardEvent) => { if (event.key === 'Escape') cancels.push('document'); };
  document.addEventListener('keydown', onDocument);
  try {
    render(<div onKeyDown={(event) => { if (event.key === 'Escape') cancels.push('editor'); }}>
      <input aria-label="Agent" /><HelpTip label="Admin" text={text} /></div>);
    const field = screen.getByRole('textbox', { name: 'Agent' });
    act(() => field.focus());
    fireEvent.pointerEnter(screen.getByRole('button', { name: 'About Admin' }), { pointerType: 'mouse' });
    fireEvent.keyDown(field, { key: 'Escape' });
    expect(tip()).not.toBeInTheDocument();
    expect(cancels).toEqual([]);
    fireEvent.keyDown(field, { key: 'Escape' });
    expect(cancels).toEqual(['editor', 'document']);
  } finally {
    document.removeEventListener('keydown', onDocument);
  }
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
