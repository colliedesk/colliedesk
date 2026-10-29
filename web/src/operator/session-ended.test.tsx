import { render, screen } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { App } from './app';
import { storeDraft, storedDraft } from './guards';

// UI review risk: a session that ends keeps the unsaved form. A new sign-in link loads the page again, so the
// draft lives in this tab's session storage; a secret typed into it never does.
afterEach(() => { vi.restoreAllMocks(); sessionStorage.clear(); window.history.replaceState(null, '', '/'); });

test('the draft outlives the page, without the secrets typed into it', () => {
  storeDraft({ set: { 'smtp.host': 'mail.example.org' }, unset: ['smtp.username'], secrets: { 'smtp.password': 'typed-secret' } });
  expect(sessionStorage.getItem('colliedesk.operator.draft')).not.toContain('typed-secret');
  expect(storedDraft()).toEqual({ set: { 'smtp.host': 'mail.example.org' }, unset: ['smtp.username'], secrets: {} });
  storeDraft({ set: {}, unset: [], secrets: {} });
  expect(sessionStorage.getItem('colliedesk.operator.draft')).toBeNull();
});

test('an ended session shows its screen and keeps the unsaved changes', async () => {
  storeDraft({ set: { 'smtp.host': 'mail.example.org' }, unset: [], secrets: {} });
  window.history.replaceState(null, '', '/operator/config/email');
  vi.spyOn(globalThis, 'fetch').mockImplementation(() => Promise.resolve(new Response(JSON.stringify({ error: { code: 'operator_session_required', message: 'no' } }),
    { status: 401, headers: { 'Content-Type': 'application/json' } })));
  render(<App />);
  expect(await screen.findByRole('heading', { name: 'Your session ended' })).toBeInTheDocument();
  expect(storedDraft().set).toEqual({ 'smtp.host': 'mail.example.org' });
});
