import { useEffect, useState } from 'react';
import type { ReactNode } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '../../api/client';
import { channelsApi } from '../../api/channels';
import type { EmailChannelSettings } from '../../api/schemas';
import type { SetupProps } from '../registry';
import { AddressForm } from './address-form';
import { TestMessage } from './email-shared';
import { ForwardStep, RepliesStep, useRefetchOnActivity } from './setup-steps';
import { setupMessages as m } from './setup-messages';
import { markSetupReturn } from '../setup-return';
import { mailboxReturn } from '../../features/settings/gmail-return';
import { addressSaved, clockTime, nextStep, readMemory, repliesThrough, stepStates, steps, testReplied, writeMemory } from './setup-state';
import type { SetupMemory, StepID, StepState } from './setup-state';
import { settingsMessages } from '../../features/settings/settings-messages';
import './email-setup.css';

// EmailSetup connects a support address in four steps on one page: the address, forwarding its mail here,
// how replies leave, and a test message. Each step's state is read from the server; a step opens again
// after it is done, and leaving the page loses nothing.
export function EmailSetup({ channel, named = false, canManage, timezone, navigate, create, returnTo }: SetupProps) {
  const client = useQueryClient();
  const id = channel?.id;
  const settings = useQuery({ queryKey: ['email-channel', id], queryFn: () => channelsApi.email(id!), enabled: Boolean(id) });
  const [memory, setMemory] = useState<SetupMemory>(() => readMemory(id));
  const remember = (change: Partial<SetupMemory>) => setMemory((current) => {
    const next = { ...current, ...change };
    if (id) writeMemory(id, next);
    return next;
  });
  const conversation = memory.testConversation;
  const timeline = useQuery({ queryKey: ['messages', conversation], queryFn: () => api.messages(conversation!), enabled: Boolean(conversation) });
  const data = settings.data;
  const states = stepStates({ channel, settings: data, memory, replied: testReplied(timeline.data?.items) });
  const ready = !id || Boolean(data);
  // Back from a mailbox provider's sign-in (Google, Microsoft), the address carries the outcome: it is read
  // once, and the replies step opens to say what happened (connected, or why not and what to do).
  const [returned] = useState(mailboxReturn);
  // The step a person is on stays open until they move on, even when its state changes under them.
  const [chosen, setChosen] = useState<StepID | null>();
  useEffect(() => {
    if (ready && chosen === undefined) setChosen(id ? (returned ? 'replies' : nextStep(states) ?? null) : 'address');
  }, [ready, chosen, id, states, returned]);
  const open = chosen === undefined ? (id ? undefined : 'address') : chosen;
  const advance = (from: StepID) => setChosen(nextStep(states, from) ?? steps.slice(steps.indexOf(from) + 1).find((step) => states[step] !== 'done') ?? null);
  const skip = (step: StepID) => {
    remember({ skipped: [...new Set([...(memory.skipped ?? []), step])] });
    advance(step);
  };
  useRefetchOnActivity(id ?? '', Boolean(id) && open === 'forward' && states.forward !== 'done');
  const refresh = () => Promise.all(['email-channel', 'channel', 'channels'].map((key) => client.invalidateQueries({ queryKey: key === 'channel' ? [key, id] : [key] })));
  const address = data?.channel?.support_address;
  const finished = steps.every((step) => states[step] === 'done');
  const body: Record<StepID, () => ReactNode> = {
    address: () => <AddressForm canManage={canManage} named={named} initial={addressSaved(data) ? address : ''}
      save={({ name, address: value }) => (id ? channelsApi.update(id, { config: { support_address: value } }).then(refresh)
        : create({ ...(name ? { name } : {}), config: { support_address: value } }))}
      onSaved={() => { if (id) setChosen('forward'); }} />,
    forward: () => channel && data && <ForwardStep channel={channel} settings={data} canManage={canManage} timezone={timezone} provider={memory.provider}
      onProvider={(provider) => remember({ provider })} onContinue={() => advance('forward')} onSkip={() => skip('forward')} />,
    replies: () => id && data && <RepliesStep channelID={id} settings={data} canManage={canManage} timezone={timezone} provider={memory.provider ?? (data.hosted_address ? 'gmail' : 'other')} done={states.replies === 'done'}
      returned={returned} onContinue={() => advance('replies')} onSkip={() => skip('replies')} beforeLeave={() => markSetupReturn(id, returnTo)} />,
    test: () => id && data && <div className="setup-body">
      <TestMessage channelID={id} ready={addressSaved(data)} forwarding={states.forward === 'done'} navigate={navigate} onSent={(sent) => remember({ testConversation: sent })} />
      {conversation && (states.test === 'done'
        ? <p className="setup-live setup-live-ok">{m.testDelivered}</p>
        : <p className="muted-copy">{m.testWaiting} <a href={`/conversations/${encodeURIComponent(conversation)}`}>{m.testOpen}</a></p>)}
    </div>,
  };
  const summary: Record<StepID, () => ReactNode> = {
    address: () => <span className="setup-value">{address}</span>,
    forward: () => <span className="setup-pill">{channel?.last_inbound_at ? m.firstEmail(clockTime(channel.last_inbound_at, timezone)) : m.firstEmail('')}</span>,
    replies: () => <span className="muted-copy">{repliesSummary(data)}</span>,
    test: () => <span className="setup-pill">{m.testDelivered}</span>,
  };
  const titles: Record<StepID, string> = { address: m.address, forward: m.forward, replies: m.replies(addressSaved(data) ? address : undefined), test: m.test };
  return <div className="setup-layout">
    <section className="settings-card setup-card" aria-labelledby="setup-heading">
      <div className="section-heading">
        <h2 id="setup-heading">{m.title}</h2><p>{m.intro}</p>
        {!canManage && <p>{m.adminsOnly}</p>}
        {id && <p><a href={`/settings/channels/${encodeURIComponent(id)}`}>{m.manage}</a></p>}
      </div>
      {finished && <p role="status" aria-label={m.finished} className="notice notice-plain"><span><strong>{m.finished}.</strong> {m.finishedBody} <a href="/">{m.openInbox}</a></span></p>}
      {!ready || open === undefined ? <div className="ch-loading" aria-busy="true" /> : <ol className="setup-steps">
        {steps.map((step, index) => <Step key={step} step={step} n={index + 1} title={titles[step]} state={states[step]} open={open === step}
          later={open !== step && states[step] === 'todo'} summary={summary[step]} body={body[step]} onOpen={canManage && id ? () => setChosen(step) : undefined} />)}
      </ol>}
    </section>
    <CustomersSee settings={data} />
  </div>;
}

function Step({ step, n, title, state, open, later, summary, body, onOpen }: { step: StepID; n: number; title: string; state: StepState; open: boolean; later: boolean;
  summary: () => ReactNode; body: () => ReactNode; onOpen?: () => void }) {
  const heading = `setup-step-${step}`;
  return <li role="group" aria-labelledby={heading} aria-disabled={later || undefined} className={`setup-step setup-${state}${open ? ' setup-open' : ''}`}>
    <div className="setup-step-head">
      <span className="setup-num" aria-hidden="true">{state === 'done' && !open ? '✓' : n}</span>
      <h3 id={heading}>{title}</h3>
      {!open && state === 'done' && summary()}
      {!open && state === 'skipped' && <span className="setup-pill setup-pill-muted">{m.skipped}</span>}
      {open && <span className="setup-of">{m.stepOf(n)}</span>}
      {!open && state !== 'todo' && onOpen && <button type="button" className="link-action setup-change" onClick={onOpen}>{state === 'done' ? m.change : m.setUp}</button>}
    </div>
    {open && body()}
  </li>;
}

function repliesSummary(settings: EmailChannelSettings | undefined): string {
  const through = repliesThrough(settings);
  if (through === 'gmail' || through === 'microsoft') return m.repliesDone[through];
  return settings?.sending_from ? settingsMessages.sendingFrom(settings.sending_from) : '';
}

// CustomersSee is what a customer's mail shows, from the live values: the address they write to and the
// From line of a reply.
function CustomersSee({ settings }: { settings: EmailChannelSettings | undefined }) {
  const channel = settings?.channel;
  const connection = settings?.gmail.connection ?? settings?.microsoft.connection;
  const from = connection?.status === 'connected' ? connection.address : settings?.sending_from;
  return <aside className="setup-preview" aria-label={m.customersSee}>
    <h3>{m.customersSee}</h3>
    <p className="setup-preview-lines">
      <span>{m.to} {addressSaved(settings) ? channel?.support_address : '…'}</span>
      <span>{m.repliesFrom} {from ? `${channel?.sender_name ?? ''} <${from}>`.trim() : m.notEmailed}</span>
    </p>
    <p className="muted-copy">{m.nothingChanges}</p>
  </aside>;
}
