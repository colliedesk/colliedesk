import { useEffect, useRef } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import type { OnboardingGoal } from '../../api/schemas';
import type { SetupProgress, SetupStepKey } from '../../api/schemas';
import { preloadChannelsPage } from '../settings/channels/channels-preload';
import { track } from '../../lib/product-analytics';
import { messages } from '../../messages';
import { setupMessages } from './setup-messages';
import { stepIsDone, visibleSteps } from './setup-steps';
import './setup-checklist.css';

// Where each step is finished.
const targets: Record<SetupStepKey, string> = { email: '/settings/channels/type/email', import: '/settings/import-freshdesk', invite: '/settings', first_reply: '/' };

interface SetupChecklistProps {
  progress: SetupProgress;
  admin: boolean;
  goal?: OnboardingGoal | null;
  onHide: () => void;
}

// SetupChecklist lists what is left before the team can receive and answer customer email. The
// server orders the steps by the sign-up goal; this only drops the ones the agent cannot take.
export function SetupChecklist({ progress, admin, goal, onHide }: SetupChecklistProps) {
  const client = useQueryClient();
  const steps = visibleSteps(progress, admin);
  const done = steps.filter(stepIsDone).length;
  const firstTodo = steps.find((step) => !stepIsDone(step))?.key;

  // A step that turns done while this is on screen is reported once; what was done on arrival is not.
  const seenDone = useRef<Set<SetupStepKey>>(undefined);
  useEffect(() => {
    const now = new Set(steps.filter(stepIsDone).map((step) => step.key));
    if (seenDone.current) {
      for (const key of now) if (!seenDone.current.has(key)) track('onboarding_step_completed', { step: key });
    }
    seenDone.current = now;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [progress, admin]);

  return (
    <section className="setup-guide" aria-label={setupMessages.heading}>
      <header>
        <h2>{setupMessages.heading}</h2>
        <span className="setup-count">{setupMessages.progress(done, steps.length)}</span>
        <button type="button" className="link-action" title={setupMessages.hideHint} onClick={onHide}>{setupMessages.hide}</button>
      </header>
      <div className="setup-bar" role="progressbar" aria-label={setupMessages.progressLabel} aria-valuemin={0} aria-valuemax={steps.length} aria-valuenow={done}>
        <span style={{ width: `${steps.length === 0 ? 0 : (done / steps.length) * 100}%` }} />
      </div>
      <ol>
        {goal === 'ai_agents' && <li className="setup-step" data-state="todo">
          <span className="setup-mark" aria-hidden="true" />
          <a href={messages.mcpURL} target="_blank" rel="noreferrer">
            <span className="setup-title">{setupMessages.mcp.title}</span>
            <span className="setup-state">{setupMessages.mcp.state}</span>
          </a>
          <span className="setup-time">{setupMessages.mcp.time}</span>
        </li>}
        {steps.map((step) => {
          const finished = stepIsDone(step);
          const primary = step.key === firstTodo;
          return (
            <li key={step.key} className="setup-step" data-state={finished ? 'done' : 'todo'} data-step={step.key}>
              <span className="setup-mark" aria-hidden="true" />
              <a href={targets[step.key]} className={primary ? 'primary-action' : undefined} onClick={() => track('onboarding_step_started', { step: step.key })}
                onPointerEnter={() => preloadChannelsPage(client, targets[step.key])} onFocus={() => preloadChannelsPage(client, targets[step.key])}>
                <span className="setup-title">{setupMessages.title[step.key]}</span>
                <span className="setup-state" data-attention={step.state === 'needs_attention' || undefined}>
                  {progress.recommended === step.key && !finished && <><b>{setupMessages.recommended}</b>{' · '}</>}
                  {(setupMessages.state[step.key] as Record<string, string>)[step.state] ?? ''}
                </span>
              </a>
              {!finished && <span className="setup-time">{setupMessages.time[step.key]}</span>}
            </li>
          );
        })}
      </ol>
    </section>
  );
}
