import { readFileSync } from 'node:fs';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, expect, test, vi } from 'vitest';
import { InvalidRequestError, UnauthorizedError } from '../../api/client';
import { OAuthConsent, oauthRequestFrom } from './oauth-consent';

const api = vi.hoisted(() => ({ describeOAuthClient: vi.fn(), me: vi.fn(), config: vi.fn(), allowOAuthClient: vi.fn() }));
vi.mock('../../api/client', async (actual) => ({ ...(await actual<object>()), api }));
const leaveTo = vi.hoisted(() => vi.fn());
vi.mock('../../lib/leave-to', () => ({ leaveTo }));

const request = oauthRequestFrom(new URLSearchParams({
  client_id: 'cdk_client_x', redirect_uri: 'http://localhost:4567/callback', response_type: 'code', code_challenge: 'c'.repeat(43),
  code_challenge_method: 'S256', state: 'st', resource: 'https://desk.example.com/mcp',
}));
const app = { client_id: 'cdk_client_x', name: 'Claude Code', redirect_uri: 'http://localhost:4567/callback', host: 'localhost', client_host: '', verified: false };
const me = { agent: { email: 'ada@example.com', display_name: 'Ada' }, workspace: { name: 'Acme Support' }, plan: null };

function show() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}><OAuthConsent request={request} here="/oauth/authorize?client_id=cdk_client_x" /></QueryClientProvider>);
}

beforeEach(() => {
  vi.clearAllMocks();
  api.describeOAuthClient.mockResolvedValue(app);
  api.me.mockResolvedValue(me);
  api.config.mockResolvedValue({ app_origin: 'https://desk.example.com', cloud_sign_in: { enabled: false } });
});

test('a signed-in person sees the app, the workspace and themselves, and Allow takes the code back to the app', async () => {
  api.allowOAuthClient.mockResolvedValue({ redirect_to: 'http://localhost:4567/callback?code=cdk_oac_1&state=st' });
  show();
  expect(await screen.findByRole('heading', { name: 'Allow Claude Code to work in Colliedesk?' })).toBeTruthy();
  expect(screen.getByText('It works in Acme Support as Ada, with everything you can do there now.')).toBeTruthy();
  expect(screen.getByText('After your answer you go back to localhost.')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Allow' }));
  await waitFor(() => expect(leaveTo).toHaveBeenCalledWith('http://localhost:4567/callback?code=cdk_oac_1&state=st'));
  expect(api.allowOAuthClient).toHaveBeenCalledWith(expect.objectContaining({ client_id: 'cdk_client_x', code_challenge_method: 'S256', state: 'st' }), expect.any(String));
});

test('Cancel sends the app access_denied with its state, on the redirect the server confirmed', async () => {
  show();
  fireEvent.click(await screen.findByRole('button', { name: 'Cancel' }));
  expect(leaveTo).toHaveBeenCalledWith('http://localhost:4567/callback?error=access_denied&state=st&iss=https%3A%2F%2Fdesk.example.com');
  expect(api.allowOAuthClient).not.toHaveBeenCalled();
});

test('an app the server does not know explains itself and never redirects', async () => {
  api.describeOAuthClient.mockRejectedValue(new InvalidRequestError('The app asked to return to an address it did not register.', { code: 'invalid_redirect_uri' }));
  show();
  expect(await screen.findByRole('alert')).toHaveTextContent('did not register');
  expect(screen.queryByRole('button', { name: 'Allow' })).toBeNull();
  expect(screen.queryByRole('button', { name: 'Cancel' })).toBeNull();
});

test('signed out on a self-hosted server: open the sign-in link here, then come back', async () => {
  api.me.mockRejectedValue(new UnauthorizedError());
  show();
  expect(await screen.findByText('Open your Colliedesk sign-in link in this browser, then come back to this page.')).toBeTruthy();
  expect(screen.getByRole('button', { name: 'I signed in' })).toBeTruthy();
});

test('signed out on Cloud: the sign-in page chooses the workspace and comes back here', async () => {
  api.me.mockRejectedValue(new UnauthorizedError());
  api.config.mockResolvedValue({ app_origin: 'https://app.colliedesk.com', cloud_sign_in: { enabled: true } });
  show();
  const link = await screen.findByRole('link', { name: 'Sign in' });
  expect(link.getAttribute('href')).toBe('https://app.colliedesk.com/sign-in?next=%2Foauth%2Fauthorize%3Fclient_id%3Dcdk_client_x');
});

test('an app published by a client ID metadata document names its host; only a known vendor host is not warned about', async () => {
  api.describeOAuthClient.mockResolvedValue({ ...app, client_id: 'https://evil.example/x', client_host: 'evil.example', name: 'Claude' });
  const { unmount } = show();
  expect(await screen.findByText('Published by evil.example.')).toBeTruthy();
  expect(screen.getByText('The app named itself; check it is the app you just connected.')).toBeTruthy();
  unmount();
  api.describeOAuthClient.mockResolvedValue({ ...app, client_id: 'https://claude.ai/oauth/claude-code-client-metadata', client_host: 'claude.ai', verified: true });
  show();
  expect(await screen.findByText('Published by claude.ai.')).toBeTruthy();
  expect(screen.queryByText('The app named itself; check it is the app you just connected.')).toBeNull();
});

// The page's text and its two buttons keep the app's spacing: the layout stylesheet spaces the actions
// from the text above them and the buttons from each other.
test('Allow and Cancel sit apart from the text and from each other', async () => {
  const style = document.createElement('style');
  style.textContent = readFileSync('src/styles/layout.css', 'utf8');
  document.head.append(style);
  show();
  const actions = (await screen.findByRole('button', { name: 'Allow' })).parentElement!;
  const rules = [...style.sheet!.cssRules].filter((rule): rule is CSSStyleRule => rule instanceof CSSStyleRule && actions.matches(rule.selectorText));
  style.remove();
  expect(rules.some((rule) => rule.style.getPropertyValue('gap'))).toBe(true);
  expect(rules.some((rule) => rule.style.getPropertyValue('margin-top'))).toBe(true);
});
