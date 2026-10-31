import { createHash } from 'node:crypto';
import { createServer } from 'node:http';
import type { IncomingMessage, Server, ServerResponse } from 'node:http';
import type { AddressInfo } from 'node:net';

// A local stand-in of Zalo's OA API for end-to-end builds (server/cmd/collie/e2e_zalo.go): start it, then
// start the server with COLLIEDESK_E2E_ZALO_URL set to its url, so what a Zalo channel tells a customer
// lands here and a test reads it. It answers only what the channel type calls, as Zalo documents it:
//   POST /v3.0/oa/message/cs        one to one          -> {"error":0,"data":{"message_id":"fz-<n>"}}
//   POST /v3.0/oa/group/message     a group             -> the same
//   POST /v2.0/oa/upload/image      a picture of a reply -> {"error":0,"data":{"attachment_id":"fz-att-<n>"}}
//   POST /v4/oa/access_token        a token refresh     -> a new access and refresh token
//   GET  /v3.0/oa/user/detail       a sender's profile  -> display_name from names (else Zalo's -213)
//   GET  /v3.0/oa/group/listmember  a group's members   -> those in names
// Any access token is taken. Its customer events come from the test itself, signed with signZaloEvent and
// posted to the server's webhook (POST /api/v1/channels/zalo/webhook).

export interface ZaloSent {
  path: string;
  recipient: { user_id?: string; group_id?: string };
  text?: string;
  quote?: string;
  at: string;
}

export interface FakeZalo {
  url: string;
  // sent is every message the server sent, oldest first.
  sent: () => ZaloSent[];
  stop: () => Promise<void>;
}

// startFakeZalo listens on port (ports.zalo of checkout.ts, or 0 for any) and names senders from names
// (Zalo user id -> display name).
export async function startFakeZalo(port = 0, names: Record<string, string> = {}): Promise<FakeZalo> {
  const sent: ZaloSent[] = [];
  let ids = 0;
  const answer = (response: ServerResponse, body: unknown) => {
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify(body));
  };
  const server: Server = createServer((request: IncomingMessage, response: ServerResponse) => {
    const chunks: Buffer[] = [];
    request.on('data', (chunk: Buffer) => chunks.push(chunk));
    request.on('end', () => {
      const url = new URL(request.url ?? '/', 'http://zalo');
      const body = Buffer.concat(chunks).toString('utf8');
      if (request.method === 'GET' && url.pathname === '/ready') return answer(response, { ok: true });
      if (request.method === 'POST' && (url.pathname === '/v3.0/oa/message/cs' || url.pathname === '/v3.0/oa/group/message')) {
        const call = JSON.parse(body || '{}') as { recipient?: ZaloSent['recipient']; message?: { text?: string; quote_message_id?: string } };
        sent.push({ path: url.pathname, recipient: call.recipient ?? {}, text: call.message?.text, quote: call.message?.quote_message_id, at: new Date().toISOString() });
        return answer(response, { error: 0, message: 'Success', data: { message_id: `fz-${++ids}` } });
      }
      if (request.method === 'POST' && url.pathname === '/v2.0/oa/upload/image') return answer(response, { error: 0, data: { attachment_id: `fz-att-${++ids}` } });
      if (request.method === 'POST' && url.pathname === '/v4/oa/access_token') {
        return answer(response, { access_token: `fz-access-${++ids}`, refresh_token: `fz-refresh-${ids}`, expires_in: '90000' });
      }
      if (request.method === 'GET' && url.pathname === '/v3.0/oa/user/detail') {
        const asked = JSON.parse(url.searchParams.get('data') ?? '{}') as { user_id?: string };
        const name = names[asked.user_id ?? ''];
        return answer(response, name ? { error: 0, data: { user_id: asked.user_id, display_name: name } } : { error: -213, message: 'User has not followed the OA' });
      }
      if (request.method === 'GET' && url.pathname === '/v3.0/oa/group/listmember') {
        const members = Object.entries(names).map(([user_id, name]) => ({ user_id, name }));
        return answer(response, { error: 0, data: { total: members.length, members } });
      }
      response.writeHead(404, { 'Content-Type': 'application/json' });
      response.end(JSON.stringify({ error: -201, message: `the stand-in does not serve ${request.method} ${url.pathname}` }));
    });
  });
  await new Promise<void>((resolve) => server.listen(port, '127.0.0.1', resolve));
  const { port: bound } = server.address() as AddressInfo;
  return {
    url: `http://127.0.0.1:${bound}`,
    sent: () => sent.map((item) => ({ ...item })),
    stop: () => new Promise<void>((resolve) => server.close(() => resolve())),
  };
}

// zaloEvent is a customer's message as Zalo delivers it: one to one, or in a group with groupOf (the
// OA's id then names the OA). id is the message's msg_id.
export function zaloEvent(appID: string, oa: string, sender: string, id: string, text: string, group?: string, at = new Date()): string {
  const name = group ? 'user_send_group_text' : 'user_send_text';
  const body: Record<string, unknown> = { app_id: appID, event_name: name, timestamp: String(at.getTime()),
    sender: { id: sender }, recipient: { id: group ?? oa }, message: { msg_id: id, text, attachments: [] } };
  if (group) body.oa_id = oa;
  return JSON.stringify(body);
}

// signZaloEvent is the X-ZEvent-Signature header of body, as Zalo computes it (server/internal/channels/zalo/webhook.go Sign).
export function signZaloEvent(appID: string, body: string, oaSecretKey: string): string {
  const timestamp = String((JSON.parse(body) as { timestamp?: string }).timestamp ?? '');
  return 'mac=' + createHash('sha256').update(appID + body + timestamp + oaSecretKey).digest('hex');
}
