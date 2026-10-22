import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { HelpCenterThemeForm } from './help-center-theme-form';
import { ids, json, serve } from './kb.test.fixture';

afterEach(() => vi.unstubAllGlobals());

const check = (pair: string, field: string, foreground: string, background: string, ratio: number, required = 4.5) =>
  ({ pair, field, foreground, background, ratio, required, passes: ratio >= required, suggestion: ratio >= required ? null : '#5c5c5c' });
const defaultChecks = [
  check('text_on_background', 'colors.text', '#16201c', '#f5f7f5', 15.51), check('link_on_background', 'colors.link', '#17675a', '#f5f7f5', 6.24),
  check('header_text_on_header', 'colors.header_text', '#16201c', '#ffffff', 16.7), check('ink_on_primary', 'colors.primary', '#ffffff', '#17675a', 6.72),
  check('footer_ink_on_footer', 'colors.footer', '#16201c', '#ffffff', 16.7), check('primary_on_background', 'colors.primary', '#17675a', '#f5f7f5', 6.24, 3),
];
const noColors = { primary: null, header: null, header_text: null, background: null, text: null, link: null, footer: null };
const saved = {
  logo_media_id: ids.image, favicon_media_id: null, logo_alt: 'Acme', linkback_url: 'https://acme.example/', contact_url: null, contact_phone: '+1 555 0100',
  colors: { ...noColors, primary: '#0b5cad' }, body_font: 'humanist-sans', heading_font: null, content_version: 4, updated_at: '2026-10-22T00:00:00Z', checks: defaultChecks,
};
const preview = { html: '<!doctype html><meta charset="utf-8">', ok: true, checks: defaultChecks, fields: {} };

function open(canManage: boolean) {
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><HelpCenterThemeForm canManage={canManage} /></QueryClientProvider>);
}

test('a refused save shows each message next to its control and moves the focus to the first one', async () => {
  const text = 'Text on the background is 2.85:1; it needs 4.5:1. #5c5c5c passes.';
  const calls = serve({
    'GET /api/v1/help-center/theme': () => json(200, saved),
    'POST /api/v1/help-center/theme/preview': () => json(200, preview),
    'PUT /api/v1/help-center/theme': () => json(422, { error: { code: 'contrast_too_low', message: `${text} Nothing was saved.`, fields: { 'colors.text': text, 'colors.link': 'Links on the background is 3:1; it needs 4.5:1.' } } }),
  });
  open(true);
  const textColor = await screen.findByRole('textbox', { name: 'Text color' });
  fireEvent.change(textColor, { target: { value: '#999999' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save brand' }));
  await waitFor(() => expect(textColor).toHaveFocus());
  expect(textColor).toHaveAttribute('aria-invalid', 'true');
  expect(textColor).toHaveAccessibleDescription(expect.stringContaining(text));
  expect(screen.getByRole('textbox', { name: 'Link color' })).toHaveAccessibleDescription(expect.stringContaining('Links on the background is 3:1'));
  const body = calls.find((call) => call.method === 'PUT')?.body as { colors: Record<string, string | null> };
  expect(body.colors).toEqual({ ...noColors, primary: '#0b5cad', text: '#999999' });
});

test('an admin saves the brand and is told it is live on the next page load', async () => {
  const calls = serve({
    'GET /api/v1/help-center/theme': () => json(200, saved),
    'POST /api/v1/help-center/theme/preview': () => json(200, preview),
    'PUT /api/v1/help-center/theme': (call) => json(200, { ...saved, ...(call.body as object), content_version: 5 }),
  });
  open(true);
  fireEvent.change(await screen.findByRole('textbox', { name: 'Your website' }), { target: { value: ' https://acme.example/help ' } });
  fireEvent.change(screen.getByRole('combobox', { name: 'Heading font' }), { target: { value: 'classic-serif' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save brand' }));
  expect(await screen.findByText('Saved. Live on the next page load.')).toHaveAttribute('role', 'status');
  expect(calls.find((call) => call.method === 'PUT')?.body).toEqual({
    logo_media_id: ids.image, favicon_media_id: null, logo_alt: 'Acme', linkback_url: 'https://acme.example/help', contact_url: null, contact_phone: '+1 555 0100',
    colors: { ...noColors, primary: '#0b5cad' }, body_font: 'humanist-sans', heading_font: 'classic-serif',
  });
});

test('each color resets to its default, and the whole brand resets to the default look until saved', async () => {
  const calls = serve({
    'GET /api/v1/help-center/theme': () => json(200, saved),
    'POST /api/v1/help-center/theme/preview': () => json(200, preview),
    'PUT /api/v1/help-center/theme': (call) => json(200, { ...saved, ...(call.body as object) }),
  });
  open(true);
  const primary = await screen.findByRole('textbox', { name: 'Primary color' });
  expect(primary).toHaveValue('#0b5cad');
  expect(screen.getByRole('button', { name: 'Reset text color to default' })).toBeDisabled();
  fireEvent.click(screen.getByRole('button', { name: 'Reset primary color to default' }));
  expect(primary).toHaveValue('');
  // An empty color shows the color in use as its hint, read from the checks.
  expect(primary).toHaveAttribute('placeholder', '#17675a');

  fireEvent.click(screen.getByRole('button', { name: 'Reset to default look' }));
  expect(screen.getByRole('textbox', { name: 'Your website' })).toHaveValue('');
  expect(screen.getByRole('combobox', { name: 'Text font' })).toHaveValue('');
  expect(screen.queryByRole('img', { name: 'Logo' })).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Save brand' }));
  await screen.findByText('Saved. Live on the next page load.');
  expect(calls.find((call) => call.method === 'PUT')?.body).toEqual({
    logo_media_id: null, favicon_media_id: null, logo_alt: '', linkback_url: null, contact_url: null, contact_phone: null,
    colors: noColors, body_font: null, heading_font: null,
  });
});

test('the preview sends the theme being edited and each color shows its contrast from the answer', async () => {
  const calls = serve({
    'GET /api/v1/help-center/theme': () => json(200, saved),
    'POST /api/v1/help-center/theme/preview': (call) => {
      const failing = (call.body as { theme: { colors: { text: string | null } } }).theme.colors.text === '#999999';
      return json(200, failing
        ? { ...preview, ok: false, checks: [check('text_on_background', 'colors.text', '#999999', '#f5f7f5', 2.67), ...defaultChecks.slice(1)], fields: { 'colors.text': 'Text on the background is 2.67:1; it needs 4.5:1. #5c5c5c passes.' } }
        : preview);
    },
  });
  open(true);
  const text = await screen.findByRole('textbox', { name: 'Text color' });
  await waitFor(() => expect(text).toHaveAccessibleDescription(expect.stringContaining('Contrast 15.51:1 (needs 4.5:1)')));
  fireEvent.change(text, { target: { value: '#999999' } });
  await waitFor(() => expect(text).toHaveAccessibleDescription(expect.stringContaining('Text on the background is 2.67:1')));
  // Not an error yet: the field is not marked invalid until a save is refused.
  expect(text).not.toHaveAttribute('aria-invalid');
  const sent = calls.filter((call) => call.method === 'POST').at(-1)?.body as { page: string; theme: { colors: Record<string, string | null> } };
  expect(sent.page).toBe('home');
  expect(sent.theme.colors.text).toBe('#999999');
});

test('an agent sees the brand but has no control to change it', async () => {
  serve({ 'GET /api/v1/help-center/theme': () => json(200, saved), 'POST /api/v1/help-center/theme/preview': () => json(200, preview) });
  open(false);
  const values = await screen.findByRole('list', { name: 'Brand' });
  expect(within(values).getByText('https://acme.example/')).toBeInTheDocument();
  expect(within(values).getByText('#0b5cad')).toBeInTheDocument();
  expect(within(values).getByText('Humanist sans')).toBeInTheDocument();
  expect(screen.getByText('Only admins can change the brand.')).toBeInTheDocument();
  expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
  expect(screen.queryByRole('combobox')).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /Save|Reset|Upload|Remove/ })).not.toBeInTheDocument();
});
