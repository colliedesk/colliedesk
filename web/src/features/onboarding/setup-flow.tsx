import { useEffect, useId, useRef } from 'react';
import { useQuery } from '@tanstack/react-query';
import { api } from '../../api/client';
import type { SetupStepKey } from '../../api/schemas';
import { messages } from '../../messages';
import { useDismissibleTooltips } from '../shell/app-shell';
import { FlowProgress, SetupFlowFrame } from './setup-flow-frame';
import { flowMessages as m } from './setup-flow-messages';
import { firstOpenStep, nextPath, ownPartIsDone, previousPath, stepPath } from './setup-flow-path';
import { StepBody } from './setup-flow-steps';
import { SetupSummary } from './setup-flow-summary';
import { setupMessages } from './setup-messages';
import { visibleSteps } from './setup-steps';
import './setup-flow.css';

// While the flow is open the server's progress is read again this often: what others finish (the first email,
// an import) moves the steps on, as a save on this screen does at once.
const pollMs = 5000;

export interface SetupFlowRouteProps {
  // step is the address's step: a step key, done for the summary, or nothing for /setup.
  step?: string;
  navigate: (href: string, replace?: boolean) => void;
}

// SetupFlowRoute is the setup, one step per screen, each done in place: Continue once the person's own part is
// done, "I'll do this later" to move on anyway, Back, and a summary at the end. The steps and their order are the
// server's (the sign-up goal's first); an agent who is not an admin gets only the steps they can take.
export function SetupFlowRoute({ step, navigate }: SetupFlowRouteProps) {
  useDismissibleTooltips();
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const setup = useQuery({ queryKey: ['setup'], queryFn: api.setupProgress, refetchInterval: pollMs });
  const admin = me.data?.agent.role === 'admin';
  const steps = me.data && setup.data ? visibleSteps(setup.data, admin) : undefined;
  const index = steps && step ? steps.findIndex((item) => item.key === step) : -1;
  const known = step === 'done' || index >= 0;
  // /setup opens the first step still to do; a step this person cannot take, or none, starts over.
  useEffect(() => {
    if (!steps) return;
    if (step === undefined) navigate(stepPath(firstOpenStep(steps)), true);
    else if (!known) navigate('/setup', true);
  }, [steps, step, known, navigate]);
  // Each screen starts at its title, so a screen reader says where the person is.
  const heading = useRef<HTMLHeadingElement>(null);
  const loaded = Boolean(steps);
  useEffect(() => { heading.current?.focus(); }, [step, loaded]);
  const missingID = useId();

  if (me.isError || setup.isError) return <SetupFlowFrame me={me.data}>
    <p role="alert" className="error-copy">{m.loadFailed}{' '}
      <button type="button" className="link-action" onClick={() => { void me.refetch(); void setup.refetch(); }}>{messages.tryAgain}</button></p>
  </SetupFlowFrame>;
  if (!steps || !known || !me.data) return <SetupFlowFrame me={me.data} busy />;
  if (step === 'done') return <SetupFlowFrame me={me.data}><SetupSummary steps={steps} heading={heading} /></SetupFlowFrame>;

  const current = steps[index];
  const stepKey: SetupStepKey = current.key;
  const ready = ownPartIsDone(current);
  const missing = ready ? undefined : (m.missing[stepKey] as Record<string, string>)[current.state];
  const next = nextPath(steps, index);
  const back = previousPath(steps, index);
  const following = steps[index + 1];
  return <SetupFlowFrame me={me.data}>
    <FlowProgress steps={steps} current={index} />
    <div className="setup-flow-title">
      {setup.data?.recommended === stepKey && <span className="setup-flow-recommended">{setupMessages.recommended}</span>}
      <h1 ref={heading} tabIndex={-1}>{setupMessages.title[stepKey]}</h1>
      <p className="setup-flow-lead">{m.lead[stepKey]}</p>
    </div>
    <div className="setup-flow-body"><StepBody step={stepKey} admin={admin} timezone={me.data.timezone} navigate={navigate} /></div>
    {/* The moves between steps. The main button names the step it opens: the email setup inside has a Continue
        of its own, for its own parts. */}
    <nav className="setup-flow-actions" aria-label={m.flowNav}>
      {missing && <p id={missingID} className="muted-copy setup-flow-missing">{missing}</p>}
      <div className="setup-flow-buttons">
        {back && <a className="button setup-flow-back" href={back}>{m.back}</a>}
        <a className="link-action setup-flow-later" href={next}>{m.later}</a>
        <button type="button" className="primary-action" disabled={!ready} aria-describedby={missing ? missingID : undefined} onClick={() => navigate(next)}>{following ? m.next(setupMessages.title[following.key]) : m.finish}</button>
      </div>
    </nav>
  </SetupFlowFrame>;
}
