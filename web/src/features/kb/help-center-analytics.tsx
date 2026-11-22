import { useEffect, useState } from 'react';
import type { FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { z } from 'zod';
import { InvalidRequestError, request } from '../../api/client';
import type { RequestBody } from '../../api/client';
import { messages } from '../../messages';
import { HelpTip } from '../../ui/help-tip';
import { analyticsMessages as text } from './help-center-analytics-messages';
import { AnalyticsStatus, TestEvent, WhatIsSent, analyticsSchema } from './help-center-analytics-status';
import type { Analytics, Provider } from './help-center-analytics-status';

const path = '/api/v1/help-center/analytics';
const queryKey = ['help-center-analytics'];
// A refused field is named, so its message shows next to the control.
const refusalFields = z.object({ fields: z.object({}).catchall(z.string()) });

type Change = RequestBody<'putHelpCenterAnalytics'>;

const analyticsApi = {
  get: () => request(path, analyticsSchema),
  save: (change: Change) => request(path, analyticsSchema, { method: 'PUT', body: JSON.stringify(change) }),
};

function fieldMessages(error: unknown): Record<string, string> {
  if (!(error instanceof InvalidRequestError)) return {};
  const parsed = refusalFields.safeParse(error.detail);
  return parsed.success ? parsed.data.fields : {};
}

const serviceName = (provider: Provider) => provider === 'ga4' ? text.ga4 : text.plausible;

// HelpCenterAnalyticsSection is the Analytics part of the help center's card: the workspace's own GA4 or
// Plausible gets each reader's page view from the server. Admins choose and test it; every agent sees its state.
export function HelpCenterAnalyticsSection({ canManage, timezone }: { canManage: boolean; timezone: string }) {
  const current = useQuery({ queryKey, queryFn: analyticsApi.get });
  if (current.isError) return <p role="alert" className="error-copy">{text.loadFailed}</p>;
  if (!current.data) return null;
  const settings = current.data;
  return <section className="stacked-form hc-section" aria-labelledby="help-center-analytics-heading">
    <h3 id="help-center-analytics-heading">{text.heading}</h3>
    <p className="muted-copy">{text.description} <HelpTip label={text.heading} text={text.descriptionTip} link={{ href: text.guide, label: text.learnMore }} /></p>
    {canManage
      ? <AnalyticsForm settings={settings} timezone={timezone} />
      : <>
          {settings.provider && <p>{text.goesTo(serviceName(settings.provider), settings.provider === 'ga4' ? settings.ga4_measurement_id ?? '' : settings.plausible_domain ?? '')}</p>}
          <AnalyticsStatus settings={settings} timezone={timezone} />
          {settings.provider && <WhatIsSent provider={settings.provider} />}
          <p className="muted-copy">{text.adminsOnly}</p>
        </>}
  </section>;
}

function AnalyticsForm({ settings, timezone }: { settings: Analytics; timezone: string }) {
  const client = useQueryClient();
  const [provider, setProvider] = useState<Provider | null>(settings.provider);
  const [enabled, setEnabled] = useState(settings.enabled);
  const [measurementId, setMeasurementId] = useState(settings.ga4_measurement_id ?? '');
  const [secret, setSecret] = useState('');
  const [domain, setDomain] = useState(settings.plausible_domain ?? '');
  const [baseUrl, setBaseUrl] = useState(settings.plausible_base_url);
  const save = useMutation({ mutationFn: analyticsApi.save, onSuccess: (saved) => { client.setQueryData(queryKey, saved); setSecret(''); } });
  useEffect(() => {
    setProvider(settings.provider);
    setEnabled(settings.enabled);
    setMeasurementId(settings.ga4_measurement_id ?? '');
    setDomain(settings.plausible_domain ?? '');
    setBaseUrl(settings.plausible_base_url);
  }, [settings]);

  const fields = fieldMessages(save.error);
  const failed = save.error && Object.keys(fields).length === 0
    ? (save.error instanceof InvalidRequestError ? save.error.message : messages.settingsActionFailed) : undefined;
  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (save.isPending || !provider) return;
    // Both services' fields are sent as they are, so switching service keeps the other one's settings. An empty
    // secret field keeps the saved secret.
    save.mutate({ provider, enabled, ga4_measurement_id: measurementId.trim() || null, plausible_domain: domain.trim() || null,
      plausible_base_url: baseUrl.trim() || null, ...(secret.trim() ? { ga4_api_secret: secret.trim() } : {}) });
  };
  // Removing the secret saves the stored settings, not unsaved edits; GA4 cannot send without it, so it stops.
  const removeSecret = () => {
    if (save.isPending || !settings.provider) return;
    save.mutate({ provider: settings.provider, enabled: settings.provider === 'ga4' ? false : settings.enabled,
      ga4_measurement_id: settings.ga4_measurement_id, plausible_domain: settings.plausible_domain,
      plausible_base_url: settings.plausible_base_url, ga4_api_secret: '' });
  };
  return <>
    <form className="stacked-form" onSubmit={submit}>
      <fieldset className="field"><legend>{text.service}</legend>
        {(['ga4', 'plausible'] as const).map((value) => <label key={value} className="field-inline">
          <input type="radio" name="help-center-analytics-provider" value={value} checked={provider === value} onChange={() => setProvider(value)} />
          <span>{serviceName(value)}</span></label>)}
      </fieldset>
      {provider === 'ga4' && <>
        <Field id="analytics-ga4-id" label={text.measurementId} help={text.measurementIdHelp} value={measurementId} maxLength={20} problem={fields.ga4_measurement_id} onChange={setMeasurementId} />
        <Field id="analytics-ga4-secret" label={text.apiSecret} help={settings.ga4_api_secret_set ? text.apiSecretSaved : text.apiSecretHelp} value={secret} maxLength={100} secret
          problem={fields.ga4_api_secret} onChange={setSecret} />
        {settings.ga4_api_secret_set && <span className="button-row">
          <button type="button" className="secondary-action" disabled={save.isPending} onClick={removeSecret}>{text.removeSecret}</button></span>}
      </>}
      {provider === 'plausible' && <>
        <Field id="analytics-plausible-domain" label={text.domain} help={text.domainHelp} value={domain} maxLength={253} problem={fields.plausible_domain} onChange={setDomain} />
        <Field id="analytics-plausible-base" label={text.baseUrl} help={text.baseUrlHelp} value={baseUrl} maxLength={200} problem={fields.plausible_base_url} onChange={setBaseUrl} />
      </>}
      {provider && <WhatIsSent provider={provider} />}
      {provider && <label className="field-inline"><input type="checkbox" checked={enabled} onChange={(event) => setEnabled(event.target.checked)} /><span>{text.enable}</span></label>}
      {(failed || fields.provider) && <p role="alert" className="error-copy">{failed ?? fields.provider}</p>}
      {provider && <span className="button-row"><button className="primary-action" type="submit" disabled={save.isPending}>{save.isPending ? text.saving : text.save}</button>
        {save.isSuccess && <span role="status" className="saved-state" data-on="true">{text.saved}</span>}</span>}
    </form>
    <AnalyticsStatus settings={settings} timezone={timezone} />
    {settings.provider && <TestEvent />}
  </>;
}

function Field({ id, label, help, value, maxLength, secret, problem, onChange }: { id: string; label: string; help: string; value: string; maxLength: number; secret?: boolean; problem?: string; onChange: (value: string) => void }) {
  return <div className="field"><label htmlFor={id}>{label}</label>
    <input id={id} type={secret ? 'password' : 'text'} value={value} maxLength={maxLength} autoComplete={secret ? 'new-password' : 'off'} spellCheck={false}
      aria-invalid={problem ? true : undefined} aria-describedby={`${id}-help`} onChange={(event) => onChange(event.target.value)} />
    <small id={`${id}-help`} className="muted-copy">{help}</small>
    {problem && <small role="alert" className="error-copy">{problem}</small>}</div>;
}
