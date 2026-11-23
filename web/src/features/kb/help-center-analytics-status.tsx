import { useMutation } from '@tanstack/react-query';
import { z } from 'zod';
import { InvalidRequestError, request } from '../../api/client';
import type { Generated } from '../../api/schemas';
import { formatDate } from '../../lib/format-date';
import { messages } from '../../messages';
import { analyticsMessages as text } from './help-center-analytics-messages';
import { onHelpCenter, useHelpCenterTarget } from './help-center-target';

export const analyticsSchema = z.object({
  provider: z.enum(['ga4', 'plausible']).nullable(), enabled: z.boolean(),
  ga4_measurement_id: z.string().nullable(), ga4_api_secret_set: z.boolean(),
  plausible_domain: z.string().nullable(), plausible_base_url: z.string(),
  last_sent_at: z.string().datetime().nullable(), last_error: z.string().nullable(), last_error_at: z.string().datetime().nullable(),
  updated_at: z.string().datetime().nullable(), updated_by_name: z.string().nullable(),
}) satisfies z.ZodType<Generated<'HelpCenterAnalytics'>>;
export type Analytics = z.infer<typeof analyticsSchema>;
export type Provider = 'ga4' | 'plausible';

const testSchema = z.object({ ok: z.boolean(), message: z.string() }) satisfies z.ZodType<Generated<'HelpCenterAnalyticsTest'>>;
const sendTest = (helpCenterId?: string) => request(`/api/v1/help-center/analytics/test${onHelpCenter(helpCenterId)}`, testSchema, { method: 'POST', body: '{}' });

// AnalyticsStatus is the status line: off, or the last page view the service accepted, or the last error when it
// is newer.
export function AnalyticsStatus({ settings, timezone }: { settings: Analytics; timezone: string }) {
  if (!settings.provider || !settings.enabled) return <p className="muted-copy">{text.off}</p>;
  const { last_sent_at: sent, last_error: error, last_error_at: errorAt } = settings;
  if (error && errorAt && (!sent || Date.parse(errorAt) > Date.parse(sent))) {
    return <p className="error-copy">{text.lastError(formatDate(errorAt, timezone), error)}</p>;
  }
  return <p className="muted-copy">{sent ? text.lastSent(formatDate(sent, timezone)) : text.noneYet}</p>;
}

// WhatIsSent states what the chosen service receives for each page view, so the team can judge its consent needs.
export function WhatIsSent({ provider }: { provider: Provider }) {
  return <div role="note" aria-label={text.whatIsSent} className="notice notice-plain">
    <span><strong>{text.whatIsSent}: </strong>{provider === 'ga4' ? text.sentGa4 : text.sentPlausible} {text.consent}</span>
  </div>;
}

// TestEvent sends one test page view to the saved service and shows its answer.
export function TestEvent() {
  const target = useHelpCenterTarget();
  const test = useMutation({ mutationFn: () => sendTest(target) });
  const failed = test.isError ? (test.error instanceof InvalidRequestError ? test.error.message : messages.settingsActionFailed) : undefined;
  return <>
    <span className="button-row"><button type="button" className="secondary-action" disabled={test.isPending} onClick={() => test.mutate()}>
      {test.isPending ? text.testing : text.test}</button></span>
    {test.data && (test.data.ok
      ? <p role="status" className="saved-state" data-on="true">{test.data.message}</p>
      : <p role="alert" className="error-copy">{test.data.message}</p>)}
    {failed && <p role="alert" className="error-copy">{failed}</p>}
  </>;
}
