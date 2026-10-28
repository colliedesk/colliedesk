import type { ChildProcess } from 'node:child_process';
import { ports } from './checkout';
import { startStandIn } from './stand-in';

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
  return await startStandIn('microsoft-stand-in', ['-listen', `127.0.0.1:${ports.microsoftStandIn}`, '-client-id', microsoftStandIn.clientID,
    '-client-secret', microsoftStandIn.clientSecret], `${microsoftStandIn.url}/stand-in/messages`);
}

export async function sentThroughMicrosoft(): Promise<SentMessage[]> {
  return await (await fetch(`${microsoftStandIn.url}/stand-in/messages`)).json() as SentMessage[];
}

// standIn plays the person or Microsoft: revoke-all (access removed in My Apps), expire (90 days unused).
export async function standIn(action: 'revoke-all' | 'expire'): Promise<void> {
  const response = await fetch(`${microsoftStandIn.url}/stand-in/${action}`, { method: 'POST' });
  if (!response.ok) throw new Error(`stand-in ${action}: ${response.status}`);
}
