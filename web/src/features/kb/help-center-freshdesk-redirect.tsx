import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { z } from 'zod';
import { request } from '../../api/client';
import type { Generated } from '../../api/schemas';
import { freshdeskRedirectMessages as text } from './help-center-freshdesk-redirect-messages';

const schema = z.object({
  state: z.enum(['no_import', 'own_domain', 'help_center_off', 'ask_plan', 'growth', 'ready']),
  freshdesk_domain: z.string().nullable(), address: z.string(), keep_weeks: z.number().int(), snippet: z.string().nullable(),
  mapped: z.object({ articles: z.number().int(), folders: z.number().int(), categories: z.number().int(), embedded: z.boolean() }).nullable(),
}) satisfies z.ZodType<Generated<'FreshdeskRedirect'>>;

const plans = [{ value: 'growth', label: text.growth }, { value: 'pro', label: text.pro }, { value: 'enterprise', label: text.enterprise }] as const;
type Plan = typeof plans[number]['value'];

// HelpCenterFreshdeskRedirectSection helps a team that moved from <name>.freshdesk.com send its old
// addresses to the new help center: it asks for the Freshdesk plan, then shows what the plan allows.
export function HelpCenterFreshdeskRedirectSection() {
  const [plan, setPlan] = useState<Plan>();
  const current = useQuery({
    queryKey: ['help-center-freshdesk-redirect', plan ?? null],
    queryFn: () => request(`/api/v1/help-center/freshdesk-redirect${plan ? `?plan=${plan}` : ''}`, schema),
    placeholderData: (previous) => previous,
  });
  if (current.isError) return <p role="alert" className="error-copy">{text.loadFailed}</p>;
  const redirect = current.data;
  if (!redirect || redirect.state === 'no_import' || !redirect.freshdesk_domain) return null;
  const domain = redirect.freshdesk_domain;
  return <section className="stacked-form" aria-labelledby="help-center-freshdesk-redirect-heading">
    <h3 id="help-center-freshdesk-redirect-heading">{text.heading}</h3>
    <p className="muted-copy">{text.intro(domain)}</p>
    {redirect.state === 'own_domain' && <p>{text.ownDomain}</p>}
    {redirect.state === 'help_center_off' && <p>{text.helpCenterOff}</p>}
    {(redirect.state === 'ask_plan' || redirect.state === 'growth' || redirect.state === 'ready') && <>
      <fieldset className="field"><legend>{text.planQuestion}</legend>
        {plans.map((item) => <label key={item.value} className="field-inline">
          <input type="radio" name="freshdesk-plan" value={item.value} checked={plan === item.value} onChange={() => setPlan(item.value)} /><span>{item.label}</span></label>)}
        <small className="muted-copy">{text.planHelp}</small></fieldset>
      {redirect.state === 'growth' && <p>{text.growthAnswer(domain)}</p>}
      {redirect.state === 'ready' && redirect.snippet && <Snippet snippet={redirect.snippet} weeks={redirect.keep_weeks} mapped={redirect.mapped} />}
    </>}
  </section>;
}

function Snippet({ snippet, weeks, mapped }: { snippet: string; weeks: number; mapped: z.infer<typeof schema>['mapped'] }) {
  const [copied, setCopied] = useState(false);
  return <div className="copy-field">
    <label className="invite-link"><span className="field-label">{text.snippetLabel}</span>
      <textarea readOnly rows={8} spellCheck={false} value={snippet} onFocus={(event) => event.currentTarget.select()} /></label>
    <span className="button-row">
      <button type="button" onClick={() => void navigator.clipboard.writeText(snippet).then(() => setCopied(true))}>{text.copy}</button>
      {copied && <span role="status">{text.copied}</span>}
    </span>
    <p className="muted-copy">{text.steps}</p>
    <p className="muted-copy">{text.javascript}</p>
    <p className="muted-copy">{text.keep(weeks)}</p>
    {mapped && <p className="muted-copy">{mapped.embedded ? text.mapped(mapped.articles, mapped.folders, mapped.categories) : text.notMapped}</p>}
  </div>;
}
