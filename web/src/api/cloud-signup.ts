import { browserTimeZone } from '../lib/format-date';
import { redeemedSchema } from './schemas';
import type { Generated } from './schemas';

// SignupAnswers are what the sign-up form asks about the team; the server stores them with the workspace.
export interface SignupAnswers {
  goal: Generated<'OnboardingGoal'>;
  role: Generated<'OnboardingRole'>;
  source: Generated<'OnboardingSource'>;
}

// NotAnAgentError means the identity provider vouched for the email but no workspace has that
// person as an agent (HTTP 403 `forbidden` from the cloud exchange): the place to offer sign-up.
export class NotAnAgentError extends Error {
  constructor() {
    super('This email is not an agent of any workspace');
    this.name = 'NotAnAgentError';
  }
}

export type SignupReason = 'trial_already_used' | 'rate_limited' | 'name_not_allowed' | 'slug_unavailable' | 'invalid' | 'signup_unavailable' | 'unavailable';

// SignupError carries why a sign-up was refused; the screen picks its own words per reason.
export class SignupError extends Error {
  readonly reason: SignupReason;

  constructor(reason: SignupReason, message = '') {
    super(message || `sign-up refused: ${reason}`);
    this.name = 'SignupError';
    this.reason = reason;
  }
}

async function signupFailure(response: Response): Promise<SignupError> {
  let code = '';
  let message = '';
  try {
    const body = await response.json() as { error?: { code?: unknown; message?: unknown } };
    if (typeof body.error?.code === 'string') code = body.error.code;
    if (typeof body.error?.message === 'string') message = body.error.message;
  } catch {
    // An unreadable body is mapped from the status alone.
  }
  if (response.status === 409 && code === 'trial_already_used') return new SignupError('trial_already_used', message);
  if (response.status === 409) return new SignupError('slug_unavailable', message);
  if (response.status === 422) return new SignupError('name_not_allowed', message);
  if (response.status === 429) return new SignupError('rate_limited', message);
  if (response.status === 503) return new SignupError('signup_unavailable', message);
  if (response.status === 400) return new SignupError('invalid', message);
  return new SignupError('unavailable');
}

// signUpCloud creates a trial workspace for the verified identity of the bearer token and returns
// the signed-in agent and workspace (the session cookie is set by the response).
export async function signUpCloud(token: string, workspaceName: string, answers?: SignupAnswers) {
  const response = await fetch('/api/v1/auth/cloud/signup', {
    method: 'POST', credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ workspace_name: workspaceName, ...answers, timezone: browserTimeZone() }),
  });
  if (!response.ok) throw await signupFailure(response);
  return redeemedSchema.parse(await response.json());
}
