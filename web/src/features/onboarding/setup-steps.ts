import type { SetupProgress, SetupStepKey, TokenPermission } from '../../api/schemas';
import { can } from '../../lib/can';
import type { Permissions } from '../../lib/can';

// The state each step ends in. Kept apart from the checklist so the inbox can decide, without loading
// it, whether the guide has anything left to show.
export const doneStates: Record<SetupStepKey, string> = { email: 'working', import: 'done', ai_drafts: 'on', reply_targets: 'on', invite: 'joined', first_reply: 'sent' };

// The permission a step's own screen needs, as in Settings: managing channels sets up email and the import,
// inviting is Settings → Team's own permission. Answering the first email needs none beyond reading the inbox.
const stepPermission: Partial<Record<SetupStepKey, TokenPermission>> = { email: 'channels:update', import: 'channels:update', ai_drafts: 'channels:update', reply_targets: 'channels:update', invite: 'team:invite' };

// visibleSteps are the steps this person can take, by the permissions of their roles.
export function visibleSteps(progress: SetupProgress, me: Permissions) {
  return progress.steps.filter((step) => {
    const needed = stepPermission[step.key];
    return !needed || can(me, needed);
  });
}

export const stepIsDone = (step: { key: SetupStepKey; state: string }) => doneStates[step.key] === step.state;

// guideHasWork is true while the person has a step left to take.
export const guideHasWork = (progress: SetupProgress, me: Permissions) => visibleSteps(progress, me).some((step) => !stepIsDone(step));
