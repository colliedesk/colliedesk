import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { ResultsAdmin } from './results-admin';
import type { ResultsAdminProps } from './results-admin';

const link = { created_at: '2026-10-20T09:00:00Z', created_by_name: 'Mai Tran', last_viewed_at: '2026-10-22T13:10:00Z' };
const weekly = { subscribed: true, can_send: true, next_at: '2026-10-26T07:00:00Z', timezone: 'Europe/Berlin' };

function renderAdmin(props: Partial<ResultsAdminProps> = {}) {
  const handlers = { onCreate: vi.fn(), onTurnOff: vi.fn(), onWeekly: vi.fn() };
  render(<ResultsAdmin timezone="Europe/Berlin" link={link} linkFailed={false} replacedOld={false} busy={false} shareFailed={false}
    weekly={weekly} weeklyBusy={false} weeklyFailed={false} {...handlers} {...props} />);
  return handlers;
}

describe('ResultsAdmin', () => {
  it('keeps the focus on the share buttons when a change ends', () => {
    const props = { timezone: 'Europe/Berlin', linkFailed: false, replacedOld: false, shareFailed: false, weekly, weeklyBusy: false, weeklyFailed: false,
      onCreate: vi.fn(), onTurnOff: vi.fn(), onWeekly: vi.fn() };
    const { rerender } = render(<ResultsAdmin {...props} link={link} busy={false} />);
    fireEvent.click(screen.getByRole('button', { name: 'Turn off' }));
    rerender(<ResultsAdmin {...props} link={link} busy />);
    (document.activeElement as HTMLElement | null)?.blur();
    rerender(<ResultsAdmin {...props} link={null} busy={false} />);
    expect(screen.getByRole('button', { name: 'Create link' })).toHaveFocus();
    fireEvent.click(screen.getByRole('button', { name: 'Create link' }));
    rerender(<ResultsAdmin {...props} link={null} busy />);
    rerender(<ResultsAdmin {...props} link={link} created="https://desk.example/r#new" busy={false} />);
    expect(screen.getByRole('button', { name: 'Copy link' })).toHaveFocus();
  });

  it('shows the link state and turns it off or replaces it', () => {
    const handlers = renderAdmin();
    expect(screen.getByText(/On · created Oct 20, 2026 by Mai Tran · last viewed Oct 22/)).toBeVisible();
    expect(screen.queryByText(/r#/)).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Turn off' }));
    fireEvent.click(screen.getByRole('button', { name: 'Create a new link' }));
    expect(handlers.onTurnOff).toHaveBeenCalled();
    expect(handlers.onCreate).toHaveBeenCalled();
  });

  it('shows a new link once, to copy, and says the old one stopped', () => {
    renderAdmin({ created: 'https://desk.example/r#token', replacedOld: true });
    expect(screen.getByText('https://desk.example/r#token')).toBeVisible();
    expect(screen.getByText(/You will not see this link again/)).toBeVisible();
    expect(screen.getByText(/Your old link was turned off/)).toBeVisible();
    expect(screen.getByRole('button', { name: 'Copy link' })).toBeVisible();
  });

  it('offers a link when there is none', () => {
    renderAdmin({ link: null });
    expect(screen.getByText('Off · no link')).toBeVisible();
    expect(screen.getByRole('button', { name: 'Create link' })).toBeVisible();
    expect(screen.queryByRole('button', { name: 'Turn off' })).toBeNull();
  });

  it('lets each admin turn the weekly email off, and says when the server cannot send it', () => {
    const handlers = renderAdmin();
    expect(screen.getByText('On. Next email: Oct 26, 2026.')).toBeVisible();
    fireEvent.click(screen.getByRole('switch', { name: 'Email me on Mondays' }));
    expect(handlers.onWeekly).toHaveBeenCalledWith(false);
  });

  it('explains why the weekly email cannot be turned on without mail settings, and still lets it be turned off', () => {
    const handlers = renderAdmin({ weekly: { ...weekly, can_send: false } });
    fireEvent.click(screen.getByRole('switch', { name: 'Email me on Mondays' }));
    expect(handlers.onWeekly).toHaveBeenCalledWith(false);
    cleanup();
    renderAdmin({ weekly: { ...weekly, subscribed: false, can_send: false } });
    expect(screen.getByRole('switch', { name: 'Email me on Mondays' })).toBeDisabled();
    expect(screen.getByText(/Ask whoever runs it to set SMTP_HOST and MAIL_FROM_DOMAIN/)).toBeVisible();
    expect(screen.getByRole('link', { name: 'How to set up outgoing email' })).toBeVisible();
  });
});
