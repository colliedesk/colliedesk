import type { SetupProgress, SetupStepKey, TokenPermission } from '../../api/schemas';
import { can } from '../../lib/can';
import type { Permissions } from '../../lib/can';

// The state each step ends in. Kept apart from the checklist so the inbox can decide, without loading
// it, whether the guide has anything left to show.
export const doneStates: Record<SetupStepKey, string> = { email: 'working', import: 'done', ai_drafts: 'on', reply_targets: 'on', invite: 'joined', first_reply: 'sent' };

// The permissions a step's own screen needs, as in Settings: managing channels sets up email and the import,
// the AI drafts step reads the AI settings (ai_settings:read) and turns drafts on (ai_settings:update), saving reply
// targets is workspace:update; the invite step
// lists the invites and the roles (team:read) and creates one (team:invite). Answering the first email needs none
// beyond reading the inbox.
const stepPermissions: Partial<Record<SetupStepKey, TokenPermission[]>> = {
  email: ['channels:update'], import: ['channels:update'], ai_drafts: ['ai_settings:update', 'ai_settings:read'], reply_targets: ['workspace:update'], invite: ['team:invite', 'team:read'],
};

// mayTake is true when the person holds every permission the step's screen needs.
export const mayTake = (me: Permissions, key: SetupStepKey) => (stepPermissions[key] ?? []).every((needed) => can(me, needed));

// The steps offered on the way: they show their state, but the four core steps alone decide whether the setup
// has work left, as the server's complete does.
const offers: ReadonlySet<SetupStepKey> = new Set(['ai_drafts', 'reply_targets']);

// visibleSteps are the steps this person can take, by the permissions of their roles.
export function visibleSteps(progress: SetupProgress, me: Permissions) {
  return progress.steps.filter((step) => mayTake(me, step.key));
}

export const stepIsDone = (step: { key: SetupStepKey; state: string }) => doneStates[step.key] === step.state;

// nothingToDo is a step this workspace cannot take here: AI drafts on a server without built-in AI and with no
// provider of the workspace's own. It is neither done nor to do, and is left out of the count.
export const nothingToDo = (step: { key: SetupStepKey; state: string }) => step.key === 'ai_drafts' && step.state === 'unavailable';

// guideHasWork is true while the person has a core step left to take.
export const guideHasWork = (progress: SetupProgress, me: Permissions) => visibleSteps(progress, me).some((step) => !offers.has(step.key) && !stepIsDone(step));
