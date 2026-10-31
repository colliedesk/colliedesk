import { readdirSync, readFileSync } from 'node:fs';
import path from 'node:path';
import { renderHook, waitFor } from '@testing-library/react';
import { expect, test } from 'vitest';
import { channelSchema, knownChannels } from '../api/schemas';
import { channelLabel } from '../features/inbox/filter-messages';
import { channelTypes } from './registry';

// The web side of "a new channel type is complete": every type the API names has a registry entry with its
// words, icon and panels, and the shared pages never name a type (src/channels/registry.ts is the one list).

const openapi = readFileSync(path.resolve(process.cwd(), '../contracts/openapi.yaml'), 'utf8');
const apiTypes = /\n {4}ChannelType:\n[\s\S]*?Shipped types: ([a-z_, ]+)\./.exec(openapi)?.[1].split(',').map((type) => type.trim()) ?? [];

test('the registry has exactly the channel types of the API, once each', () => {
  expect(apiTypes.length).toBeGreaterThan(0);
  expect(channelTypes.map((ui) => ui.type).sort()).toEqual([...apiTypes].sort());
  expect(new Set(channelTypes.map((ui) => ui.type)).size).toBe(channelTypes.length);
});

test('every capability code the API documents is put in words by a type that carries it', () => {
  const codes = /Fixed codes of what the channel carries \(([^)]*)\)/.exec(openapi)?.[1].split(',').map((code) => code.trim()) ?? [];
  expect(codes.length).toBeGreaterThan(0);
  for (const code of codes) expect(channelTypes.some((ui) => ui.capabilityLabel(code)), code).toBe(true);
});

test.each(channelTypes.map((ui) => [ui.type, ui] as const))('%s has its words, its icon, and a panel for every tab it declares', async (_type, ui) => {
  expect(ui.label).not.toBe('');
  expect(ui.description).not.toBe('');
  expect(ui.connectLabel).not.toBe('');
  expect(typeof ui.Icon).toBe('function');
  expect(ui.tabs.length).toBeGreaterThan(0);
  // History is the shared tab every channel page ends with.
  expect(ui.tabs.map((tab) => tab.id)).not.toContain('history');
  const { result } = renderHook(() => ui.panels.use());
  await waitFor(() => expect(result.current.status).toBe('ready'));
  const panels = result.current.status === 'ready' ? result.current.module : undefined;
  for (const tab of ui.tabs) expect(typeof panels?.Tabs[tab.id], tab.id).toBe('function');
  expect(typeof panels?.Connect).toBe('function');
});

test('the shared channel pages name no channel type', () => {
  const folder = path.resolve(process.cwd(), 'src/features/settings/channels');
  const files = readdirSync(folder).filter((name) => /\.(ts|tsx)$/.test(name) && !/\.test\./.test(name));
  expect(files.length).toBeGreaterThan(0);
  for (const name of files) {
    const text = readFileSync(path.join(folder, name), 'utf8');
    for (const type of apiTypes) expect(text, `${name} names ${type}`).not.toMatch(new RegExp(`['"\`]${type}['"\`]`));
  }
});

// How a conversation started: the contract's ConversationChannel is any channel type's name, and documents
// the values known today; the Filter menu offers those (knownChannels), each with its words, and a chat
// type's name is still parsed, so it never fails the whole inbox list.
test('the known conversation channels are the ones the contract documents, each has a label, and another type still parses', () => {
  const block = (/\n {4}ConversationChannel:\n([\s\S]*?)\n {4}[A-Z]/.exec(openapi)?.[1] ?? '').replace(/\s+/g, ' ');
  const documented = /Values: ([a-z_, ]+)\./.exec(block)?.[1].split(',').map((value) => value.trim()) ?? [];
  expect(documented.length).toBeGreaterThan(0);
  expect([...knownChannels].sort()).toEqual([...documented].sort());
  for (const value of knownChannels) expect(channelLabel(value), value).not.toBe(value);
  expect(channelSchema.safeParse('zalo').success).toBe(true);
  expect(channelSchema.safeParse('Not a type').success).toBe(false);
});
