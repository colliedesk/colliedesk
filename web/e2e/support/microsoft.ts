import { spawn, spawnSync } from 'node:child_process';
import type { ChildProcess } from 'node:child_process';
import { mkdirSync } from 'node:fs';
import { join } from 'node:path';
import { ports, scratchDir } from './checkout';
import { goEnv } from './cli';

// The local stand-in for Microsoft's sign-in, token endpoint, admin consent page and Graph
// (server/cmd/microsoft-stand-in). An end-to-end build of collie reaches it through
// COLLIEDESK_E2E_MICROSOFT_URL; no Microsoft account or credential is involved.
export const microsoftStandIn = {
  url: `http://127.0.0.1:${ports.microsoftStandIn}`,
  clientID: 'e2e-microsoft-client',
  clientSecret: 'e2e-microsoft-secret',
  // The environment a server sends Microsoft 365 replies with, through the stand-in.
  env(): Record<string, string> {
    return { MICROSOFT_OAUTH_CLIENT_ID: this.clientID, MICROSOFT_OAUTH_CLIENT_SECRET: this.clientSecret, COLLIEDESK_E2E_MICROSOFT_URL: this.url };
  },
};

export interface SentMessage { account: string; message_id: string; raw: string }

// startMicrosoftStandIn builds and runs the stand-in, resolving once it answers.
export async function startMicrosoftStandIn(): Promise<ChildProcess> {
  const bin = join(scratchDir, 'bin', 'microsoft-stand-in');
  mkdirSync(join(scratchDir, 'bin'), { recursive: true });
  const built = spawnSync('go', ['build', '-o', bin, './cmd/microsoft-stand-in'], { cwd: '../server', encoding: 'utf8', env: { ...process.env, ...goEnv } });
  if (built.status !== 0) throw new Error(`go build microsoft-stand-in failed: ${built.stderr}`);
  const child = spawn(bin, ['-listen', `127.0.0.1:${ports.microsoftStandIn}`, '-client-id', microsoftStandIn.clientID, '-client-secret', microsoftStandIn.clientSecret],
    { stdio: ['ignore', 'inherit', 'inherit'] });
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline) {
    if (child.exitCode !== null) throw new Error(`microsoft-stand-in exited with ${child.exitCode}`);
    try {
      if ((await fetch(`${microsoftStandIn.url}/stand-in/messages`)).ok) return child;
    } catch { /* not listening yet */ }
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
  child.kill('SIGTERM');
  throw new Error('microsoft-stand-in did not start');
}

export async function sentThroughMicrosoft(): Promise<SentMessage[]> {
  return await (await fetch(`${microsoftStandIn.url}/stand-in/messages`)).json() as SentMessage[];
}

// standIn plays the person or Microsoft: revoke-all (access removed in My Apps), expire (90 days unused).
export async function standIn(action: 'revoke-all' | 'expire'): Promise<void> {
  const response = await fetch(`${microsoftStandIn.url}/stand-in/${action}`, { method: 'POST' });
  if (!response.ok) throw new Error(`stand-in ${action}: ${response.status}`);
}
