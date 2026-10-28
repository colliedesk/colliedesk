import { useEffect, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { z } from 'zod';
import { InvalidRequestError, request } from '../../api/client';
import type { RequestBody } from '../../api/client';
import type { Generated } from '../../api/schemas';
import { formatDate } from '../../lib/format-date';
import { messages } from '../../messages';
import { domainMessages } from './help-center-domain-messages';

const domainPath = '/api/v1/help-center/domain';
const date = z.string().datetime().nullable();
const recordSchema = z.object({
  type: z.enum(['CNAME', 'A', 'AAAA', 'TXT']), name: z.string(), value: z.string(), purpose: z.enum(['route_and_verify', 'route', 'verify']),
}) satisfies z.ZodType<Generated<'HelpCenterDomainRecord'>>;
const domainSchema = z.object({
  hostname: z.string().nullable(), status: z.enum(['none', 'pending', 'active']), records: z.array(recordSchema),
  created_at: date, verified_at: date, checked_at: date, check_error: z.string().nullable(), certificate_requested_at: date,
  failing_since: date, pending_reason: z.enum(['proof_lost', 'taken_over']).nullable(), awaiting_payment: z.boolean(),
}) satisfies z.ZodType<Generated<'HelpCenterDomain'>>;

export type HelpCenterDomain = z.infer<typeof domainSchema>;
type DomainRecord = z.infer<typeof recordSchema>;

export const domainApi = {
  get: () => request(domainPath, domainSchema),
  set: (hostname: string) => request(domainPath, domainSchema, { method: 'PUT', body: JSON.stringify({ hostname } satisfies RequestBody<'putHelpCenterDomain'>) }),
  check: () => request(`${domainPath}/check`, domainSchema, { method: 'POST' }),
  remove: () => request(domainPath, domainSchema, { method: 'DELETE' }),
};

const failure = (error: unknown) => error instanceof InvalidRequestError ? error.message : messages.settingsActionFailed;
// An active domain stops serving when no check has proved it for this long (the server's release time).
const releaseAfterMs = 7 * 24 * 60 * 60 * 1000;

// HelpCenterDomainSection shows the help center's custom domain in its card: its state, the DNS records to
// set (each copyable), the last check and what it found. Admins set, check and remove it; every agent sees it.
export function HelpCenterDomainSection({ canManage, timezone }: { canManage: boolean; timezone: string }) {
  const client = useQueryClient();
  const current = useQuery({ queryKey: ['help-center-domain'], queryFn: domainApi.get });
  // The help center's address follows the domain, so its card reads it again after every change.
  const applied = (saved: HelpCenterDomain) => {
    client.setQueryData(['help-center-domain'], saved);
    void client.invalidateQueries({ queryKey: ['help-center'], exact: true });
  };
  const set = useMutation({ mutationFn: domainApi.set, onSuccess: applied });
  const check = useMutation({ mutationFn: domainApi.check, onSuccess: applied, onError: () => void current.refetch() });
  const remove = useMutation({ mutationFn: domainApi.remove, onSuccess: applied });
  const [hostname, setHostname] = useState('');
  useEffect(() => setHostname(current.data?.hostname ?? ''), [current.data?.hostname]);
  // Removing takes the help center off the domain at once, so it asks first; Cancel is the focused default.
  const [confirming, setConfirming] = useState(false);
  const removeButton = useRef<HTMLButtonElement>(null);
  const returnFocus = useRef(false);
  const cancelRemove = () => { returnFocus.current = true; setConfirming(false); };
  useEffect(() => {
    if (confirming || !returnFocus.current) return;
    returnFocus.current = false;
    removeButton.current?.focus();
  }, [confirming]);

  if (current.isError) return <p role="alert" className="error-copy">{domainMessages.loadFailed}</p>;
  if (!current.data) return null;
  const domain = current.data;
  const error = set.error ?? check.error ?? remove.error;
  return <section className="stacked-form" aria-labelledby="help-center-domain-heading">
    <h3 id="help-center-domain-heading">{domainMessages.heading}</h3>
    <p className="muted-copy">{domainMessages.description}</p>
    {domain.awaiting_payment && <p className="muted-copy">{domainMessages.trial}</p>}
    {domain.status !== 'active' && <p className="muted-copy"><strong>{domainMessages.cutoverHeading}</strong> {domainMessages.cutover}</p>}
    <p role="status" className="saved-state" data-on={domain.status === 'active' && !domain.awaiting_payment ? 'true' : 'false'}>
      {domain.status === 'active' && domain.awaiting_payment && domain.hostname ? domainMessages.verifiedUnpaid(domain.hostname) : domainMessages.state(domain.status, domain.hostname)}</p>
    {domain.records.length > 0 && <div className="email-channel-dns">
      <p className="muted-copy">{domain.status === 'active' ? domainMessages.recordsKeep : domainMessages.recordsHelp}</p>
      {domain.records.map((record) => <DnsRecord key={`${record.type} ${record.name}`} record={record} />)}
    </div>}
    {domain.status === 'pending' && domain.pending_reason && domain.hostname && <p className="warning-note">{domainMessages.released(domain.pending_reason, domain.hostname)}</p>}
    {domain.status === 'active' && domain.failing_since && domain.hostname && <p className="warning-note">{domainMessages.failing(
      formatDate(domain.failing_since, timezone), formatDate(new Date(Date.parse(domain.failing_since) + releaseAfterMs).toISOString(), timezone), domain.hostname)}</p>}
    {domain.check_error && <p className={domain.status === 'active' ? 'warning-note' : 'muted-copy'}>{domainMessages.lastFinding(domain.check_error)}</p>}
    {domain.checked_at && <p className="muted-copy">{domainMessages.lastChecked(formatDate(domain.checked_at, timezone))}</p>}
    {domain.verified_at && <p className="muted-copy">{domainMessages.verified(formatDate(domain.verified_at, timezone))}</p>}
    {error && <p role="alert" className="error-copy">{failure(error)}</p>}
    {canManage
      ? <>
          <form className="form-row" onSubmit={(event) => { event.preventDefault(); if (!set.isPending) { check.reset(); remove.reset(); set.mutate(hostname.trim()); } }}>
            <label className="field"><span>{domainMessages.hostname}</span>
              <input value={hostname} maxLength={253} placeholder="help.example.com" autoComplete="off" spellCheck={false} required
                onChange={(event) => setHostname(event.target.value)} /></label>
            <button type="submit" className="primary-action" disabled={set.isPending}>{set.isPending ? domainMessages.saving : domainMessages.save}</button>
          </form>
          {domain.status !== 'none' && !confirming && <span className="button-row">
            <button type="button" disabled={check.isPending} onClick={() => { set.reset(); remove.reset(); check.mutate(); }}>{check.isPending ? domainMessages.checking : domainMessages.checkNow}</button>
            <button type="button" className="danger-action" ref={removeButton} onClick={() => setConfirming(true)}>{domainMessages.remove}</button>
          </span>}
          {confirming && domain.hostname && <div className="confirm-panel" role="group" aria-labelledby="remove-domain-question"
            onKeyDown={(event) => { if (event.key === 'Escape') cancelRemove(); }}>
            <p><strong id="remove-domain-question">{domainMessages.removeQuestion}</strong> {domainMessages.removeConsequence(domain.hostname)}</p>
            <span className="button-row">
              <button type="button" className="danger-action" onClick={() => { setConfirming(false); set.reset(); check.reset(); remove.mutate(); }}>{domainMessages.confirmRemove}</button>
              <button type="button" autoFocus onClick={cancelRemove}>{messages.cancel}</button>
            </span>
          </div>}
        </>
      : <p className="muted-copy">{domainMessages.adminsOnly}</p>}
  </section>;
}

function DnsRecord({ record }: { record: DomainRecord }) {
  const [copied, setCopied] = useState<'name' | 'value'>();
  const copy = (part: 'name' | 'value') => void navigator.clipboard.writeText(record[part]).then(() => setCopied(part));
  return <div className="copy-field" role="group" aria-label={domainMessages.record(record.type, record.purpose)}>
    <strong>{domainMessages.record(record.type, record.purpose)}</strong>
    <label className="invite-link"><span className="field-label">{domainMessages.recordName}</span><input readOnly value={record.name} /></label>
    <label className="invite-link"><span className="field-label">{domainMessages.recordValue}</span><input readOnly value={record.value} /></label>
    <span className="button-row">
      <button type="button" onClick={() => copy('name')} aria-label={domainMessages.copyName(record.type)}>{domainMessages.copyNameShort}</button>
      <button type="button" onClick={() => copy('value')} aria-label={domainMessages.copyValue(record.type)}>{domainMessages.copyValueShort}</button>
      {copied && <span role="status">{domainMessages.copied}</span>}
    </span>
  </div>;
}
