import type { RefObject } from 'react';
import type { SetupStepKey } from '../../api/schemas';
import { CheckIcon } from '../../ui/icons';
import { flowMessages as m } from './setup-flow-messages';
import { ownPartIsDone, stepPath } from './setup-flow-path';
import { setupMessages } from './setup-messages';
import { stepIsDone } from './setup-steps';

type Step = { key: SetupStepKey; state: string };

// The words for where a step stands: done, its own words while others or time finish it ("Importing"), or to do.
function stateWords(step: Step) {
  if (stepIsDone(step)) return m.stateDone;
  if (ownPartIsDone(step)) return (setupMessages.state[step.key] as Record<string, string>)[step.state] ?? m.stateTodo;
  return step.state === 'needs_attention' ? m.stateAttention : m.stateTodo;
}

// SetupSummary ends the flow: every step with where it stands, a way back to each one not done, and the inbox.
export function SetupSummary({ steps, heading }: { steps: Step[]; heading: RefObject<HTMLHeadingElement | null> }) {
  return <>
    <div className="setup-flow-title">
      <h1 ref={heading} tabIndex={-1}>{m.doneTitle}</h1>
      <p className="setup-flow-lead">{steps.every(stepIsDone) ? m.doneAll : m.doneSome}</p>
    </div>
    <ul className="setup-flow-summary" aria-label={m.stepsLabel}>
      {steps.map((step) => {
        const done = stepIsDone(step);
        return <li key={step.key} data-state={done ? 'done' : ownPartIsDone(step) ? 'waiting' : 'todo'}>
          <span className="setup-flow-mark" aria-hidden="true">{done && <CheckIcon />}</span>
          {done ? <span className="setup-flow-summary-title">{setupMessages.title[step.key]}</span> : <a href={stepPath(step.key)}>{setupMessages.title[step.key]}</a>}
          <span className="setup-flow-summary-state">{stateWords(step)}</span>
        </li>;
      })}
    </ul>
    <footer className="setup-flow-actions"><div className="setup-flow-buttons">
      <a className="button primary-action setup-flow-inbox" href="/">{m.openInbox}</a>
    </div></footer>
  </>;
}
