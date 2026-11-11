import type { SetupStepKey } from '../../api/schemas';

type Step = { key: SetupStepKey; state: string };

// The states in which the person's own part of a step is done. What follows is up to others or to time: the
// first customer email, an import running, a teammate accepting the invite.
const ownPartDone: Record<SetupStepKey, ReadonlySet<string>> = {
  email: new Set(['waiting', 'working']),
  import: new Set(['running', 'done']),
  invite: new Set(['invited', 'joined']),
  first_reply: new Set(['sent']),
};

export const ownPartIsDone = (step: Step) => ownPartDone[step.key].has(step.state);

export const stepPath = (key: SetupStepKey | 'done') => `/setup/${key}`;

// firstOpenStep is where /setup starts: the first step whose own part is not done, else the summary.
export const firstOpenStep = (steps: Step[]): SetupStepKey | 'done' => steps.find((step) => !ownPartIsDone(step))?.key ?? 'done';

// The address after a step (the summary after the last) and before it (none before the first).
export const nextPath = (steps: Step[], index: number) => stepPath(steps[index + 1]?.key ?? 'done');
export const previousPath = (steps: Step[], index: number) => (index > 0 ? stepPath(steps[index - 1].key) : undefined);
