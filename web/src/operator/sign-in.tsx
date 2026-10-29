import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { ApiError, api } from './api';
import type { SignInStep } from './api';
import { CommandLine, when } from './copy';
import { ConsoleLogo } from './logo';
import { m } from './messages';
import { navigate } from './router';

// takeToken reads the link's token once and takes it out of the address at once, so it never stays in the
// history, a bookmark or a Referer (design ops-uiux, decision 6).
export function takeToken(): string {
  const params = new URLSearchParams(window.location.search);
  const token = params.get('token') ?? '';
  if (token) {
    params.delete('token');
    const rest = params.toString();
    window.history.replaceState(null, '', window.location.pathname + (rest ? `?${rest}` : ''));
  }
  return token;
}

type State =
  | { kind: 'continue' }
  | { kind: 'busy' }
  | { kind: 'code'; step: SignInStep; wrong?: string }
  | { kind: 'no-totp' }
  | { kind: 'locked'; until: string; email?: string }
  | { kind: 'link'; title: string; used?: boolean }
  | { kind: 'no-link' };

const pairCommand = (email = '<your email>', reset = false) => `docker compose exec server /app/collie operator pair --email ${email}${reset ? ' --reset-totp' : ''}`;

function linkError(error: unknown): State {
  if (error instanceof ApiError) {
    if (error.code === 'operator_locked') return { kind: 'locked', until: String(error.body.locked_until ?? '') };
    if (error.code === 'operator_link_expired') return { kind: 'link', title: m.linkExpired };
    if (error.code === 'operator_link_used') return { kind: 'link', title: m.linkUsed, used: true };
  }
  return { kind: 'link', title: m.linkInvalid };
}

export function SignIn({ onSignedIn }: { onSignedIn: () => void }) {
  const token = useRef<string>('');
  const [state, setState] = useState<State>({ kind: 'continue' });
  const [code, setCode] = useState('');
  const main = useRef<HTMLElement>(null);
  useEffect(() => {
    token.current = takeToken();
    if (!token.current) setState({ kind: 'no-link' });
  }, []);
  useEffect(() => { main.current?.focus(); }, [state.kind]);

  const finish = (step: SignInStep) => {
    if (step.step === 'done') {
      if (step.totp_required === false) setState({ kind: 'no-totp' });
      else { onSignedIn(); navigate('/', true); }
      return;
    }
    setCode('');
    setState({ kind: 'code', step });
  };
  const redeem = async () => {
    setState({ kind: 'busy' });
    try {
      finish(await api.redeem(token.current));
    } catch (error) {
      setState(linkError(error));
    }
  };
  const confirm = async (event: FormEvent) => {
    event.preventDefault();
    if (state.kind !== 'code') return;
    const step = state.step;
    try {
      finish(await api.confirmCode(step.ticket ?? '', code));
    } catch (error) {
      if (error instanceof ApiError && error.code === 'operator_code_wrong') {
        setState({ kind: 'code', step, wrong: m.codeWrong(Number(error.body.tries_left ?? 0)) });
        setCode('');
      } else if (error instanceof ApiError && error.code === 'operator_locked') {
        setState({ kind: 'locked', until: String(error.body.locked_until ?? ''), email: step.email });
      } else {
        setState(linkError(error));
      }
    }
  };

  let body;
  switch (state.kind) {
    case 'continue':
    case 'busy':
      body = <>
        <h1 id="sign-title">{m.signInTitle}</h1>
        <p>{m.signInLead}</p>
        <button type="button" className="primary-action" disabled={state.kind === 'busy'} onClick={() => void redeem()}>{state.kind === 'busy' ? m.continuing : m.continue}</button>
      </>;
      break;
    case 'code': {
      const enroll = state.step.step === 'enroll';
      body = <form onSubmit={(event) => void confirm(event)} noValidate>
        <h1 id="sign-title">{enroll ? m.enrollTitle : m.codeTitle}</h1>
        <p>{enroll ? m.enrollLead : m.codeLead}</p>
        {enroll && <div className="ops-enroll">
          {state.step.qr_svg && <img className="qr" alt={m.qrLabel} src={`data:image/svg+xml;utf8,${encodeURIComponent(state.step.qr_svg)}`} width={176} height={176} />}
          <CommandLine command={(state.step.key ?? '').replace(/(.{4})/g, '$1 ').trim()} label={m.copyKey} />
        </div>}
        <div className="field">
          <label className="field-label" htmlFor="totp-code">{m.authenticatorCode}</label>
          <input id="totp-code" inputMode="numeric" autoComplete="one-time-code" maxLength={6} spellCheck={false} value={code}
            aria-invalid={state.wrong ? true : undefined} aria-describedby={state.wrong ? 'totp-wrong' : undefined}
            onChange={(event) => setCode(event.target.value.replace(/\D/g, ''))} />
          {state.wrong && <span className="error-copy field-note" id="totp-wrong" role="alert">{state.wrong}</span>}
        </div>
        <button type="submit" className="primary-action" disabled={code.length !== 6}>{enroll ? m.turnOnAndSignIn : m.signIn}</button>
        {!enroll && <details><summary>{m.lostAuthenticator}</summary><p>{m.lostHelp}</p><CommandLine command={pairCommand(state.step.email, true)} /></details>}
      </form>;
      break;
    }
    case 'no-totp':
      body = <>
        <h1 id="sign-title">{m.signInTitle}</h1>
        <p className="warning-note">{m.noTotpNote}</p>
        <CommandLine command="docker compose exec server /app/collie config set operator.require_totp=true" />
        <button type="button" className="primary-action" onClick={() => { onSignedIn(); navigate('/', true); }}>{m.goOverview}</button>
      </>;
      break;
    case 'locked':
      body = <>
        <h1 id="sign-title">{m.lockedTitle}</h1>
        <p>{m.lockedUntil(when(state.until))}</p>
        <p>{m.lostHelp}</p>
        <CommandLine command={pairCommand(state.email, true)} />
      </>;
      break;
    case 'link':
      body = <>
        <h1 id="sign-title">{state.title}</h1>
        <p>{m.linkHelp}</p>
        <CommandLine command={pairCommand()} />
        {state.used && <><p>{m.usedHelp}</p><CommandLine command="docker compose exec server /app/collie operator sessions" /></>}
      </>;
      break;
    case 'no-link':
      body = <>
        <h1 id="sign-title">{m.noLinkTitle}</h1>
        <p>{m.noLinkLead}</p>
        <CommandLine command={pairCommand()} />
      </>;
      break;
  }
  return (
    <div className="ops-shell ops-shell-sign">
      <header className="ops-band"><ConsoleLogo /><div className="ops-id"><strong>{m.consoleName}</strong><span className="ops-host">{window.location.host}</span></div></header>
      <main id="main" className="ops-sign" tabIndex={-1} ref={main} aria-labelledby="sign-title">
        <section className="sign-card" aria-live="polite">{body}</section>
      </main>
    </div>
  );
}
