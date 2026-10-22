import type { APIResponse, Page } from '@playwright/test';
import { servers } from './cli';
import type { IdentityProvider } from './identity-provider';

// Every sign-up comes from its own client address (the server trusts X-Forwarded-For from this host),
// so repeated runs against one database never meet the three-per-address limit.
export function newClientAddress() {
  return `10.${Math.floor(Math.random() * 256)}.${Math.floor(Math.random() * 256)}.${1 + Math.floor(Math.random() * 254)}`;
}

// The sign-up form sends the three onboarding answers with the name; the journeys answer as a team leaving Freshdesk.
export const answers = { goal: 'leave_helpdesk', role: 'support_lead', source: 'email' };

// signUp starts a trial on the cloud server as the sign-up page does once the identity provider verified the email.
export function signUp(page: Page, provider: IdentityProvider, email: string, workspaceName: string): Promise<APIResponse> {
  return page.request.post(`${servers.cloud}/api/v1/auth/cloud/signup`, {
    headers: { Origin: servers.cloud, Authorization: `Bearer ${provider.token({ email })}`, 'X-Forwarded-For': newClientAddress() },
    data: { workspace_name: workspaceName, ...answers },
  });
}
