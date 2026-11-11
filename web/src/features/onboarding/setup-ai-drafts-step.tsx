import { useId } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { aiApi } from '../../api/ai';
import type { AISettings, AISlotInput } from '../../api/ai';
import { ClockIcon, ExternalLinkIcon, MailIcon, SparklesIcon } from '../../ui/icons';
import { FlowActions } from './setup-flow-actions';
import type { FlowMoves } from './setup-flow-actions';
import { aiDraftsMessages as m } from './setup-ai-and-targets-messages';
import './setup-ai-and-targets.css';

// Where turning drafts on sends them: the server's built-in AI when it offers drafts, else the workspace's
// first provider. Undefined: neither, so nothing can be turned on here.
function draftSource(settings: AISettings): AISlotInput | undefined {
  if ((settings.server?.models.draft ?? []).length > 0) return { source: 'server', enabled: true, default_level: 'suggest' };
  const provider = settings.providers[0];
  return provider ? { source: 'provider', provider_id: provider.id, enabled: true, default_level: 'suggest' } : undefined;
}

// recipient names where the data goes, in the words of "What is sent".
function recipient(settings?: AISettings): string {
  if (settings?.server) return settings.server.kind === 'anthropic' ? m.runsClaude(settings.server.label) : settings.server.label;
  return settings?.providers[0]?.label ?? m.yourProvider;
}

// costText is "What it costs" from the server's estimate, as Settings → Built-in AI computes it: half of the
// month's included AI divided by a draft's cost, and the automatic drafts a day.
function costText(settings: AISettings): string {
  const perDay = settings.automatic.draft_per_day;
  if (settings.server) {
    const each = settings.auto_draft_estimate.draft_cost_micro_usd;
    if (!settings.allowance) return m.costServer(perDay);
    const half = Math.floor(settings.allowance.limit_micro_usd / 2);
    return m.costIncluded(each ? Math.floor(half / each) : null, perDay);
  }
  return settings.providers.length > 0 ? m.costOwn(perDay) : m.costNone;
}

// Example is one conversation of Today with its draft ready: only a picture of what the team will see, so
// none of it can be pressed.
function Example({ thin }: { thin: boolean }) {
  const e = m.example;
  const labelID = useId();
  return <figure className="setup-example" aria-labelledby={labelID}>
    <figcaption id={labelID} className="setup-example-label">{m.exampleLabel}</figcaption>
    <div className="setup-example-card">
      <div className="setup-example-row">
        <span className="setup-example-waiting">{e.waiting}</span>
        <span className="setup-example-priority" data-priority="urgent">{e.priority}</span>
        <strong>{e.customer}</strong>
        <span className="setup-example-subject">{e.subject}</span>
        <MailIcon /><span className="setup-example-avatar" aria-hidden="true">{e.initials}</span>
      </div>
      <div className="setup-example-body">
        <p>{e.message}</p>
        <p className="setup-example-meta"><ClockIcon />{e.meta.join(' · ')}</p>
        <div className="setup-example-draft">
          <p><SparklesIcon /><b>{e.draftReady}</b> <span>{thin ? e.sourcesThin : e.sources}</span></p>
          <p>{thin ? e.draftThin : e.draft}</p>
        </div>
        <div className="setup-example-actions">
          <span className="setup-example-button">{e.review}</span><span className="setup-example-button">{e.open}</span>
          <span className="setup-example-note">{m.exampleNote}</span>
        </div>
      </div>
    </div>
  </figure>;
}

// AIDraftsStep turns reply drafts on the way Settings → Built-in AI does (the same call, so the consent is
// recorded the same way): Reply drafts on, Suggest by default for channels. Triage stays as it is.
export function AIDraftsStep({ state, workspace, moves }: { state: string; workspace: string; moves: FlowMoves }) {
  const client = useQueryClient();
  const settings = useQuery({ queryKey: ['ai-settings'], queryFn: aiApi.settings });
  const refresh = () => Promise.all(['setup', 'ai-settings'].map((key) => client.invalidateQueries({ queryKey: [key] })));
  const turnOn = useMutation({ mutationFn: (input: AISlotInput) => aiApi.setSlot('draft', input), onSuccess: refresh });
  const draft = settings.data?.slots.find((item) => item.slot === 'draft');
  const turnOff = useMutation({
    mutationFn: () => aiApi.setSlot('draft', draft?.source === 'provider' && draft.provider_id
      ? { source: 'provider', provider_id: draft.provider_id, enabled: false } : { source: draft?.source ?? 'server', enabled: false }),
    onSuccess: refresh,
  });
  const sentID = useId();
  const on = state === 'on';
  const unavailable = state === 'unavailable';
  const source = settings.data ? draftSource(settings.data) : undefined;
  const to = recipient(settings.data);
  const short = settings.data?.server?.label ?? settings.data?.providers[0]?.label ?? m.yourProvider;
  const primary = on
    ? <button type="button" className="primary-action" onClick={moves.next}>{moves.nextLabel}</button>
    : unavailable
      ? <a className="button primary-action" href="/settings/ai" target="_blank" rel="noopener noreferrer">{m.addProvider} <ExternalLinkIcon /><span className="sr-only">{m.newTab}</span></a>
      : <button type="button" className="primary-action" disabled={!source || turnOn.isPending} onClick={() => source && turnOn.mutate(source)}>{m.turnOn}</button>;
  return <>
    <div className="setup-flow-body setup-steps-body">
      <Example thin={state === 'nothing_to_read'} />
      {state === 'nothing_to_read' && <section className="setup-thin" role="note" aria-label={m.thinTitle}>
        <b>{m.thinTitle}</b><p>{m.thinText(workspace)}</p>
        <p className="setup-thin-links"><a href="/setup/import">{m.thinImport}</a><a href="/knowledge/new">{m.thinArticle}</a></p>
      </section>}
      <ol className="setup-how" aria-label={m.howLabel}>
        {m.how.map((row, index) => <li key={row.title}><span>{index + 1}</span><div><b>{row.title}</b><p>{row.text}</p></div></li>)}
      </ol>
      {on && <p className="setup-done" role="status"><span className="setup-done-mark">{m.on}</span>{m.onText}
        <button type="button" className="link-action" disabled={turnOff.isPending} onClick={() => turnOff.mutate()}>{m.turnOff}</button></p>}
      {(turnOn.isError || turnOff.isError) && <p role="alert" className="error-copy">{m.failed}</p>}
      <dl className="setup-facts">
        <div id={sentID}><dt>{m.sentTitle}</dt><dd>{m.sent(unavailable ? m.yourProvider : to)}{unavailable ? '' : on ? m.sentRecorded : m.sentConsent}</dd></div>
        {settings.data && <div><dt>{m.costTitle}</dt><dd>{costText(settings.data)}</dd></div>}
      </dl>
    </div>
    <FlowActions back={moves.back} later={on ? undefined : moves.later} primary={primary} pinned
      note={on || unavailable ? undefined : <>{m.consentLine(short)} <a href={`#${sentID}`}>{m.whatIsSent}</a></>} />
  </>;
}
