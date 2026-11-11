import { useEffect, useId, useRef } from 'react';
import { useQuery } from '@tanstack/react-query';
import { api } from '../../api/client';
import { can } from '../../lib/can';
import type { SetupStepKey } from '../../api/schemas';
import { messages } from '../../messages';
import { useDismissibleTooltips } from '../shell/app-shell';
import { FlowProgress, SetupFlowFrame } from './setup-flow-frame';
import { flowMessages as m } from './setup-flow-messages';
import { firstOpenStep, nextPath, ownPartIsDone, previousPath, stepPath } from './setup-flow-path';
import { StepBody } from './setup-flow-steps';
import { FlowActions } from './setup-flow-actions';
import type { FlowMoves } from './setup-flow-actions';
import { AIDraftsStep } from './setup-ai-drafts-step';
import { ReplyTargetsStep } from './setup-reply-targets-step';
import { SetupSummary } from './setup-flow-summary';
import { setupMessages } from './setup-messages';
import { mayTake, visibleSteps } from './setup-steps';
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
// server's (the sign-up goal's first); a person gets only the steps their roles may take.
export function SetupFlowRoute({ step, navigate }: SetupFlowRouteProps) {
  useDismissibleTooltips();
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const setup = useQuery({ queryKey: ['setup'], queryFn: api.setupProgress, refetchInterval: pollMs });
  // The same steps as the inbox's setup guide: each one the person's roles may take. admin is managing channels,
  // which the email setup and the first reply's link to it need.
  const admin = can(me.data, 'channels:update');
  const steps = me.data && setup.data ? visibleSteps(setup.data, me.data) : undefined;
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

  // A check that fails after the steps loaded keeps them on screen, with what the person typed or was shown
  // once (an invite link); only a flow that never loaded says so.
  if ((me.isError && !me.data) || (setup.isError && !setup.data)) return <SetupFlowFrame me={me.data}>
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
  const moves: FlowMoves = { back, later: next, next: () => navigate(next), nextLabel: following ? m.next(setupMessages.title[following.key]) : m.finish };
  const importState = steps.find((item) => item.key === 'import')?.state;
  return <SetupFlowFrame me={me.data}>
    <FlowProgress steps={steps} current={index} />
    <div className="setup-flow-title">
      {setup.data?.recommended === stepKey && <span className="setup-flow-recommended">{setupMessages.recommended}</span>}
      <h1 ref={heading} tabIndex={-1}>{m.heading[stepKey] ?? setupMessages.title[stepKey]}</h1>
      <p className="setup-flow-lead">{m.lead[stepKey]}</p>
    </div>
    {/* AI drafts and reply targets have a main button of their own (Turn on AI drafts, Use Same day). */}
    {stepKey === 'ai_drafts' ? <AIDraftsStep state={current.state} workspace={me.data.workspace.name} moves={moves} />
      : stepKey === 'reply_targets' && setup.data ? <ReplyTargetsStep state={current.state} setup={setup.data} zone={me.data.workspace.timezone} importState={importState} moves={moves} />
      : <>
        <div className="setup-flow-body"><StepBody step={stepKey} admin={admin} canInvite={mayTake(me.data, 'invite')} canRevoke={can(me.data, 'team:revoke')} timezone={me.data.timezone} navigate={navigate} /></div>
        {/* The moves between steps. The main button names the step it opens: the email setup inside has a Continue
            of its own, for its own parts. */}
        <FlowActions back={back} later={next} missing={missing ? { id: missingID, text: missing } : undefined}
          primary={<button type="button" className="primary-action" disabled={!ready} aria-describedby={missing ? missingID : undefined} onClick={moves.next}>{moves.nextLabel}</button>} />
      </>}
  </SetupFlowFrame>;
}
