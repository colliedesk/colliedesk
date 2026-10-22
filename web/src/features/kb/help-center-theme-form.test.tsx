import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { previewPolicy } from './help-center-preview';
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
  colors: { ...noColors, primary: '#0b5cad' }, body_font: 'humanist-sans', heading_font: null, custom_css: '', content_version: 4, updated_at: '2026-10-22T00:00:00Z', checks: defaultChecks,
};
const preview = {
  html: `<!doctype html><meta charset="utf-8">\n<meta http-equiv="Content-Security-Policy" content="${previewPolicy(window.location.origin)}">\n<h1>Acme help</h1>`,
  ok: true, checks: defaultChecks, fields: {},
};

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
    colors: { ...noColors, primary: '#0b5cad' }, body_font: 'humanist-sans', heading_font: 'classic-serif', custom_css: '',
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
    colors: noColors, body_font: null, heading_font: null, custom_css: '',
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
  expect(within(values).getByText('Custom CSS').nextElementSibling).toHaveTextContent('None');
  expect(screen.getByText('Only admins can change the brand.')).toBeInTheDocument();
  expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
  expect(screen.queryByRole('combobox')).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /Save|Reset|Upload|Remove/ })).not.toBeInTheDocument();
});

test('a website, contact link or phone number still being typed leaves the preview working: it is sent as empty and its field says the shape', async () => {
  const calls = serve({
    'GET /api/v1/help-center/theme': () => json(200, saved),
    'POST /api/v1/help-center/theme/preview': () => json(200, preview),
    'PUT /api/v1/help-center/theme': () => json(422, { error: { code: 'validation_failed', message: 'Check the highlighted fields. Nothing was saved.', fields: { linkback_url: 'Send an absolute http or https address with a host name and no user name.' } } }),
  });
  open(true);
  const website = await screen.findByRole('textbox', { name: 'Your website' });
  const contact = screen.getByRole('textbox', { name: 'Contact link' });
  const phone = screen.getByRole('textbox', { name: 'Phone number' });
  fireEvent.change(website, { target: { value: 'https:' } });
  fireEvent.change(contact, { target: { value: 'mailto:help@' } });
  fireEvent.change(phone, { target: { value: '+1 555 01a' } });
  await waitFor(() => {
    const sent = calls.filter((call) => call.method === 'POST').at(-1)?.body as { theme: Record<string, unknown> } | undefined;
    expect(sent?.theme).toMatchObject({ linkback_url: null, contact_url: null, contact_phone: null });
  });
  expect(website).toHaveAccessibleDescription('Not in the preview yet: write an address such as https://example.com.');
  expect(contact).toHaveAccessibleDescription('Not in the preview yet: write an https:// address, or mailto: and one email address.');
  expect(phone).toHaveAccessibleDescription('Not in the preview yet: use digits, spaces and + - ( ).');
  expect(website).not.toHaveAttribute('aria-invalid');
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();

  // Complete values go to the preview as typed.
  fireEvent.change(website, { target: { value: 'https://acme.example' } });
  fireEvent.change(contact, { target: { value: 'mailto:help@acme.example' } });
  fireEvent.change(phone, { target: { value: '+1 (555) 010-2030' } });
  await waitFor(() => {
    const sent = calls.filter((call) => call.method === 'POST').at(-1)?.body as { theme: Record<string, unknown> };
    expect(sent.theme).toMatchObject({ linkback_url: 'https://acme.example', contact_url: 'mailto:help@acme.example', contact_phone: '+1 (555) 010-2030' });
  });
  expect(website).toHaveAccessibleDescription('Linked from the top of every page. An address starting with https:// or http://.');

  // Save still sends what is written, and the server decides.
  fireEvent.change(website, { target: { value: 'https:' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save brand' }));
  await waitFor(() => expect(website).toHaveAttribute('aria-invalid', 'true'));
  expect((calls.find((call) => call.method === 'PUT')?.body as { linkback_url: string }).linkback_url).toBe('https:');
});

test('editing a field a refused save named clears its message there, shows its live hint again, and leaves the focus where the person types', async () => {
  const text = 'Text on the background is 2.85:1; it needs 4.5:1. #5c5c5c passes.';
  const link = 'Links on the background is 3:1; it needs 4.5:1.';
  serve({
    'GET /api/v1/help-center/theme': () => json(200, saved),
    'POST /api/v1/help-center/theme/preview': () => json(200, preview),
    'PUT /api/v1/help-center/theme': () => json(422, { error: { code: 'contrast_too_low', message: `${text} Nothing was saved.`, fields: { 'colors.text': text, 'colors.link': link, linkback_url: 'Send an absolute http or https address.' } } }),
  });
  open(true);
  const textColor = await screen.findByRole('textbox', { name: 'Text color' });
  fireEvent.change(textColor, { target: { value: '#999999' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save brand' }));
  const website = screen.getByRole('textbox', { name: 'Your website' });
  await waitFor(() => expect(website).toHaveFocus());

  fireEvent.change(textColor, { target: { value: '#5c5c5c' } });
  textColor.focus();
  expect(textColor).not.toHaveAttribute('aria-invalid');
  await waitFor(() => expect(textColor).toHaveAccessibleDescription('Contrast 15.51:1 (needs 4.5:1)'));
  // The others keep theirs until they are edited, and the focus stays in the field being typed in.
  expect(screen.getByRole('textbox', { name: 'Link color' })).toHaveAttribute('aria-invalid', 'true');
  expect(website).toHaveAttribute('aria-invalid', 'true');
  expect(textColor).toHaveFocus();

  fireEvent.change(website, { target: { value: 'https://acme.example' } });
  expect(website).not.toHaveAttribute('aria-invalid');
  expect(website).toHaveAccessibleDescription('Linked from the top of every page. An address starting with https:// or http://.');
});

test('the preview waits for the saved brand: no request for the default look while it loads', async () => {
  let loaded: (response: Response) => void = () => {};
  const calls = serve({
    'GET /api/v1/help-center/theme': () => new Promise<Response>((resolve) => { loaded = resolve; }),
    'POST /api/v1/help-center/theme/preview': () => json(200, preview),
  });
  open(true);
  await new Promise((resolve) => setTimeout(resolve, 450));
  expect(calls.filter((call) => call.method === 'POST')).toHaveLength(0);
  loaded(json(200, saved));
  await waitFor(() => expect(calls.filter((call) => call.method === 'POST')).toHaveLength(1));
  expect((calls.find((call) => call.method === 'POST')?.body as { theme: { linkback_url: string } }).theme.linkback_url).toBe('https://acme.example/');
});

const cssRefusal = (message: string) => json(422, { error: { code: 'custom_css_refused', message: `The custom CSS was refused: ${message}. Nothing was saved.`, fields: { custom_css: message } } });

test('custom CSS sits under Advanced, is saved as written, and a refusal opens it with the message there and the caret at the reported line', async () => {
  const message = 'line 2, column 14: url() is not allowed';
  const calls = serve({
    'GET /api/v1/help-center/theme': () => json(200, saved),
    'POST /api/v1/help-center/theme/preview': () => json(200, preview),
    'PUT /api/v1/help-center/theme': () => cssRefusal(message),
  });
  open(true);
  await screen.findByRole('textbox', { name: 'Text color' });
  const css = screen.getByRole('textbox', { name: 'Custom CSS' });
  expect(css).not.toBeVisible();
  const advanced = screen.getByText('Advanced');
  fireEvent.click(advanced);
  expect(css).toBeVisible();
  expect(css).toHaveAccessibleDescription(expect.stringContaining('The contrast checks above do not cover it'));
  const first = '.masthead{border-bottom:2px solid #0b5cad}\n';
  const written = `${first}a{background:url(x)}\n`;
  fireEvent.change(css, { target: { value: written } });
  // Folded again: the refusal must open it to show where.
  fireEvent.click(advanced);
  expect(css).not.toBeVisible();
  fireEvent.click(screen.getByRole('button', { name: 'Save brand' }));
  await waitFor(() => expect(css).toHaveFocus());
  expect(css).toBeVisible();
  expect(css).toHaveAttribute('aria-invalid', 'true');
  expect(css).toHaveAccessibleDescription(message);
  expect((css as HTMLTextAreaElement).selectionStart).toBe(first.length + 13);
  expect((calls.find((call) => call.method === 'PUT')?.body as { custom_css: string }).custom_css).toBe(written);
});

test('the preview shows custom CSS as it is typed, and while the server refuses it keeps the last page and says why at the field', async () => {
  const message = 'line 1, column 14: url() is not allowed';
  const calls = serve({
    'GET /api/v1/help-center/theme': () => json(200, { ...saved, custom_css: '.masthead{border-bottom:2px solid #0b5cad}' }),
    'POST /api/v1/help-center/theme/preview': (call) => ((call.body as { theme: { custom_css: string } }).theme.custom_css.includes('url(') ? cssRefusal(message) : json(200, preview)),
  });
  open(true);
  // Saved custom CSS shows unfolded.
  const css = await screen.findByRole('textbox', { name: 'Custom CSS' });
  expect(css).toBeVisible();
  await screen.findByTitle('Preview of the help center');
  fireEvent.change(css, { target: { value: 'a{background:url(x)}' } });
  await waitFor(() => expect(css).toHaveAccessibleDescription(`Not in the preview yet: ${message}`));
  expect(css).not.toHaveAttribute('aria-invalid');
  expect(screen.getByTitle('Preview of the help center')).toBeInTheDocument();
  expect(screen.getByRole('alert')).toHaveTextContent('The preview keeps the last custom CSS it could show.');
  expect((calls.filter((call) => call.method === 'POST').at(-1)?.body as { theme: { custom_css: string } }).theme.custom_css).toBe('a{background:url(x)}');
});
