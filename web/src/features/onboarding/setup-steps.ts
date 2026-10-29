import type { SetupProgress, SetupStepKey } from '../../api/schemas';

// The state each step ends in. Kept apart from the checklist so the inbox can decide, without loading
// it, whether the guide has anything left to show.
export const doneStates: Record<SetupStepKey, string> = { email: 'working', import: 'done', invite: 'joined', first_reply: 'sent' };

const adminSteps: ReadonlySet<SetupStepKey> = new Set(['email', 'import', 'invite']);

// visibleSteps are the steps this agent can take: admins have all, an agent only answers email.
export function visibleSteps(progress: SetupProgress, admin: boolean) {
  return progress.steps.filter((step) => admin || !adminSteps.has(step.key));
}

export const stepIsDone = (step: { key: SetupStepKey; state: string }) => doneStates[step.key] === step.state;

// guideHasWork is true while the agent has a step left to take.
export const guideHasWork = (progress: SetupProgress, admin: boolean) => visibleSteps(progress, admin).some((step) => !stepIsDone(step));
