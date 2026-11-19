import { useEffect, useId, useRef, useState } from 'react';
import { autoUpdate, flip, offset, shift, useFloating } from '@floating-ui/react';
import type { Me } from '../../api/schemas';
import { track } from '../../lib/product-analytics';
import { useNow } from '../../lib/use-now';
import { usePhone } from '../../lib/use-phone';
import { can } from '../../lib/can';
import { XIcon } from '../../ui/icons';
import { billingMessages } from '../settings/billing-messages';

const storageKey = 'colliedesk-survey-switch-blocker-v1';
const dayMs = 24 * 60 * 60 * 1000;
const maxAnswer = 1000;

// answeredBefore treats an unreadable store as answered: a browser that cannot remember would ask on every page.
function answeredBefore(): boolean {
  try { return localStorage.getItem(storageKey) !== null; } catch { return true; }
}
function remember() {
  try { localStorage.setItem(storageKey, '1'); } catch { /* the survey is closed for this visit either way */ }
}

interface TrialSurveyProps {
  me: Me;
  analytics?: { enabled: boolean; realm: string | null };
  now?: Date;
}

// TrialSurvey asks a cloud trial admin one question in the last week of the trial, as one short line in the
// trial strip. Tell us opens the question under it (a sheet from the bottom on a phone); Escape or a press
// outside closes it and keeps what was typed until the page reloads. The typed answer leaves the browser only
// when the admin presses Send; it is the one analytics event that carries text. The team running the cloud is
// never asked.
export function TrialSurvey({ me, analytics, now }: TrialSurveyProps) {
  const clock = useNow('UTC');
  const phone = usePhone();
  const [skipped] = useState(answeredBefore);
  const [done, setDone] = useState<'thanks' | 'closed' | null>(null);
  const [open, setOpen] = useState(false);
  const [answer, setAnswer] = useState('');
  const [failed, setFailed] = useState(false);
  const reported = useRef(false);
  const trigger = useRef<HTMLButtonElement | null>(null);
  const layer = useRef<HTMLDivElement | null>(null);
  const id = useId();
  const labelId = `${id}-question`;
  const layerId = `${id}-layer`;
  const { refs, floatingStyles } = useFloating({
    open: open && !phone, placement: 'bottom-end', strategy: 'fixed', whileElementsMounted: autoUpdate,
    middleware: [offset(8), flip({ padding: 8 }), shift({ padding: 8 })],
  });

  useEffect(() => {
    if (!open) return;
    // A press anywhere but the question or its button closes it; the draft stays.
    const press = (event: Event) => {
      const target = event.target as Node;
      if (layer.current?.contains(target) || trigger.current?.contains(target)) return;
      setOpen(false);
    };
    // On window in the capture phase, so Escape closes the question before the inbox's own shortcuts see it.
    const key = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return;
      event.stopPropagation();
      event.preventDefault();
      setOpen(false);
      trigger.current?.focus();
    };
    document.addEventListener('pointerdown', press);
    window.addEventListener('keydown', key, true);
    return () => { document.removeEventListener('pointerdown', press); window.removeEventListener('keydown', key, true); };
  }, [open]);

  const plan = me.plan;
  if (done === 'thanks') return <span className="trial-survey" role="status">{billingMessages.surveyThanks}</span>;
  if (done === 'closed' || skipped) return null;
  if (!analytics?.enabled || analytics.realm !== 'cloud' || !can(me, 'billing:manage') || plan?.plan !== 'trial' || plan.internal) return null;
  const msLeft = Date.parse(plan.trial_ends_at) - (now ?? clock).getTime();
  if (!(msLeft <= 7 * dayMs)) return null;

  const toggle = () => {
    if (open) { setOpen(false); return; }
    if (!reported.current) { reported.current = true; track('survey_opened', { survey: 'switch_blocker_v1' }); }
    setOpen(true);
  };
  const send = () => {
    const text = answer.trim();
    if (!text) return;
    if (!track('survey_answered', { survey: 'switch_blocker_v1', answer: text })) { setFailed(true); return; }
    remember();
    setDone('thanks');
  };
  const dismiss = () => {
    track('survey_dismissed', { survey: 'switch_blocker_v1' });
    remember();
    setDone('closed');
  };
  return (
    <span className="trial-survey">
      <span aria-hidden="true" className="trial-survey-dot">·</span>
      <span>{billingMessages.surveyPrompt}</span>
      <button type="button" ref={(node) => { trigger.current = node; refs.setReference(node); }} className="trial-survey-open"
        aria-expanded={open} aria-controls={open ? layerId : undefined} onClick={toggle}>{billingMessages.surveyOpen}</button>
      <button type="button" className="icon-button trial-survey-hide" aria-label={billingMessages.surveyHide}
        data-tip={billingMessages.surveyHide} onClick={dismiss}><XIcon /></button>
      {open && phone && <span className="trial-survey-backdrop" aria-hidden="true" />}
      {open && <div id={layerId} role="dialog" aria-labelledby={labelId}
        ref={(node) => { layer.current = node; refs.setFloating(node); }}
        className={phone ? 'trial-survey-layer trial-survey-sheet' : 'trial-survey-layer'} style={phone ? undefined : floatingStyles}>
        <label id={labelId} htmlFor={`${id}-answer`}>{billingMessages.surveyQuestion}</label>
        <textarea id={`${id}-answer`} autoFocus maxLength={maxAnswer} value={answer}
          onChange={(event) => { setAnswer(event.target.value); setFailed(false); }} />
        <p className="trial-survey-call">{billingMessages.surveyCallLead}{' '}
          <a href={billingMessages.surveyCallUrl} target="_blank" rel="noreferrer">{billingMessages.surveyCall}</a></p>
        {failed && <p role="alert" className="error-copy">{billingMessages.surveyFailed}</p>}
        <div className="button-row">
          <button type="button" onClick={dismiss}>{billingMessages.surveyNotNow}</button>
          <button type="button" className="primary-action" disabled={answer.trim() === ''} onClick={send}>{billingMessages.surveySend}</button>
        </div>
      </div>}
    </span>
  );
}
