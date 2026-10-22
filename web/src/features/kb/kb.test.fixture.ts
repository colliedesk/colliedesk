import { vi } from 'vitest';

// Fixtures and a fake server for the knowledge base tests: the real calls and schemas run against it.
export const ids = {
  guides: '11111111-1111-4111-8111-111111111111',
  setup: '22222222-2222-4222-8222-222222222222',
  deep: '33333333-3333-4333-8333-333333333333',
  billing: '44444444-4444-4444-8444-444444444444',
  article: '55555555-5555-4555-8555-555555555555',
  image: '66666666-6666-4666-8666-666666666666',
};

export const collection = (id: string, name: string, parent: string | null, count = 0, slug = name.toLowerCase()) => ({
  id, parent_id: parent, name, slug, description: '', position: 0, article_count: count, created_at: '2026-10-01T00:00:00Z', updated_at: '2026-10-01T00:00:00Z',
});

export const collections = [
  collection(ids.guides, 'Guides', null, 1),
  collection(ids.setup, 'Setup', ids.guides, 2),
  collection(ids.deep, 'Email', ids.setup, 0),
  collection(ids.billing, 'Billing', null, 0),
];

export const change = { version: 1, kind: 'create', restored_from_version: null, changed_by: { agent_id: null, name: 'Ada' }, token: null, agent_request_id: null, created_at: '2026-10-02T00:00:00Z' };

export const summary = (n: number, title: string, status: 'draft' | 'published' = 'draft') => ({
  id: `00000000-0000-4000-8000-${String(n).padStart(12, '0')}`, number: n, collection_id: ids.setup, position: 0, title, slug: title.toLowerCase().replace(/ /g, '-'), status,
  published_at: status === 'published' ? '2026-10-02T00:00:00Z' : null, author_agent_id: null, created_at: '2026-10-02T00:00:00Z', updated_at: '2026-10-02T00:00:00Z', last_change: change,
  url: `https://desk.example.com/help/acme/a/${n}-${title.toLowerCase().replace(/ /g, '-')}`,
});

export const article = (fields: Record<string, unknown> = {}) => ({
  ...summary(7, 'Reset your password'), id: ids.article, body_md: 'Open **Settings**.', has_source_html: false, translations: [], deleted_at: null, ...fields,
});

export const me = {
  agent: { id: '77777777-7777-4777-8777-777777777777', email: 'ada@example.com', display_name: 'Ada', role: 'admin', timezone_override: null },
  workspace: { id: '88888888-8888-4888-8888-888888888888', slug: 'acme', name: 'Acme', timezone: 'UTC' }, timezone: 'UTC', scope: null, plan: null,
};

export const json = (status: number, body?: unknown) => new Response(body === undefined ? null : JSON.stringify(body), { status });
export const refusal = (status: number, code: string, message: string) => json(status, { error: { code, message } });

export interface Call { method: string; path: string; search: URLSearchParams; body: unknown; headers: Record<string, string> }

// serve answers each call with the first handler whose "METHOD /path" matches, and records every call.
export function serve(handlers: Record<string, (call: Call) => Response | Promise<Response>>) {
  const calls: Call[] = [];
  const fetchMock = vi.fn(async (input: string, init: RequestInit = {}) => {
    const url = new URL(input, 'http://localhost');
    const method = init.method ?? 'GET';
    const headers = (init.headers ?? {}) as Record<string, string>;
    const body = typeof init.body === 'string' ? JSON.parse(init.body) as unknown : init.body;
    const call = { method, path: url.pathname, search: url.searchParams, body, headers };
    calls.push(call);
    const handler = handlers[`${method} ${url.pathname}`];
    return handler ? handler(call) : json(404, { error: { code: 'not_found', message: 'Not found' } });
  });
  vi.stubGlobal('fetch', fetchMock);
  return calls;
}
