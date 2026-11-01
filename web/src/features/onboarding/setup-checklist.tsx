import { useEffect, useRef, useState } from 'react';
import type { CSSProperties, KeyboardEvent } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import type { OnboardingGoal } from '../../api/schemas';
import type { SetupProgress, SetupStepKey } from '../../api/schemas';
import { preloadChannelsPage } from '../settings/channels/channels-preload';
import { track } from '../../lib/product-analytics';
import { request } from '../../api/client';
import { setupProgressSchema } from '../../api/schemas';
import { messages } from '../../messages';
import { CheckIcon, ChevronDownIcon } from '../../ui/icons';
import { setupMessages } from './setup-messages';
import { stepIsDone, visibleSteps } from './setup-steps';
import './setup-checklist.css';

// Where each step is finished.
const targets: Record<SetupStepKey, string> = { email: '/settings/channels/type/email', import: '/settings/import-freshdesk', invite: '/settings', first_reply: '/', preview: '/settings#help-center-heading', custom_domain: '/settings#help-center-domain-heading', plan: '/settings/billing', cname: '/settings#help-center-domain-heading', search_console: '/settings#help-center-search-console-heading' };

// Whether the agent left the guide open or folded, kept by this browser; without a stored choice, none.
const storageKey = 'colliedesk-setup-dock-v1';
function storedChoice(): boolean | undefined {
  try {
    const value = localStorage.getItem(storageKey);
    return value === 'open' ? true : value === 'folded' ? false : undefined;
  } catch { return undefined; }
}
function storeChoice(open: boolean) {
  try { localStorage.setItem(storageKey, open ? 'open' : 'folded'); } catch { /* the choice holds for this visit */ }
}

interface SetupChecklistProps {
  progress: SetupProgress;
  admin: boolean;
  goal?: OnboardingGoal | null;
  // Open until the agent folds it, for an inbox with no conversation for the guide to cover.
  defaultOpen?: boolean;
  onHide: () => void;
}

// SetupChecklist lists what is left before the team can receive and answer customer email. The
// server orders the steps by the sign-up goal; this only drops the ones the agent cannot take.
// It waits in a corner of the screen, folded to one button with the count, so it never covers a
// conversation the agent did not ask it to.
export function SetupChecklist({ progress, admin, goal, defaultOpen = false, onHide }: SetupChecklistProps) {
  const client = useQueryClient();
  const update = useMutation({
    mutationFn: (body: { step: 'preview' | 'custom_domain' | 'search_console'; state: 'done' | 'skipped' | 'none' }) =>
      request('/api/v1/workspace/setup/move', setupProgressSchema, { method: 'PUT', body: JSON.stringify(body) }),
    onSuccess: (saved) => client.setQueryData(['setup'], saved),
  });
  const steps = visibleSteps(progress, admin);
  const done = steps.filter(stepIsDone).length;
  const domainSkipped = steps.some((step) => step.state === 'skipped');
  const firstTodo = steps.find((step) => !stepIsDone(step))?.key;
  const [choice, setChoice] = useState(storedChoice);
  const open = choice ?? defaultOpen;
  const launcher = useRef<HTMLButtonElement>(null);
  const minimize = useRef<HTMLButtonElement>(null);
  // Focus follows the agent's own open or fold, never a guide that arrives open.
  const toggled = useRef(false);
  const toggle = (next: boolean) => { toggled.current = true; setChoice(next); storeChoice(next); };
  useEffect(() => {
    if (!toggled.current) return;
    toggled.current = false;
    (open ? minimize : launcher).current?.focus();
  }, [open]);
  const fold = (event: KeyboardEvent) => {
    if (event.key !== 'Escape') return;
    event.stopPropagation();
    toggle(false);
  };
  const share = steps.length === 0 ? 0 : done / steps.length;

  // A step that turns done while this is on screen is reported once; what was done on arrival is not.
  const seenDone = useRef<Set<SetupStepKey>>(undefined);
  useEffect(() => {
    const now = new Set(steps.filter((step) => step.state !== 'skipped' && stepIsDone(step)).map((step) => step.key));
    if (seenDone.current) {
      for (const key of now) if (!seenDone.current.has(key)) track('onboarding_step_completed', { step: key });
    }
    seenDone.current = now;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [progress, admin]);

  if (!open) {
    return (
      <div className="setup-dock">
        <button type="button" ref={launcher} className="setup-launcher" aria-expanded={false} onClick={() => toggle(true)}>
          <span className="setup-ring" aria-hidden="true" style={{ '--share': share } as CSSProperties} />
          <span className="setup-launcher-title">{setupMessages.heading}</span>
          <span className="setup-count">{setupMessages.progress(done, steps.length)}</span>
        </button>
      </div>
    );
  }
  return (
    <div className="setup-dock">
      <section className="setup-guide" aria-label={setupMessages.heading} data-move={goal === 'leave_helpdesk' || undefined} onKeyDown={fold}>
        <header>
          <h2>{setupMessages.heading}</h2>
          <span className="setup-count">{setupMessages.progress(done, steps.length)}</span>
          <button type="button" className="link-action" title={setupMessages.hideHint} onClick={onHide}>{setupMessages.hide}</button>
          <button type="button" ref={minimize} className="icon-button setup-minimize" aria-label={setupMessages.minimize} aria-expanded
            data-tip={setupMessages.minimize} onClick={() => toggle(false)}><ChevronDownIcon /></button>
        </header>
        <div className="setup-bar" role="progressbar" aria-label={setupMessages.progressLabel} aria-valuemin={0} aria-valuemax={steps.length} aria-valuenow={done}>
          <span style={{ width: `${share * 100}%` }} />
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
            const target = step.url || targets[step.key];
            return (
              <li key={step.key} className="setup-step" data-state={finished ? 'done' : 'todo'} data-step={step.key}>
                <span className="setup-mark" aria-hidden="true">{finished && <CheckIcon />}</span>
                <a href={target} className={primary ? 'primary-action' : undefined} onClick={() => track('onboarding_step_started', { step: step.key })}
                  onPointerEnter={() => preloadChannelsPage(client, target)} onFocus={() => preloadChannelsPage(client, target)}>
                  <span className="setup-title">{setupMessages.title[step.key]}</span>
                  <span className="setup-state" data-attention={step.state === 'needs_attention' || undefined}>
                    {progress.recommended === step.key && !finished && <><b>{setupMessages.recommended}</b>{' · '}</>}
                    {step.detail ? setupMessages.prerequisites[step.detail] ?? (setupMessages.state[step.key] as Record<string, string>)[step.state] : (setupMessages.state[step.key] as Record<string, string>)[step.state] ?? ''}
                  </span>
                </a>
                {!finished && <span className="setup-time">{setupMessages.time[step.key]}</span>}
                {!finished && ((step.key === 'preview' && !step.detail) || (step.key === 'search_console' && primary)) &&
                  <button type="button" className="link-action" disabled={update.isPending} onClick={() => update.mutate({ step: step.key as 'preview' | 'search_console', state: 'done' })}>
                    {step.key === 'preview' ? setupMessages.previewChecked : setupMessages.searchSubmitted}
                  </button>}
                {step.key === 'custom_domain' && <button type="button" className="link-action" disabled={update.isPending}
                  onClick={() => update.mutate({ step: 'custom_domain', state: domainSkipped ? 'none' : 'skipped' })}>
                  {domainSkipped ? setupMessages.restoreDomain : setupMessages.skipDomain}
                </button>}
              </li>
            );
          })}
        </ol>
        {update.isError && <p role="alert">{setupMessages.updateFailed}</p>}
      </section>
    </div>
  );
}
