import type { SetupProgress, SetupStepKey } from '../../api/schemas';
import type { Permissions } from '../../lib/can';
import { nothingToDo, visibleSteps } from './setup-steps';

type Step = { key: SetupStepKey; state: string };

// The states in which the person's own part of a step is done. What follows is up to others or to time: the
// first customer email, an import running, a teammate accepting the invite.
const ownPartDone: Record<SetupStepKey, ReadonlySet<string>> = {
  email: new Set(['waiting', 'working']),
  import: new Set(['running', 'done']),
  ai_drafts: new Set(['on']),
  reply_targets: new Set(['on']),
  invite: new Set(['invited', 'joined']),
  first_reply: new Set(['sent']),
};

export const ownPartIsDone = (step: Step) => ownPartDone[step.key].has(step.state);

export const stepPath = (key: SetupStepKey | 'done') => `/setup/${key}`;

// firstOpenStep is where /setup starts: the first step whose own part is not done and that has something to do
// here (AI drafts without any AI has none), else the summary.
export const firstOpenStep = (steps: Step[]): SetupStepKey | 'done' =>
  steps.find((step) => !ownPartIsDone(step) && !nothingToDo(step))?.key ?? 'done';

// The address after a step (the summary after the last) and before it (none before the first).
export const nextPath = (steps: Step[], index: number) => stepPath(steps[index + 1]?.key ?? 'done');
export const previousPath = (steps: Step[], index: number) => (index > 0 ? stepPath(steps[index - 1].key) : undefined);

// The steps that bring customers into an empty inbox: the address they write to, or the history to move over. The
// first answer to a customer waits for their first email, so an empty inbox never offers it.
const fillsInbox: readonly SetupStepKey[] = ['email', 'import'];

// emptyInboxStep is the step the empty inbox offers as its button: the first of those the person may take whose own
// part is not done. A connected address that waits for its first email is done; the import is the next way in.
export function emptyInboxStep(progress: SetupProgress | undefined, me: Permissions): 'email' | 'import' | undefined {
  const step = progress && visibleSteps(progress, me).find((candidate) => fillsInbox.includes(candidate.key) && !ownPartIsDone(candidate));
  return step?.key === 'email' || step?.key === 'import' ? step.key : undefined;
}
