import type { APIRequestContext } from '@playwright/test';
import type { ChildProcess } from 'node:child_process';
import { basename } from 'node:path';
import { test } from '@playwright/test';
import { checkoutID, portBase } from './checkout';
import { databaseURL, mustColliedesk, psql, startServer, stopServer, withDatabase } from './cli';

// The loopback chat type of end-to-end builds (server/cmd/collie/e2e_chat.go): no shipped type is a chat
// yet, so a journey that needs cards of the intake queue gets them from this stand-in, through the real
// webhook, journal worker and rules. Its groups collect messages for intake and close a batch after 2
// quiet seconds; several customers write in a group.
//
// It runs on a server of its own, on a database of its own: the web app knows only the shipped channel
// types (Settings → Channels does not load beside an unknown one), and every server on a database
// processes what any channel of it received, so the shared servers must never meet a loopback channel.
// Each spec file gets a port of its own and a database named after it: CI runs files in parallel, and one
// file's afterAll must never stop the server or drop the database another file is using. The ports are
// above every checkout's fixed range (checkout.ts) and below the system's ephemeral ones, so no other
// spec and no outgoing connection ever takes one.

export interface ChatServer { origin: string; databaseURL: string; stop: () => Promise<void> }

export interface LoopbackMessage { id: string; from: string; name?: string; text: string; group?: string; reference?: string }

// startChatServer migrates a fresh database, lets workspaces add loopback channels (the type list is the
// operator's, written as the database owner) and starts `collie serve` with the type on it, and env (for
// example AI_ALLOW_PRIVATE_BASE_URLS for a stand-in model on this machine). Call stop in afterAll.
export async function startChatServer(env: Record<string, string> = {}): Promise<ChatServer> {
  const port = chatPort(test.info().file);
  const database = `${new URL(databaseURL).pathname.slice(1)}_chat_${port}`;
  const url = withDatabase(databaseURL, database);
  const origin = `http://localhost:${port}`;
  const admin = withDatabase(databaseURL, 'postgres');
  psql(`DROP DATABASE IF EXISTS ${database} WITH (FORCE)`, admin);
  psql(`CREATE DATABASE ${database}`, admin);
  mustColliedesk(['migrate'], { databaseURL: url, origin });
  psql(`INSERT INTO channel_types(type,connectable) VALUES('loopback',true) ON CONFLICT (type) DO NOTHING`, url);
  const server: ChildProcess = await startServer(port, { ...env, DATABASE_URL: url, APP_ORIGIN: origin, COLLIEDESK_E2E_CHAT: '1' }, 'e2e');
  return {
    origin, databaseURL: url,
    stop: async () => {
      await stopServer(server);
      psql(`DROP DATABASE IF EXISTS ${database} WITH (FORCE)`, admin);
    },
  };
}

// addLoopbackChannel adds a loopback channel named by its account, as the signed-in admin of request,
// and returns its id.
export async function addLoopbackChannel(request: APIRequestContext, origin: string, account: string): Promise<string> {
  const response = await request.post(`${origin}/api/v1/channels`, { headers: { Origin: origin }, data: { type: 'loopback', config: { account } } });
  if (response.status() !== 201) throw new Error(`add loopback channel: ${response.status()} ${await response.text()}`);
  return (await response.json() as { id: string }).id;
}

// holdGroupsForReview makes the channel's rules hold what a group says without a command for a person,
// so each batch is a card waiting in the queue.
export async function holdGroupsForReview(request: APIRequestContext, origin: string, channelID: string): Promise<void> {
  const response = await request.patch(`${origin}/api/v1/channels/${channelID}`, { headers: { Origin: origin },
    data: { settings: { scopes: { group: { gather: { quiet_seconds: 2, max_seconds: 20 }, review: true } } } } });
  if (!response.ok()) throw new Error(`hold groups for review: ${response.status()} ${await response.text()}`);
}

// sendLoopback delivers what customers wrote on the channel of account to its webhook (public, no
// secret: the account names the channel). Messages with a group are said in that group.
export async function sendLoopback(request: APIRequestContext, origin: string, account: string, messages: LoopbackMessage[]): Promise<void> {
  const response = await request.post(`${origin}/api/v1/channels/loopback/webhook`, { data: { account, events: messages } });
  if (!response.ok()) throw new Error(`loopback webhook: ${response.status()} ${await response.text()}`);
}

// waitForCards polls the queue (state review by default) until it holds count cards of the channel, and
// returns their ids, oldest first. A batch closes 2 seconds after its last message and the journal
// worker runs every 2 seconds.
export async function waitForCards(request: APIRequestContext, origin: string, channelID: string, count: number, state = 'review'): Promise<string[]> {
  const deadline = Date.now() + 30_000;
  for (;;) {
    const response = await request.get(`${origin}/api/v1/intake/gatherings?state=${state}&channel_id=${channelID}`);
    const ids = response.ok() ? (await response.json() as { items: { id: string }[] }).items.map((card) => card.id) : [];
    if (ids.length >= count) return ids;
    if (Date.now() > deadline) throw new Error(`waited for ${count} ${state} cards of ${channelID}, found ${ids.length}`);
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
}

// The spec files that start a chat server, each on its own slot; another file takes one of the rest.
const chatSlots = ['intake-queue', 'message-moves', 'review', 'review-ai', 'channel-ai-intake'];

// chatPort is the port of the chat server of a spec file: 20 per checkout from 28000 (COLLIEDESK_E2E_PORT_BASE
// moves them right after its fixed range).
function chatPort(file: string): number {
  const base = process.env.COLLIEDESK_E2E_PORT_BASE ? portBase + 20 : 28_000 + (parseInt(checkoutID, 16) % 100) * 20;
  const name = basename(file).replace(/\.spec\.ts$/, '');
  let slot = chatSlots.indexOf(name);
  if (slot < 0) slot = chatSlots.length + ([...name].reduce((sum, char) => sum + char.charCodeAt(0), 0) % (20 - chatSlots.length));
  return base + slot;
}
