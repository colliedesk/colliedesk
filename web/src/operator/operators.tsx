import { useEffect, useState } from 'react';
import { ApiError, api, cloudPublishableKey } from './api';
import type { Operator, OperatorSession } from './api';
import type { ConsoleData } from './app';
import { CommandLine, when } from './copy';
import { ConsoleDialog } from './dialog';
import { m } from './messages';
import { pm } from './page-messages';

const allPermissions = ['operator_config:write', 'operator:manage', 'operator_requests:cancel'];

export function Operators({ data }: { data: ConsoleData }) {
  const [team, setTeam] = useState<{ operators: Operator[]; sessions: OperatorSession[] } | null>(null);
  const [dialog, setDialog] = useState<'invite' | 'token' | { revoke: Operator } | { session: OperatorSession } | null>(null);
  const [shown, setShown] = useState<{ title: string; value: string; note: string } | null>(null);
  const load = () => { void api.team().then(setTeam, () => setTeam({ operators: [], sessions: [] })); };
  useEffect(load, []);
  if (!team) return <div className="ops-page-inner" aria-busy="true"><div className="skeleton" /></div>;
  const me = data.me.operator.id;
  const active = team.operators.filter((operator) => !operator.revoked_at);
  return <div className="ops-page-inner">
    <header className="page-head"><div><h1 id="page-title">{m.operators}</h1><p>{pm.operatorsLead}</p></div>
      <div className="button-row"><button type="button" onClick={() => setDialog('invite')}>{pm.inviteOperator}</button><button type="button" onClick={() => setDialog('token')}>{pm.createToken}</button></div></header>
    {active.length === 1 && <p className="warning-note">{pm.onlyOperator}</p>}
    <section className="settings-card"><div className="section-heading"><h2>{pm.operatorsSection}</h2></div>
      <div className="settings-body ops-table-wrap" tabIndex={0} role="region" aria-label={pm.operatorsSection}><table className="ops-table">
        <thead><tr><th scope="col">{pm.email}</th><th scope="col">{pm.authenticator}</th><th scope="col"><span className="sr-only">{pm.revoke}</span></th></tr></thead>
        <tbody>{team.operators.map((operator) => <tr key={operator.id}>
          <td data-label={pm.email}>{operator.email} {operator.id === me && <span className="badge">{pm.you}</span>} {operator.link_unused && <span className="badge">{pm.linkNotUsed}</span>}</td>
          <td data-label={pm.authenticator}>{operator.revoked_at ? pm.ended : operator.totp_enabled_at ? m.on : m.off}</td>
          <td>{!operator.revoked_at && operator.id !== me && <button type="button" className="danger-action" onClick={() => setDialog({ revoke: operator })}>{pm.revoke}</button>}</td></tr>)}</tbody></table></div>
    </section>
    <section className="settings-card"><div className="section-heading"><h2>{pm.sessions}</h2></div>
      <div className="settings-body ops-table-wrap" tabIndex={0} role="region" aria-label={pm.sessions}><table className="ops-table">
        <thead><tr><th scope="col">{pm.tokenName}</th><th scope="col">{pm.email}</th><th scope="col">{pm.lastSeen}</th><th scope="col">{pm.expires}</th><th scope="col"><span className="sr-only">{pm.revoke}</span></th></tr></thead>
        <tbody>{team.sessions.map((session) => <tr key={session.id}>
          <td data-label={pm.tokenName}>{session.id === data.me.session.id ? pm.thisBrowser : session.label} <code>{session.kind}</code></td>
          <td data-label={pm.email}>{session.email}</td><td data-label={pm.lastSeen} className="ops-when">{when(session.last_seen_at)}</td><td data-label={pm.expires} className="ops-when">{when(session.expires_at)}</td>
          <td><button type="button" className="danger-action" onClick={() => setDialog({ session })}>{session.id === data.me.session.id ? m.signOut : pm.revoke}</button></td></tr>)}</tbody></table></div>
    </section>
    {dialog && <Act data={data} dialog={dialog} onClose={() => setDialog(null)} onShown={(value) => { setDialog(null); setShown(value); load(); }} onDone={() => { setDialog(null); load(); }} />}
    {shown && <ShownOnce {...shown} onClose={() => setShown(null)} />}
  </div>;
}

function Act({ data, dialog, onClose, onShown, onDone }: { data: ConsoleData; dialog: 'invite' | 'token' | { revoke: Operator } | { session: OperatorSession };
  onClose: () => void; onShown: (value: { title: string; value: string; note: string }) => void; onDone: () => void }) {
  const [email, setEmail] = useState('');
  const [label, setLabel] = useState('');
  const [permissions, setPermissions] = useState<string[]>([]);
  const [code, setCode] = useState('');
  const [error, setError] = useState('');
  const totp = data.me.operator.totp_enabled;
  const codeReady = !totp || /^\d{6}$/.test(code);
  const run = async () => {
    try {
      if (dialog === 'invite') {
        const made = await api.invite(email, code);
        onShown({ title: pm.inviteLink, value: made.link, note: cloudPublishableKey() ? `${pm.inviteAgain} ${pm.inviteCloud}` : pm.inviteAgain });
      } else if (dialog === 'token') {
        const made = await api.createToken(label, permissions, code);
        const endpoint = `${data.me.console.path_mode ? window.location.origin : data.me.console.origin}/mcp/operator`;
        onShown({ title: pm.oneTimeTitle, value: made.token, note: pm.tokenEndpoint(endpoint) });
      } else if ('revoke' in dialog) {
        await api.revokeOperator(dialog.revoke.id, code);
        onDone();
      } else {
        await api.revokeSession(dialog.session.id, code);
        onDone();
      }
    } catch (problem) {
      setError(problem instanceof ApiError ? problem.message : m.nothingSaved);
    }
  };
  const title = dialog === 'invite' ? pm.inviteOperator : dialog === 'token' ? pm.createToken : 'revoke' in dialog ? pm.revokeOperatorTitle(dialog.revoke.email) : pm.revokeSessionTitle(dialog.session.label || pm.thisBrowser, dialog.session.email);
  const ready = codeReady && (dialog !== 'invite' || email.includes('@')) && (dialog !== 'token' || label.trim() !== '');
  return <ConsoleDialog title={title} help={typeof dialog === 'object' ? ('revoke' in dialog ? pm.revokeOperatorLead : pm.revokeSessionLead) : undefined} onClose={onClose} closeLabel={pm.cancel}
    alert={typeof dialog === 'object'} onSubmit={() => { if (ready) void run(); }}
    footer={<><button type="button" onClick={onClose}>{pm.cancel}</button>
      <button type="submit" className={typeof dialog === 'object' ? 'danger-action' : 'primary-action'} disabled={!ready}>{dialog === 'invite' ? pm.invite : dialog === 'token' ? pm.createToken : pm.revoke}</button></>}>
    {dialog === 'invite' && <div className="field"><label className="field-label" htmlFor="invite-email">{pm.email}</label>
      <input id="invite-email" type="email" autoComplete="off" value={email} onChange={(event) => setEmail(event.target.value)} /></div>}
    {dialog === 'token' && <>
      <div className="field"><label className="field-label" htmlFor="token-label">{pm.tokenName}</label><input id="token-label" value={label} onChange={(event) => setLabel(event.target.value)} /></div>
      <fieldset className="check-list"><legend>{pm.tokenAsks}</legend>
        <label><input type="checkbox" checked disabled />{pm.permissionNames['operator:read']}</label>
        {allPermissions.map((name) => <label key={name}><input type="checkbox" checked={permissions.includes(name)}
          onChange={(event) => setPermissions(event.target.checked ? [...permissions, name] : permissions.filter((item) => item !== name))} />{pm.permissionNames[name]}</label>)}
      </fieldset>
    </>}
    {totp && <div className="field"><label className="field-label" htmlFor="act-code">{pm.confirmItsYou}: {m.authenticatorCode}</label>
      <input id="act-code" inputMode="numeric" autoComplete="one-time-code" maxLength={6} value={code} onChange={(event) => setCode(event.target.value.replace(/\D/g, ''))} /></div>}
    {error && <p className="error-copy" role="alert">{error}</p>}
  </ConsoleDialog>;
}

// ShownOnce shows a link or token that will never be shown again: no close button, and Escape or a click
// outside only says to copy it first (design ops-uiux, review 2).
export function ShownOnce({ title, value, note, onClose }: { title: string; value: string; note: string; onClose: () => void }) {
  const [reminded, setReminded] = useState(false);
  return <ConsoleDialog title={title} alert onClose={() => setReminded(true)} closeLabel={pm.done}
    footer={<button type="button" className="primary-action" onClick={onClose}>{pm.done}</button>}>
    <CommandLine command={value} />
    <p>{note}</p>
    {reminded && <p className="error-copy" role="alert">{pm.copyBeforeClose}</p>}
  </ConsoleDialog>;
}
