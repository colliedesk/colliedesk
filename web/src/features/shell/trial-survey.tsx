import { useId, useState } from 'react';
import type { Me } from '../../api/schemas';
import { track } from '../../lib/product-analytics';
import { useNow } from '../../lib/use-now';
import { messages } from '../../messages';

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

// TrialSurvey asks a cloud trial admin one question in the last week of the trial. The typed answer
// leaves the browser only when the admin presses Send; it is the one analytics event that carries text.
export function TrialSurvey({ me, analytics, now }: TrialSurveyProps) {
  const clock = useNow('UTC');
  const [skipped] = useState(answeredBefore);
  const [done, setDone] = useState<'thanks' | 'closed' | null>(null);
  const [answer, setAnswer] = useState('');
  const [failed, setFailed] = useState(false);
  const labelId = useId();
  const plan = me.plan;
  if (done === 'thanks') return <section className="trial-survey"><p role="status">{messages.surveyThanks}</p></section>;
  if (done === 'closed' || skipped) return null;
  if (!analytics?.enabled || analytics.realm !== 'cloud' || me.agent.role !== 'admin' || plan?.plan !== 'trial') return null;
  const msLeft = Date.parse(plan.trial_ends_at) - (now ?? clock).getTime();
  if (!(msLeft <= 7 * dayMs)) return null;

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
    <section className="trial-survey" aria-labelledby={labelId}>
      <label id={labelId} htmlFor={`${labelId}-answer`}>{messages.surveyQuestion}</label>
      <textarea id={`${labelId}-answer`} maxLength={maxAnswer} value={answer} onChange={(event) => { setAnswer(event.target.value); setFailed(false); }} />
      {failed && <p role="alert" className="error-copy">{messages.surveyFailed}</p>}
      <div className="button-row">
        <button type="button" className="primary-action" disabled={answer.trim() === ''} onClick={send}>{messages.surveySend}</button>
        <button type="button" onClick={dismiss}>{messages.surveyNotNow}</button>
      </div>
    </section>
  );
}
