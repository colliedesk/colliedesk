import type { SetupProgress, SetupStepKey, TokenPermission } from '../../api/schemas';
import { can } from '../../lib/can';
import type { Permissions } from '../../lib/can';

// The state each step ends in. Kept apart from the checklist so the inbox can decide, without loading
// it, whether the guide has anything left to show.
export const doneStates: Record<SetupStepKey, string> = { email: 'working', import: 'done', invite: 'joined', first_reply: 'sent' };

// The permissions a step's own screen needs, as in Settings: managing channels sets up email and the import;
// the invite step lists the invites and the roles (team:read) and creates one (team:invite). Answering the
// first email needs none beyond reading the inbox.
const stepPermissions: Partial<Record<SetupStepKey, TokenPermission[]>> = { email: ['channels:update'], import: ['channels:update'], invite: ['team:invite', 'team:read'] };

// mayTake is true when the person holds every permission the step's screen needs.
export const mayTake = (me: Permissions, key: SetupStepKey) => (stepPermissions[key] ?? []).every((needed) => can(me, needed));

// visibleSteps are the steps this person can take, by the permissions of their roles.
export function visibleSteps(progress: SetupProgress, me: Permissions) {
  return progress.steps.filter((step) => mayTake(me, step.key));
}

export const stepIsDone = (step: { key: SetupStepKey; state: string }) => doneStates[step.key] === step.state;

// guideHasWork is true while the person has a step left to take.
export const guideHasWork = (progress: SetupProgress, me: Permissions) => visibleSteps(progress, me).some((step) => !stepIsDone(step));
