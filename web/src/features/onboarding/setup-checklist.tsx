import { useEffect, useId, useRef } from 'react';
import type { ReactNode } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import type { OnboardingGoal, SetupProgress, SetupStepKey } from '../../api/schemas';
import { preloadSetupStep } from './setup-flow-part';
import { track } from '../../lib/product-analytics';
import { messages } from '../../messages';
import { setupMessages } from './setup-messages';
import { nothingToDo, quickStartCount, setUpKeys, stepIsDone, visibleSteps } from './setup-steps';
import type { Permissions } from '../../lib/can';
import './setup-checklist.css';
import { frameMessages } from '../shell/frame-messages';

// Each step is done on its screen of the setup flow.
const target = (step: SetupStepKey) => `/setup/${step}`;

interface SetupChecklistProps {
  progress: SetupProgress;
  // me holds the permissions that decide which steps this person takes.
  me: Permissions;
  goal?: OnboardingGoal | null;
  workspaceName: string;
  // skipped leaves Try more out; onSkip asks for it (true) or brings it back (false).
  skipped?: boolean;
  onSkip: (skipped: boolean) => void;
}

// The success disc with a tick, for a finished step.
const DoneMark = () => <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true">
  <circle cx="12" cy="12" r="10" fill="currentColor" />
  <path d="m7.5 12.5 3 3 6-6.5" fill="none" stroke="var(--on-accent)" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
</svg>;

function Group({ title, action, children }: { title: string; action?: ReactNode; children: ReactNode }) {
  const id = useId();
  return <section className="quick-start-group" aria-labelledby={id}>
    <div className="quick-start-group-head"><h3 id={id}>{title}</h3>{action}</div>
    <ol>{children}</ol>
  </section>;
}

// SetupChecklist is what Quick start opens: how far the workspace is ("2 of 6" and a bar of one segment per step),
// then Set up (what it takes to receive and answer customer email) and Try more (the invites, the two offers and an
// AI agent), which the person may skip. The server orders the steps by the sign-up goal; this only drops the ones
// the person's roles cannot take. Done steps are ticked and struck through; the next one to do is highlighted.
export function SetupChecklist({ progress, me, goal, workspaceName, skipped = false, onSkip }: SetupChecklistProps) {
  const client = useQueryClient();
  const steps = visibleSteps(progress, me);
  const setUp = steps.filter((step) => setUpKeys.has(step.key));
  const tryMore = steps.filter((step) => !setUpKeys.has(step.key));
  const { done, total } = quickStartCount(progress, me, skipped);
  const shown = skipped ? setUp : [...setUp, ...tryMore];
  const next = shown.find((step) => !stepIsDone(step) && !nothingToDo(step))?.key;

  // A step that turns done while this is on screen is reported once; what was done on arrival is not.
  const seenDone = useRef<Set<SetupStepKey>>(undefined);
  useEffect(() => {
    const now = new Set(steps.filter(stepIsDone).map((step) => step.key));
    if (seenDone.current) {
      for (const key of now) if (!seenDone.current.has(key)) track('onboarding_step_completed', { step: key });
    }
    seenDone.current = now;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [progress, me]);

  const row = (step: (typeof steps)[number]) => {
    const finished = stepIsDone(step);
    const idle = nothingToDo(step);
    return (
      <li key={step.key} className="checklist-step" data-state={finished ? 'done' : idle ? 'none' : 'todo'} data-step={step.key}>
        <span className="setup-mark" aria-hidden="true">{finished && <DoneMark />}</span>
        <a href={target(step.key)} className={step.key === next ? 'primary-action' : undefined} onClick={() => track('onboarding_step_started', { step: step.key })}
          onPointerEnter={() => preloadSetupStep(client, step.key)} onFocus={() => preloadSetupStep(client, step.key)}>
          <span className="setup-title">{setupMessages.title[step.key]}</span>
          <span className="setup-state" data-attention={step.state === 'needs_attention' || undefined}>
            {progress.recommended === step.key && !finished && <><b>{setupMessages.recommended}</b>{' · '}</>}
            {(setupMessages.state[step.key] as Record<string, string>)[step.state] ?? ''}
          </span>
        </a>
        {!finished && !idle && <span className="setup-time">{setupMessages.time[step.key]}</span>}
      </li>
    );
  };
  // The AI agent guide is a page of the docs: never counted, first in Try more for a team that signed up for it.
  const mcp = <li key="mcp" className="checklist-step" data-state="todo" data-step="mcp">
    <span className="setup-mark" aria-hidden="true" />
    <a href={messages.mcpURL} target="_blank" rel="noreferrer">
      <span className="setup-title">{setupMessages.mcp.title}</span>
      <span className="setup-state">{setupMessages.mcp.state}</span>
    </a>
    <span className="setup-time">{setupMessages.mcp.time}</span>
  </li>;
  const offered = tryMore.some((step) => !nothingToDo(step));
  return <>
    <header className="quick-start-head">
      <div className="quick-start-title"><h2>{frameMessages.getReady(workspaceName)}</h2><span className="setup-count">{setupMessages.progress(done, total)}</span></div>
      <div className="setup-segments" role="progressbar" aria-label={setupMessages.progressLabel} aria-valuemin={0} aria-valuemax={total} aria-valuenow={done}>
        {Array.from({ length: total }, (_, index) => <span key={index} data-done={index < done || undefined} />)}
      </div>
    </header>
    <div className="quick-start-body">
      {setUp.length > 0 && <Group title={setupMessages.setUp}>{setUp.map(row)}</Group>}
      <Group title={setupMessages.tryMore} action={offered && <button type="button" className="link-action quick-start-skip" onClick={() => onSkip(!skipped)}>
        {skipped ? setupMessages.unskip : setupMessages.skip}</button>}>
        {!skipped && (goal === 'ai_agents' ? [mcp, ...tryMore.map(row)] : [...tryMore.map(row), mcp])}
      </Group>
    </div>
  </>;
}
