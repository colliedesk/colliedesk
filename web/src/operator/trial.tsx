import { useEffect, useState } from 'react';
import { ApiError, api } from './api';
import type { Trial } from './api';
import { ConsoleDialog } from './dialog';
import { AlertIcon } from './icons';
import { m } from './messages';

// The thresholds at which the polite live region speaks: a screen reader hears the time left at these
// moments, never every second (UI review risk b). The visible timer is not live.
export const announceAt = [300, 60, 30];

export function clock(seconds: number): string {
  const left = Math.max(0, seconds);
  return `${Math.floor(left / 60)}:${String(left % 60).padStart(2, '0')}`;
}

// announcement is what the live region says with `left` seconds to go: the last threshold reached.
export function announcement(left: number): string {
  if (left <= 0) return m.trialEnded;
  const reached = announceAt.filter((threshold) => left <= threshold);
  if (reached.length === 0) return '';
  return m.trialLeftSpoken(Math.min(...reached));
}

// TrialStrip is the band under the server band while a trial change runs (D12): what is tried, the time left,
// what this process saw, and the two ways out. Confirming from this page proves the console still works.
export default function TrialStrip({ trial, totp, onDecided }: { trial: Trial; totp: boolean; onDecided: () => void }) {
  const ends = Date.parse(trial.expires_at);
  const [left, setLeft] = useState(() => Math.round((ends - Date.now()) / 1000));
  const [deciding, setDeciding] = useState<'confirm' | 'revert' | null>(null);
  useEffect(() => {
    const timer = window.setInterval(() => setLeft(Math.round((ends - Date.now()) / 1000)), 1000);
    return () => window.clearInterval(timer);
  }, [ends]);
  const evidence = trial.evidence;
  const cloud = trial.keys.some((key) => key.startsWith('cloud_auth.') || key === 'clerk.publishable_key');
  const proxies = trial.keys.includes('trusted_proxies');
  const page = evidence.help_center;
  const doubtful = (cloud && evidence.cloud_sign_ins_ok === 0 && evidence.cloud_sign_ins_refused > 0) || (proxies && evidence.client_ips === 1) || (page !== undefined && !page.ok);
  return <section className="ops-strip" aria-label={m.trialInProgress}>
    <AlertIcon />
    <span><strong>{m.tryingVersion(trial.version, trial.keys.join(', '))}</strong> {m.revertsTo(trial.parent_version)}{' '}
      <span className="timer" role="timer" aria-live="off">{clock(left)}</span> {m.unlessConfirmed}
      <span className="strip-more"> {proxies && m.trialYourIP(evidence.your_ip, evidence.client_ips)}{cloud && m.trialSignIns(evidence.cloud_sign_ins_ok, evidence.cloud_sign_ins_refused)}
        {page?.url && m.trialHelpCenter(page.url, page.ok, page.status)}</span></span>
    <span className="sr-only" aria-live="polite">{announcement(left)}</span>
    <div className="button-row">
      <button type="button" onClick={() => setDeciding('revert')}>{m.revertNow}</button>
      <button type="button" className="primary-action" onClick={() => setDeciding('confirm')}>{m.confirmVersion(trial.version)}</button>
    </div>
    {deciding && <Decide trial={trial} action={deciding} totp={totp} doubtful={deciding === 'confirm' && doubtful}
      onClose={() => setDeciding(null)} onDone={() => { setDeciding(null); onDecided(); }} />}
  </section>;
}

function Decide({ trial, action, totp, doubtful, onClose, onDone }: {
  trial: Trial; action: 'confirm' | 'revert'; totp: boolean; doubtful: boolean; onClose: () => void; onDone: () => void;
}) {
  const [code, setCode] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const ready = !totp || /^\d{6}$/.test(code);
  const submit = async () => {
    if (!ready || busy) return;
    setBusy(true);
    setError('');
    try {
      await api.trial(action, { version: trial.version, reauth_code: totp ? code : undefined });
      onDone();
    } catch (failure) {
      setError(failure instanceof ApiError && failure.code === 'no_trial' ? m.trialGone : failure instanceof ApiError && failure.code === 'reauth_code_invalid' ? m.codeNoMatch : m.nothingSaved);
      setCode('');
    } finally {
      setBusy(false);
    }
  };
  const title = action === 'confirm' ? m.confirmVersion(trial.version) : m.revertTo(trial.parent_version);
  return <ConsoleDialog title={title} help={action === 'confirm' ? m.confirmTrialLead : m.revertTrialLead} onClose={onClose} closeLabel={m.cancel} onSubmit={() => void submit()}
    footer={<><button type="button" onClick={onClose}>{m.cancel}</button>
      <button type="submit" className={action === 'confirm' ? 'primary-action' : 'danger-action'} disabled={!ready || busy}>{title}</button></>}>
    {doubtful && <p className="warning-note" role="alert">{m.trialDoubt}</p>}
    {totp && <div className="field"><label className="field-label" htmlFor="trial-code">{m.confirmItsYou}: {m.authenticatorCode}</label>
      <input id="trial-code" inputMode="numeric" autoComplete="one-time-code" maxLength={6} spellCheck={false} value={code}
        onChange={(event) => setCode(event.target.value.replace(/\D/g, ''))} /></div>}
    {error && <p className="error-copy" role="alert">{error}</p>}
  </ConsoleDialog>;
}
