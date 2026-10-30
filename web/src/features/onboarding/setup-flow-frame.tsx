import type { ReactNode } from 'react';
import type { Me, SetupStepKey } from '../../api/schemas';
import { messages } from '../../messages';
import { XIcon } from '../../ui/icons';
import { LogoMark } from '../../ui/logo';
import { flowMessages as m } from './setup-flow-messages';
import { ownPartIsDone } from './setup-flow-path';

// SetupFlowFrame is the whole screen of the setup flow: the mark, the workspace and address this browser
// acts in (always on screen, as the identity bar is on every other page), the way out, and one column.
export function SetupFlowFrame({ me, busy = false, children }: { me?: Me; busy?: boolean; children?: ReactNode }) {
  return <div className="setup-flow">
    <header className="setup-flow-head">
      <span className="setup-flow-logo"><LogoMark size={24} /></span>
      {me && <section className="setup-flow-identity" aria-label={messages.signedInAs}><strong>{me.workspace.name}</strong><span>{me.agent.email}</span></section>}
      <a href="/" className="setup-flow-exit" aria-label={m.exit} data-tip={m.exit}><XIcon /></a>
    </header>
    <main className="setup-flow-main" aria-busy={busy || undefined}><div className="setup-flow-column">{children}</div></main>
  </div>;
}

// FlowProgress is one segment per step, filled once the person's own part of it is done, and where they are.
export function FlowProgress({ steps, current }: { steps: { key: SetupStepKey; state: string }[]; current: number }) {
  const where = m.stepOf(current + 1, steps.length);
  return <div className="setup-flow-progress" role="progressbar" aria-label={m.progressLabel} aria-valuemin={1} aria-valuemax={steps.length}
    aria-valuenow={current + 1} aria-valuetext={where}>
    <span className="setup-flow-segments">
      {steps.map((step, index) => <span key={step.key} data-state={ownPartIsDone(step) ? 'done' : 'todo'} data-current={index === current || undefined} />)}
    </span>
    <span>{where}</span>
  </div>;
}
