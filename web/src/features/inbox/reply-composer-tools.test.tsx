import { render, screen } from '@testing-library/react';
import { beforeAll, expect, test, vi } from 'vitest';
import { presenceViewPart } from './presence-part';
import { ReplyComposer } from './reply-composer';

// The presence chunk loads with a notice; start with it there, so no test ends while it is still on its way.
beforeAll(async () => {
  presenceViewPart.preload();
  await import('./presence-view');
});

// An icon is not a name: each tool of the reply box says what it is on hover and focus, as every other icon
// button of the app does (data-tip), and keeps that name for a screen reader.
test('every tool of the reply box has an accessible name and the tooltip of the app', () => {
  render(<ReplyComposer onSubmit={vi.fn()} aiDraftsFor="33333333-3333-4333-8333-333333333333" canManage />);
  const tools = document.querySelector('.composer-actions') as HTMLElement;
  const names: Array<[string, string]> = [['Add attachment', 'Add attachment'], ['Insert saved reply', 'Insert saved reply (/)'], ['Articles', 'Articles'], ['Draft with AI', 'Draft with AI']];
  for (const [name, tip] of names) {
    const tool = screen.getByRole('button', { name: new RegExp(`^${name}`) });
    expect(tools).toContainElement(tool);
    expect(tool, name).toHaveAttribute('data-tip', tip);
    // One tooltip, not the app's and the browser's on top of each other.
    expect(tool, name).not.toHaveAttribute('title');
  }
});

test('the off AI draft control names the feature and its state, and says how to turn it on', () => {
  render(<ReplyComposer onSubmit={vi.fn()} aiDraftsOff canManage />);
  const off = screen.getByRole('link', { name: 'Draft with AI (off)' });
  expect(off).toHaveAttribute('data-tip', 'Reply drafts are off. Turn them on in Settings → Built-in AI.');
  expect(off).not.toHaveAttribute('title');
  // The words on the control read as the name does, so a person never sees a bare "Off".
  expect(off).toHaveTextContent('Draft with AI');
  expect(off).toHaveTextContent('Off');
});
