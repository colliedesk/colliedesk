import { readFileSync } from 'node:fs';
import { fireEvent, render, screen } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import type { Session } from '../../api/schemas';
import { ConnectedAppsSection } from './connected-apps-section';

const session = (change: Partial<Session> = {}): Session => ({
  id: '11111111-1111-4111-8111-111111111111', agent_id: '22222222-2222-4222-8222-222222222222', agent_email: 'ada@example.com', agent_name: 'Ada',
  label: 'Claude Code', token: { kind: 'oauth', role: null, role_held: true, permissions: [], legacy_tier: null }, role: null, permissions: [], current: false,
  created_at: '2026-11-06T09:00:00Z', last_seen_at: '2026-11-06T10:00:00Z', expires_at: '2099-12-06T10:00:00Z', revoked_at: null,
  app: { client_id: 'https://claude.ai/cc.json', user_agent: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15',
    connected_at: '2026-11-06T09:00:00Z', needs_reconnecting: false }, ...change,
});

test('each connected app is a row with Disconnect, and a person session or API token is not listed here', () => {
  const disconnect = vi.fn();
  render(<ConnectedAppsSection sessions={[session(), session({ id: '33333333-3333-4333-8333-333333333333', label: 'Firefox', token: null, app: null })]}
    timezone="UTC" mcpAddress="https://desk.example.com/mcp" onDisconnect={disconnect} />);
  expect(screen.getByText('Claude Code')).toBeTruthy();
  expect(screen.queryByText('Firefox')).toBeNull();
  expect(screen.getByText('Active')).toBeTruthy();
  expect(screen.getByText(/add https:\/\/desk\.example\.com\/mcp in Claude/)).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Disconnect Claude Code' }));
  expect(disconnect).toHaveBeenCalledWith('11111111-1111-4111-8111-111111111111');
});

test('a disconnected or expired app needs reconnecting and says how in each app', () => {
  render(<ConnectedAppsSection sessions={[session({ revoked_at: '2026-11-06T11:00:00Z', app: { ...session().app!, needs_reconnecting: true } })]}
    timezone="UTC" mcpAddress="https://desk.example.com/mcp" />);
  expect(screen.getByText('Needs reconnecting')).toBeTruthy();
  expect(screen.getByText(/codex mcp login colliedesk/)).toBeTruthy();
  expect(screen.queryByRole('button', { name: /Disconnect/ })).toBeNull();
});

test('no app yet says so', () => {
  render(<ConnectedAppsSection sessions={[]} timezone="UTC" mcpAddress="https://desk.example.com/mcp" />);
  expect(screen.getByText('No AI app is connected yet.')).toBeTruthy();
});

// jsdom does not lay text out, so this reads the shipped stylesheet: the last rule of components.css that
// matches the element and sets the property is the one that applies.
function declared(element: Element, property: 'white-space' | 'overflow' | 'text-overflow') {
  const style = document.createElement('style');
  style.textContent = readFileSync('src/styles/components.css', 'utf8');
  document.head.append(style);
  const rules = [...style.sheet!.cssRules].filter((rule): rule is CSSStyleRule => rule instanceof CSSStyleRule);
  style.remove();
  return rules.filter((rule) => element.matches(rule.selectorText) && rule.style.getPropertyValue(property)).at(-1)?.style.getPropertyValue(property);
}

// The steps are the one thing a row that needs reconnecting is for: they wrap, never cut to one line.
test('the reconnect steps wrap in full instead of being cut with an ellipsis', () => {
  render(<ConnectedAppsSection sessions={[session({ revoked_at: '2026-11-06T11:00:00Z', app: { ...session().app!, needs_reconnecting: true } })]}
    timezone="UTC" mcpAddress="https://desk.example.com/mcp" />);
  const steps = screen.getByText(/codex mcp login colliedesk/);
  expect(declared(steps, 'white-space')).not.toBe('nowrap');
  expect(declared(steps, 'text-overflow')).not.toBe('ellipsis');
});
