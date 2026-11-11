import { useId } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { aiApi } from '../../api/ai';
import type { AISettings, AISlotInput } from '../../api/ai';
import { messages } from '../../messages';
import { ClockIcon, ExternalLinkIcon, MailIcon, SparklesIcon } from '../../ui/icons';
import { slotInput } from '../settings/ai-slot-input';
import { failureReason } from './failure-reason';
import { FlowActions } from './setup-flow-actions';
import type { FlowMoves } from './setup-flow-actions';
import { aiDraftsMessages as m } from './setup-ai-and-targets-messages';
import './setup-ai-and-targets.css';

// draftInput is where turning drafts on sends them: the source the draft feature already has, kept as Settings
// keeps it (provider, model, effort); without one, the server's built-in AI when it offers drafts, else the
// workspace's first provider. Undefined: none of these, so nothing can be turned on here.
function draftInput(settings: AISettings): AISlotInput | undefined {
  const slot = settings.slots.find((item) => item.slot === 'draft');
  if (slot && slot.source !== 'none') return slotInput(slot);
  if ((settings.server?.models.draft ?? []).length > 0) return { source: 'server', enabled: true };
  const provider = settings.providers[0];
  return provider ? { source: 'provider', provider_id: provider.id, enabled: true } : undefined;
}

// Who receives the drafts' data, named in "What is sent" (to, with the models it runs) and in the consent line
// (short). Both follow the source that will be sent, never only whether the server has built-in AI.
function recipient(settings: AISettings, input?: AISlotInput): { to: string; short: string } {
  if (input?.source === 'server') {
    const label = settings.server?.label ?? m.builtIn;
    return { to: settings.server?.kind === 'anthropic' ? m.runsClaude(label) : label, short: label };
  }
  const provider = input?.source === 'provider' ? settings.providers.find((item) => item.id === input.provider_id) : undefined;
  const label = provider?.label ?? m.yourProvider;
  return { to: label, short: label };
}

// costText is "What it costs" for that source: the server's estimate as Settings → Built-in AI computes it (half
// of the month's included AI divided by a draft's cost, and the automatic drafts a day); a workspace provider
// bills the workspace.
function costText(settings: AISettings, input?: AISlotInput): string {
  const perDay = settings.automatic.draft_per_day;
  if (input?.source === 'server') {
    const each = settings.auto_draft_estimate.draft_cost_micro_usd;
    if (!settings.allowance) return m.costServer(perDay);
    const half = Math.floor(settings.allowance.limit_micro_usd / 2);
    return m.costIncluded(each ? Math.floor(half / each) : null, perDay);
  }
  return input?.source === 'provider' ? m.costOwn(perDay) : m.costNone;
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
  // Turning on and off are Settings' own calls: the slot as it is, at Suggest or back to Off (drafts on request).
  const setLevel = useMutation({ mutationFn: (input: AISlotInput) => aiApi.setSlot('draft', input), onSuccess: refresh });
  const draft = settings.data?.slots.find((item) => item.slot === 'draft');
  const sentID = useId();
  const on = state === 'on';
  const unavailable = state === 'unavailable';
  const source = settings.data ? draftInput(settings.data) : undefined;
  const { to, short } = settings.data ? recipient(settings.data, source) : { to: m.yourProvider, short: m.yourProvider };
  const turnOn = () => source && setLevel.mutate({ ...source, enabled: true, default_level: 'suggest' });
  const turnOff = () => draft && setLevel.mutate({ ...slotInput(draft), default_level: 'off' });
  const primary = on
    ? <button type="button" className="primary-action" onClick={moves.next}>{moves.nextLabel}</button>
    : unavailable
      ? <a className="button primary-action" href="/settings/ai" target="_blank" rel="noopener noreferrer">{m.addProvider} <ExternalLinkIcon /><span className="sr-only">{m.newTab}</span></a>
      : <button type="button" className="primary-action" disabled={!source || setLevel.isPending} onClick={turnOn}>{m.turnOn}</button>;
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
        <button type="button" className="link-action" disabled={!draft || setLevel.isPending} onClick={turnOff}>{m.turnOff}</button></p>}
      {setLevel.isError && <p role="alert" className="error-copy">{on ? m.offFailed : m.failed}</p>}
      {settings.isError && !settings.data && <p role="alert" className="error-copy">{m.settingsFailed(failureReason(settings.error))}{' '}
        <button type="button" className="link-action" onClick={() => void settings.refetch()}>{messages.tryAgain}</button></p>}
      <dl className="setup-facts">
        <div id={sentID}><dt>{m.sentTitle}</dt><dd>{m.sent(unavailable ? m.yourProvider : to)}{unavailable ? '' : on ? m.sentRecorded : m.sentConsent}</dd></div>
        {settings.data && <div><dt>{m.costTitle}</dt><dd>{costText(settings.data, source)}</dd></div>}
      </dl>
    </div>
    <FlowActions back={moves.back} later={on ? undefined : moves.later} primary={primary} pinned
      note={on || unavailable ? undefined : <>{m.consentLine(short)} <a href={`#${sentID}`}>{m.whatIsSent}</a></>} />
  </>;
}
