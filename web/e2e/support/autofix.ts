import type { ChildProcess } from 'node:child_process';
import { mkdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { ports, scratchDir } from './checkout';
import { startStandIn } from './stand-in';

// The local stand-ins Autofix talks to: GitHub (server/cmd/github-stand-in: the App's install and authorize
// pages, tokens, repositories, issues, pull requests, and the webhooks it signs) and Cursor's Cloud Agents API
// (server/cmd/cursor-stand-in). An end-to-end build of collie reaches them through COLLIEDESK_E2E_GITHUB_URL and
// COLLIEDESK_E2E_CURSOR_URL; no GitHub App, account or Cursor key is involved.
const origin = `http://localhost:${ports.autofix}`;
const keyFile = join(scratchDir, 'autofix-github-app.pem');
const app = { id: '4242', slug: 'colliedesk-e2e', clientID: 'Iv1.e2e-autofix', clientSecret: 'e2e-autofix-client-secret', webhookSecret: 'e2e-autofix-webhook-secret' };

export const githubStandIn = { url: `http://127.0.0.1:${ports.githubStandIn}` };
export const cursorStandIn = { url: `http://127.0.0.1:${ports.cursorStandIn}`, key: 'cursor-e2e-key-0123456789abcdef' };

// startAutofixStandIns runs both stand-ins; the GitHub one writes the App key the server signs with.
export async function startAutofixStandIns(): Promise<ChildProcess[]> {
  mkdirSync(scratchDir, { recursive: true });
  const github = await startStandIn('github-stand-in', ['-listen', `127.0.0.1:${ports.githubStandIn}`, '-key', keyFile, '-app-id', app.id, '-slug', app.slug,
    '-client-id', app.clientID, '-client-secret', app.clientSecret, '-webhook-secret', app.webhookSecret,
    '-callback-url', `${origin}/api/v1/autofix/github/callback`, '-webhook-url', `${origin}/api/v1/webhooks/github`], `${githubStandIn.url}/stand-in/state`);
  try {
    return [github, await startStandIn('cursor-stand-in', ['-listen', `127.0.0.1:${ports.cursorStandIn}`, '-key', cursorStandIn.key], `${cursorStandIn.url}/stand-in/state`)];
  } catch (error) {
    github.kill('SIGTERM');
    throw error;
  }
}

// autofixEnv is the environment of a server whose GitHub App and Cursor are the stand-ins.
export function autofixEnv(): Record<string, string> {
  return {
    APP_ORIGIN: origin, GITHUB_APP_ID: app.id, GITHUB_APP_SLUG: app.slug, GITHUB_APP_PRIVATE_KEY: readFileSync(keyFile, 'utf8'),
    GITHUB_APP_CLIENT_ID: app.clientID, GITHUB_APP_CLIENT_SECRET: app.clientSecret, GITHUB_APP_WEBHOOK_SECRET: app.webhookSecret,
    COLLIEDESK_E2E_GITHUB_URL: githubStandIn.url, COLLIEDESK_E2E_CURSOR_URL: cursorStandIn.url,
  };
}

async function post(base: string, path: string, body: unknown): Promise<Response> {
  const response = await fetch(`${base}${path}`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
  if (!response.ok) throw new Error(`stand-in ${path}: ${response.status} ${await response.text()}`);
  return response;
}

// github arranges GitHub's side and plays GitHub's webhooks.
export const github = {
  installation: (body: { id: number; account: string; users: string[]; repository_selection?: 'selected' | 'all' }) => post(githubStandIn.url, '/stand-in/installations', body),
  repo: (body: { installation_id: number; id: number; full_name: string; visibility?: 'private' | 'public' | 'internal' }) => post(githubStandIn.url, '/stand-in/repos', body),
  copilot: (repoID: number, on: boolean) => post(githubStandIn.url, '/stand-in/copilot', { repo_id: repoID, on }),
  fail: (route: string, status: number) => post(githubStandIn.url, '/stand-in/fail', { route, status }),
  pull: (body: { repo_id: number; number: number; state?: 'open' | 'closed'; merged?: boolean; merge_sha?: string; merged_by?: string; head_ref?: string; closes?: number[] }) =>
    post(githubStandIn.url, '/stand-in/pulls', body),
  // pullEvent delivers pull_request for a pull request set with pull(); the answer is the server's.
  pullEvent: async (body: { action: string; installation_id: number; repo_id: number; number: number }) => (await post(githubStandIn.url, '/stand-in/webhook/pull_request', body)).json(),
  issueEvent: async (body: { action: string; installation_id: number; repo_id: number; number: number; assignee?: string }) => (await post(githubStandIn.url, '/stand-in/webhook/issues', body)).json(),
  issues: async (repoID: number) => await (await fetch(`${githubStandIn.url}/stand-in/issues?repo_id=${repoID}`)).json() as CreatedIssue[],
  state: async () => await (await fetch(`${githubStandIn.url}/stand-in/state`)).json() as { user_tokens: number; revoked_tokens: number },
};

export interface CreatedIssue { number: number; title: string; body: string; author: string; state: string; comments: { body: string; author: string }[]; assignees: string[] }

// cursor scripts the stand-in's next runs and reads what it received.
export const cursor = {
  script: (steps: { status: string; result?: string; branch?: string; pr_url?: string }[]) => post(cursorStandIn.url, '/stand-in/script', steps),
  state: async () => await (await fetch(`${cursorStandIn.url}/stand-in/state`)).json() as { creates: { agent_id: string; repo: string; prompt: string }[]; cancels: string[]; agents: number },
};
