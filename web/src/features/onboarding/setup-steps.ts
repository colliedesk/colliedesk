import type { SetupProgress, SetupStepKey, TokenPermission } from '../../api/schemas';
import { can } from '../../lib/can';
import type { Permissions } from '../../lib/can';

// The state each step ends in. Kept apart from the checklist so Quick start can count, without loading it, what is
// left to do.
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

// Quick start's Set up group: what the team needs to receive and answer customer email. Every other step (the
// invites and the two offers) is in Try more, which the person may skip.
export const setUpKeys: ReadonlySet<SetupStepKey> = new Set(['email', 'import', 'first_reply']);

// visibleSteps are the steps this person can take, by the permissions of their roles.
export function visibleSteps(progress: SetupProgress, me: Permissions) {
  return progress.steps.filter((step) => mayTake(me, step.key));
}

export const stepIsDone = (step: { key: SetupStepKey; state: string }) => doneStates[step.key] === step.state;

// nothingToDo is a step this workspace cannot take here: AI drafts on a server without built-in AI and with no
// provider of the workspace's own. It is neither done nor to do, and is left out of the count.
export const nothingToDo = (step: { key: SetupStepKey; state: string }) => step.key === 'ai_drafts' && step.state === 'unavailable';

// highlightedStep is the step to do next, the one Quick start highlights and the empty inbox offers: the first of Set up,
// then of Try more (left out once skipped), that is neither done nor without anything to do here.
export function highlightedStep(progress: SetupProgress | undefined, me: Permissions, skipped: boolean): SetupStepKey | undefined {
  if (!progress) return undefined;
  const steps = visibleSteps(progress, me);
  const setUp = steps.filter((step) => setUpKeys.has(step.key));
  const shown = skipped ? setUp : [...setUp, ...steps.filter((step) => !setUpKeys.has(step.key))];
  return shown.find((step) => !stepIsDone(step) && !nothingToDo(step))?.key;
}

// quickStartCount counts what Quick start shows: every step this person takes, but one with nothing to do here and,
// once skipped, the Try more steps. setUpLeft is true while a Set up step is still to do (the icon's amber dot).
export function quickStartCount(progress: SetupProgress, me: Permissions, skipped: boolean) {
  const counted = visibleSteps(progress, me).filter((step) => !nothingToDo(step) && (!skipped || setUpKeys.has(step.key)));
  const done = counted.filter(stepIsDone).length;
  return { done, total: counted.length, left: counted.length - done, setUpLeft: counted.some((step) => setUpKeys.has(step.key) && !stepIsDone(step)) };
}

// The browser keeps a person's Skip these, per person, so a teammate on the same computer keeps their own.
const skippedKey = (agentId: string) => `colliedesk.quick-start.skipped.${agentId}`;
export function readSkipped(agentId: string): boolean {
  try { return localStorage.getItem(skippedKey(agentId)) === 'yes'; } catch { return false; }
}
export function storeSkipped(agentId: string, skipped: boolean) {
  try {
    if (skipped) localStorage.setItem(skippedKey(agentId), 'yes');
    else localStorage.removeItem(skippedKey(agentId));
  } catch { /* the choice holds for this visit */ }
}
