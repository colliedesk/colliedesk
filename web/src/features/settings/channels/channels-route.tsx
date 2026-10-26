import { useEffect, useId, useRef, useState } from 'react';
import type { MouseEvent, ReactNode } from 'react';
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, InvalidRequestError } from '../../../api/client';
import { channelsApi } from '../../../api/channels';
import type { Channel, ChannelDetail, ChannelList, ChannelType, ChannelTypeInfo } from '../../../api/channels';
import { channelTypes, channelUI, upcomingChannels } from '../../../channels/registry';
import type { ChannelUI, ConnectProps } from '../../../channels/registry';
import { formatDate } from '../../../lib/format-date';
import { messages } from '../../../messages';
import { Dialog } from '../../../ui/dialog';
import { ChatIcon, PlusIcon } from '../../../ui/icons';
import { Part, PartUnavailable } from '../../../ui/lazy-part';
import { panelID, tabID, Tabs } from '../../../ui/tabs';
import { NameForm } from '../name-form';
import { saveState } from '../save-feedback';
import { SettingsNav } from '../settings-nav';
import type { SettingsPath, Shown } from '../settings-nav';
import { ChannelAITab } from './channel-ai-tab';
import { channelsMessages as m } from './channels-messages';
import './channels.css';
import { useCreateAction } from '../../../api/use-create';
import { takeSetupReturn } from '../../../channels/setup-return';
import { useBillingShown } from '../../billing/billing-shown';

// The pages under /settings/channels. Everything a type adds comes from the web registry
// (src/channels/registry.ts); nothing here names a type.
//
//   /settings/channels                 the list (?tab=history for the history of every channel)
//   /settings/channels/<id>?tab=…      one channel: its type's tabs, then History
//   /settings/channels/<id>/setup      the type's guided setup of that channel, when the type has one
//   /settings/channels/new/<type>      the type's setup or connect form; the channel exists once it is saved
//   /settings/channels/type/<type>     the type's home when it has one; otherwise, and always with a tab or a
//                                      mailbox outcome in the address, the workspace's channel of that type

export interface ChannelsSearch { tab?: string; gmail?: string; gmail_error?: string; microsoft?: string; microsoft_error?: string }

export interface ChannelsRouteProps {
  // path is what follows /settings/channels/ ('' for the list).
  path: string;
  search: ChannelsSearch;
  // navigate follows an address inside the app; replace keeps the address out of the history.
  navigate: (href: string, replace?: boolean) => void;
}

const base = '/settings/channels';
// A channel that does not exist (removed, mistyped, another workspace's) answers 404, as everywhere in the app.
const isMissing = (error: unknown) => error instanceof Error && error.message === 'API 404';
const withSearch = (path: string, search: ChannelsSearch) => {
  const query = new URLSearchParams(Object.entries(search).filter((entry): entry is [string, string] => typeof entry[1] === 'string' && entry[1] !== '')).toString();
  return `${path}${query ? `?${query}` : ''}`;
};

export function ChannelsRoute({ path, search, navigate }: ChannelsRouteProps) {
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const config = useQuery({ queryKey: ['public-config'], queryFn: api.config });
  const isAdmin = me.data?.agent.role === 'admin';
  const billingShown = useBillingShown(isAdmin);
  const timezone = me.data?.timezone ?? 'UTC';
  const shown: Shown = { general: isAdmin, team: isAdmin, channels: true, savedReplies: true, exportData: isAdmin,
    billing: billingShown, importData: isAdmin, ai: true };
  const [first, second] = path.split('/');
  const page = { navigate, isAdmin, timezone };
  // The type a page belongs to names it and marks its place in the nav; a channel's type is read from the
  // list the pages load anyway.
  const onChannel = Boolean(first) && first !== 'new' && first !== 'type';
  const listed = useQuery({ queryKey: ['channels'], queryFn: channelsApi.list, enabled: onChannel });
  const type = first === 'new' || first === 'type' ? second : listed.data?.items.find((item) => item.id === first)?.type;
  const typeHome = first === 'new' || first === 'type' || second === 'setup' ? channelUI(type ?? '') : undefined;
  let content: ReactNode;
  if (!me.data) content = <div className="ch-loading" aria-busy="true" />;
  else if (path === '') content = <ChannelsList {...page} tab={search.tab === 'history' ? 'history' : 'channels'} />;
  else if (first === 'new') content = <NewChannel {...page} type={second ?? ''} />;
  else if (first === 'type') content = carriesChannelState(search) ? <TypeShortcut {...page} type={second ?? ''} search={search} /> : <TypeHome {...page} type={second ?? ''} search={search} />;
  else if (second === 'setup') content = <ChannelSetup {...page} id={first} />;
  else content = <ChannelPage {...page} id={first} tab={search.tab} search={search} />;
  return (
    <main className="settings-page panel channels-settings">
      <header><h1>{typeHome?.label ?? m.heading}</h1><p>{typeHome?.description ?? m.description}</p></header>
      <div className="settings-layout">
        <SettingsNav page={navPage(path === '' ? undefined : type)} shown={shown} />
        <div className="settings-sections ch-page">{content}</div>
      </div>
    </main>
  );
}

interface PageProps { navigate: ChannelsRouteProps['navigate']; isAdmin: boolean; timezone: string }

// navPage is the nav item of a page: its type's home, or Channels.
const navPage = (type: string | undefined): SettingsPath => (type && channelUI(type) ? `${base}/type/${type}` as SettingsPath : base);

// A tab or a mailbox provider's outcome in the address is about one channel (a link from the inbox, the
// provider's redirect): those open that channel, never the type's home.
const carriesChannelState = (search: ChannelsSearch) => Boolean(search.tab || search.gmail || search.gmail_error || search.microsoft || search.microsoft_error);

function useChannels() {
  return useQuery({ queryKey: ['channels'], queryFn: channelsApi.list });
}

function LoadFailed({ onRetry }: { onRetry: () => void }) {
  return <p role="alert" className="error-copy">{m.loadFailed} <button type="button" className="link-action" onClick={onRetry}>{m.tryAgain}</button></p>;
}

const stateLabel: Record<Channel['status']['state'], string> = {
  working: m.statusWorking, waiting: m.statusWaiting, off: m.statusOff, needs_attention: m.statusNeedsAttention, not_connected: m.statusNotConnected, removed: m.statusRemoved,
};
const stateClass: Record<Channel['status']['state'], string> = { working: 'ch-st-ok', waiting: 'ch-st-off', off: 'ch-st-off', needs_attention: 'ch-st-warn', not_connected: 'ch-st-off', removed: 'ch-st-off' };

function StatePill({ channel }: { channel: Channel }) {
  return <span className={`status ${stateClass[channel.status.state]}`}>{stateLabel[channel.status.state]}</span>;
}

// The types this server runs, in the registry's order, only those the web knows. A workspace may add
// several channels of every type.
function addable(list: ChannelList | undefined): { ui: ChannelUI; info: ChannelTypeInfo }[] {
  return channelTypes.flatMap((ui) => {
    const info = list?.types.find((type) => type.type === ui.type);
    return info?.available ? [{ ui, info }] : [];
  });
}

// typeCount is how many live channels of the channel's type the workspace has: past one, names and
// the Default mark tell them apart.
const typeCount = (list: ChannelList | undefined, type: string) => list?.types.find((info) => info.type === type)?.count ?? 0;

// TypePicker lists the types to connect: links on the empty page, choices in Add channel.
function TypePicker({ list, choose }: { list: ChannelList | undefined; choose?: (href: string) => void }) {
  return <ul className="ch-types">
    {addable(list).map(({ ui }) => {
      const href = `${base}/new/${ui.type}`;
      return <li key={ui.type}>
        {choose
          ? <button type="button" className="ch-type" onClick={() => choose(href)}><ui.Icon /><span><strong>{ui.connectLabel}</strong><small>{ui.description}</small></span></button>
          : <a href={href} className="ch-type"><ui.Icon /><span><strong>{ui.connectLabel}</strong><small>{ui.description}</small></span></a>}
      </li>;
    })}
    {upcomingChannels.map((upcoming) => <li key={upcoming.label}>
      <span className="ch-type" aria-disabled="true"><span><strong>{upcoming.label}</strong>
        <small>{m.notAvailableYet} · <a href={`https://github.com/colliedesk/colliedesk/issues/${upcoming.issue}`} target="_blank" rel="noreferrer">{m.trackedIn(upcoming.issue)}</a></small></span></span>
    </li>)}
  </ul>;
}

// The name of the channel just removed, for the list it goes back to: it says so there and takes the
// focus the removed page's buttons had. Read once.
let justRemoved: string | undefined;
const takeRemoved = () => { const name = justRemoved; justRemoved = undefined; return name; };

function ChannelsList({ navigate, isAdmin, timezone, tab }: PageProps & { tab: 'channels' | 'history' }) {
  const channels = useChannels();
  const [removedName] = useState(takeRemoved);
  const removedNote = useRef<HTMLParagraphElement>(null);
  const listed = Boolean(channels.data);
  useEffect(() => { if (removedName && listed) removedNote.current?.focus(); }, [removedName, listed]);
  const [adding, setAdding] = useState(false);
  const prefix = useId();
  const list = channels.data;
  const show = (next: 'channels' | 'history') => navigate(withSearch(base, next === 'history' ? { tab: 'history' } : {}), true);
  const attention = (list?.items ?? []).filter((channel) => channel.status.state === 'needs_attention');
  return <>
    <div className="ch-tabs-row">
      <Tabs label={m.tabs} prefix={prefix} value={tab} onChange={show} tabs={[{ id: 'channels', label: m.tabChannels, count: list?.items.length }, { id: 'history', label: m.tabHistory }]} />
      {isAdmin && list && list.items.length > 0 && <button type="button" className="ch-add" onClick={() => setAdding(true)}><PlusIcon /><span>{m.addChannel}</span></button>}
    </div>
    <div role="tabpanel" id={panelID(prefix, tab)} aria-labelledby={tabID(prefix, tab)} className="ch-stack">
      {tab === 'history' ? <History timezone={timezone} />
        : channels.isError ? <LoadFailed onRetry={() => void channels.refetch()} />
          : !list ? <div className="ch-loading" aria-busy="true"><span className="sr-only">{m.loading}</span></div>
            : list.items.length === 0
              ? <>{removedName && <p className="notice notice-plain" role="status" tabIndex={-1} ref={removedNote}><span>{m.removed(removedName)}</span></p>}
                  <section className="settings-card ch-empty" aria-labelledby="ch-empty-heading">
                  <div className="section-heading"><h2 id="ch-empty-heading">{m.emptyTitle}</h2><p>{isAdmin ? m.emptyBody : m.adminsOnly}</p></div>
                  {isAdmin && <TypePicker list={list} />}
                </section></>
              : <>
                  {removedName && <p className="notice notice-plain" role="status" tabIndex={-1} ref={removedNote}><span>{m.removed(removedName)}</span></p>}
                  {attention.length > 0 && <div className="notice" role="status"><span><strong>{m.needsAttention}: </strong>
                    {attention.map((channel) => <span key={channel.id} className="ch-reason">{channel.name}: {m.reason(channel.status.reason_code, channel.status.message)}{' '}
                      <a href={`${base}/${channel.id}`}>{m.fix}</a></span>)}</span></div>}
                  {list.items.filter((channel) => channel.enabled && channel.unsent_count > 0).map((channel) => <Unsent key={channel.id} channel={channel} />)}
                  <section className="panel ch-list" aria-label={m.tabChannels}>
                    {list.items.map((channel) => <ChannelRow key={channel.id} channel={channel} isAdmin={isAdmin}
                      markDefault={channel.is_default === true && typeCount(list, channel.type) > 1} />)}
                  </section>
                </>}
    </div>
    {adding && <AddChannel list={list} onClose={() => setAdding(false)} navigate={navigate} />}
  </>;
}

function Unsent({ channel }: { channel: Channel }) {
  return <div className="notice notice-plain" role="status"><span>{m.unsent(channel.unsent_count, channel.name)}</span>
    <span className="notice-actions"><a href={`/?delivery_failed=true&channel_id=${channel.id}`}>{m.seeThem}</a></span></div>;
}

function ChannelRow({ channel, isAdmin, markDefault }: { channel: Channel; isAdmin: boolean; markDefault: boolean }) {
  const ui = channelUI(channel.type);
  const href = `${base}/${channel.id}`;
  return <div className={`ch-row${channel.enabled ? '' : ' ch-row-off'}`}>
    <div className="ch-row-name">{ui && <ui.Icon />}<span><span className="ch-row-title"><strong>{channel.name}</strong>{markDefault && <span className="ch-default">{m.defaultLabel}</span>}</span>
      {channel.summary && <small>{channel.summary}</small>}</span></div>
    <div className="ch-row-state"><StatePill channel={channel} />
      {channel.held_count > 0 && <small>{m.held(channel.held_count, channel.name)}</small>}</div>
    <div className="ch-row-actions">
      {isAdmin && <EnabledSwitch channel={channel} />}
      <a href={href} className="button" aria-label={m.configureName(channel.name)}>{m.configure}</a>
    </div>
  </div>;
}

// EnabledSwitch turns a channel on at once, and off after a question that says what stops.
function EnabledSwitch({ channel }: { channel: Channel }) {
  const client = useQueryClient();
  const [asking, setAsking] = useState(false);
  const toggle = useMutation({
    mutationFn: (enabled: boolean) => channelsApi.update(channel.id, { enabled }),
    // The type's own settings may show the same state, so they reload too.
    onSettled: () => {
      const keys = ['channels', 'channel', 'channel-events', ...(channelUI(channel.type)?.caches ?? [])];
      return client.invalidateQueries({ predicate: (query) => keys.includes(String(query.queryKey[0])) });
    },
  });
  const on = toggle.isPending ? Boolean(toggle.variables) : channel.enabled;
  const id = useId();
  // The question's Turn off leaves focus nowhere (the switch is busy meanwhile); it comes back here.
  const input = useRef<HTMLInputElement>(null);
  const returnFocus = useRef(false);
  useEffect(() => {
    if (toggle.isPending || !returnFocus.current) return;
    returnFocus.current = false;
    input.current?.focus();
  }, [toggle.isPending]);
  return <span className="ch-switch">
    <label className="switch"><input id={id} ref={input} type="checkbox" role="switch" checked={on} aria-label={channel.name} disabled={toggle.isPending}
      onChange={(event) => { if (event.target.checked) toggle.mutate(true); else setAsking(true); }} /></label>
    <span aria-live="polite" className="sr-only">{toggle.isPending ? m.saving : toggle.isSuccess ? (channel.enabled ? m.turnedOn : m.turnedOff) : ''}</span>
    {toggle.isError && <span role="alert" className="error-copy">{m.switchFailed}</span>}
    {asking && <Dialog alert title={m.turnOffTitle(channel.name)} closeLabel={m.close} onClose={() => setAsking(false)}
      onSubmit={() => { setAsking(false); returnFocus.current = true; toggle.mutate(false); }}
      footer={<><button type="button" autoFocus onClick={() => setAsking(false)}>{m.cancel}</button><button type="submit" className="danger-action">{m.turnOff}</button></>}>
      <p>{m.turnOffBody}</p>
    </Dialog>}
  </span>;
}

function AddChannel({ list, onClose, navigate }: { list: ChannelList | undefined; onClose: () => void; navigate: PageProps['navigate'] }) {
  return <Dialog title={m.addChannel} help={m.addChannelHelp} closeLabel={m.close} onClose={onClose}
    footer={<button type="button" onClick={onClose}>{m.cancel}</button>}>
    <TypePicker list={list} choose={(href) => { onClose(); navigate(href); }} />
  </Dialog>;
}

// TypeShortcut opens the workspace's default channel of a type, or that type's connect form when there is none.
function TypeShortcut({ type, search, navigate }: PageProps & { type: string; search: ChannelsSearch }) {
  const channels = useChannels();
  const defaultID = channels.data?.types.find((info) => info.type === type)?.default_channel_id;
  const channel = channels.data?.items.find((item) => item.id === defaultID) ?? channels.data?.items.find((item) => item.type === type);
  const ready = Boolean(channels.data);
  useEffect(() => {
    if (!ready) return;
    navigate(channel ? withSearch(`${base}/${channel.id}`, search) : withSearch(`${base}/new/${type}`, search), true);
  }, [ready, channel, type, search, navigate]);
  if (channels.isError) return <LoadFailed onRetry={() => void channels.refetch()} />;
  return <div className="ch-loading" aria-busy="true"><span className="sr-only">{m.loading}</span></div>;
}

// useCreate adds a channel of a type and opens it: its setup when guided, else its page.
function useCreate(type: string | undefined, navigate: PageProps['navigate']) {
  const client = useQueryClient();
  const createChannel = useCreateAction((input: Parameters<typeof channelsApi.create>[0], key: string) => channelsApi.create(input, key));
  return (guided: boolean): ConnectProps['create'] => async (input) => {
    const created = await createChannel.run({ type: type as ChannelType, ...input });
    await client.invalidateQueries({ queryKey: ['channels'] });
    navigate(`${base}/${created.id}${guided ? '/setup' : ''}`, true);
    return created;
  };
}

const noChannels: ChannelList = { items: [], types: [] };

function NewChannel({ type, navigate, isAdmin, timezone }: PageProps & { type: string }) {
  const ui = channelUI(type);
  const create = useCreate(ui?.type, navigate);
  const channels = useChannels();
  if (!ui) return <p role="alert" className="error-copy">{m.unknownType}</p>;
  return <>
    <a href={base} className="ch-back">{m.backToChannels}</a>
    <Part part={ui.panels} failed={<PartUnavailable />} render={({ Connect, Setup }) => (Setup
      ? <Setup canManage={isAdmin} timezone={timezone} navigate={navigate} create={create(true)} list={channels.data ?? noChannels} named />
      : <section className="settings-card" aria-labelledby="ch-new-heading">
          <div className="section-heading"><h2 id="ch-new-heading">{ui.connectLabel}</h2>{!isAdmin && <p>{m.adminsOnly}</p>}</div>
          <Connect canManage={isAdmin} timezone={timezone} create={create(false)} />
        </section>)} />
  </>;
}

// TypeHome is a type's own page when it has one; a type without it opens its channel, as before.
function TypeHome({ type, search, navigate, isAdmin, timezone }: PageProps & { type: string; search: ChannelsSearch }) {
  const ui = channelUI(type);
  const create = useCreate(ui?.type, navigate);
  const channels = useChannels();
  const fallback = <TypeShortcut navigate={navigate} isAdmin={isAdmin} timezone={timezone} type={type} search={search} />;
  if (!ui) return fallback;
  if (channels.isError) return <LoadFailed onRetry={() => void channels.refetch()} />;
  return <Part part={ui.panels} failed={<PartUnavailable />} render={({ Home }) => (!Home ? fallback
    : channels.data ? <Home list={channels.data} canManage={isAdmin} timezone={timezone} navigate={navigate} create={create(true)} />
      : <div className="ch-loading" aria-busy="true"><span className="sr-only">{m.loading}</span></div>)} />;
}

// ChannelSetup is a channel's guided setup; a type without one shows the channel's page.
function ChannelSetup({ id, navigate, isAdmin, timezone }: PageProps & { id: string }) {
  const detail = useQuery({ queryKey: ['channel', id], queryFn: () => channelsApi.get(id), retry: (count, error) => !isMissing(error) && count < 2 });
  const channels = useChannels();
  const channel = detail.data;
  const ui = channel ? channelUI(channel.type) : undefined;
  const create = useCreate(ui?.type, navigate);
  if (isMissing(detail.error)) return <><p role="alert" className="error-copy">{m.notFound}</p><a href={base}>{m.backToChannels}</a></>;
  if (detail.isError) return <LoadFailed onRetry={() => void detail.refetch()} />;
  if (!channel || !ui) return <div className="ch-loading" aria-busy="true"><span className="sr-only">{m.loading}</span></div>;
  return <>
    <a href={`${base}/type/${ui.type}`} className="ch-back">{ui.label}</a>
    <Part part={ui.panels} failed={<PartUnavailable />} render={({ Setup }) => (Setup
      ? <Setup channel={channel} canManage={isAdmin} timezone={timezone} navigate={navigate} create={create(true)} list={channels.data ?? noChannels} />
      : <ChannelPage id={id} navigate={navigate} isAdmin={isAdmin} timezone={timezone} search={{}} />)} />
  </>;
}

function ChannelPage({ id, tab, search, navigate, isAdmin, timezone }: PageProps & { id: string; tab?: string; search: ChannelsSearch }) {
  const client = useQueryClient();
  // A mailbox sign-in started in the channel's setup comes back here (the provider's redirect): it goes on
  // in the setup, with the outcome.
  const mailboxOutcome = search.gmail || search.gmail_error || search.microsoft || search.microsoft_error;
  useEffect(() => {
    if (mailboxOutcome && takeSetupReturn(id)) navigate(withSearch(`${base}/${id}/setup`, { ...search, tab: undefined }), true);
  }, [mailboxOutcome, id, search, navigate]);
  const detail = useQuery({ queryKey: ['channel', id], queryFn: () => channelsApi.get(id), retry: (count, error) => !isMissing(error) && count < 2 });
  const [removing, setRemoving] = useState(false);
  // What the last open or discard of held messages did; it stays after the notice of held messages goes.
  const [heldDone, setHeldDone] = useState<{ kind: 'opened' | 'discarded'; count: number; skipped?: number }>();
  // The buttons that did it are gone with the notice, so focus goes to what they did.
  const heldNote = useRef<HTMLParagraphElement>(null);
  useEffect(() => { if (heldDone) heldNote.current?.focus(); }, [heldDone]);
  const prefix = useId();
  const channel = detail.data;
  const ui = channel ? channelUI(channel.type) : undefined;
  // Removing the default hands it to the oldest other live channel of the type, as the server does.
  const channels = useChannels();
  const nextDefault = channel && channels.data?.items.filter((item) => item.type === channel.type && item.id !== channel.id)
    .sort((a, b) => a.created_at.localeCompare(b.created_at) || a.id.localeCompare(b.id))[0];
  // The inbox reads the channel too (its type's settings, the conversations' channel_ref), so they reload.
  const removed = async () => {
    const keys = ['channels', 'channel-events', 'conversations', ...(ui?.caches ?? [])];
    await client.invalidateQueries({ predicate: (query) => keys.includes(String(query.queryKey[0])) });
    client.removeQueries({ queryKey: ['channel', id] });
    justRemoved = channel?.name;
    navigate(base, true);
  };
  const remove = useMutation({ mutationFn: () => channelsApi.remove(id), onSuccess: removed });
  if (isMissing(detail.error)) return <><p role="alert" className="error-copy">{m.notFound}</p><a href={base}>{m.backToChannels}</a></>;
  if (detail.isError) return <LoadFailed onRetry={() => void detail.refetch()} />;
  if (!channel) return <div className="ch-loading" aria-busy="true"><span className="sr-only">{m.loading}</span></div>;
  // A type with no screens here (a chat type before its own, a test build's stand-in) keeps the frame's
  // tabs: what AI does on it and its history.
  const tabs = [...(ui?.tabs ?? []), { id: 'ai', label: m.tabAI }, { id: 'history', label: m.tabHistory }];
  const open = tabs.find((item) => item.id === tab)?.id ?? tabs[0].id;
  const capabilities = ui ? channel.capabilities.map(ui.capabilityLabel).filter(Boolean) : [];
  return <>
    <a href={base} className="ch-back">{m.backToChannels}</a>
    <section className="settings-card ch-detail" aria-labelledby="ch-detail-heading">
      <div className="ch-detail-head">
        <div className="ch-row-name">{ui ? <ui.Icon /> : <ChatIcon />}<span><h2 id="ch-detail-heading">{channel.name}</h2>{channel.summary && <small>{channel.summary}</small>}</span></div>
        <div className="ch-row-actions"><StatePill channel={channel} />{isAdmin && <EnabledSwitch channel={channel} />}</div>
      </div>
      {channel.status.state === 'needs_attention' && <p className="notice" role="status"><span>{m.reason(channel.status.reason_code, channel.status.message)}</span></p>}
      {channel.status.state === 'waiting' && <p className="notice notice-plain" role="status"><span>{ui?.waitingHelp ?? m.waitingHelp}</span></p>}
      {ui && <ChannelIdentity channel={channel} label={ui.label} defaultHelp={ui.defaultHelp} defaultBadge={ui.defaultBadge} isAdmin={isAdmin} />}
      {capabilities.length > 0 && <p className="muted-copy ch-carries">{m.capabilities}: {capabilities.join(' · ')}</p>}
      {channel.held_count > 0 && <Held channel={channel} isAdmin={isAdmin} onDone={setHeldDone} />}
      {heldDone && <p className="notice notice-plain" role="status" tabIndex={-1} ref={heldNote}><span>{heldDone.kind === 'opened' ? m.heldOpened(heldDone.count, heldDone.skipped ?? 0) : m.heldDiscarded(heldDone.count)}</span>
        {heldDone.kind === 'opened' && <span className="notice-actions"><a href={`/?channel_id=${channel.id}`}>{m.viewInInbox}</a></span>}</p>}
      {channel.enabled && channel.unsent_count > 0 && <Unsent channel={channel} />}
      <Tabs label={channel.name} prefix={prefix} value={open} tabs={tabs} onChange={(next) => navigate(withSearch(`${base}/${id}`, { tab: next }), true)} />
      <div role="tabpanel" id={panelID(prefix, open)} aria-labelledby={tabID(prefix, open)} className="ch-panel">
        {open === 'history' ? <History timezone={timezone} channelID={channel.id} />
          : open === 'ai' ? <ChannelAITab channel={channel} isAdmin={isAdmin} />
          : ui && <Part part={ui.panels} failed={<PartUnavailable />} render={({ Tabs: panels }) => {
              const Panel = panels[open];
              return Panel ? <Panel channel={channel} canManage={isAdmin} timezone={timezone} navigate={navigate} /> : null;
            }} />}
      </div>
      {isAdmin && <div className="button-row ch-remove">
        <button type="button" className="danger-action" onClick={() => setRemoving(true)}>{ui?.removal?.label ?? m.remove}</button>
        {remove.isError && <span role="alert" className="error-copy">{remove.error instanceof InvalidRequestError ? remove.error.message : m.actionFailed}</span>}
      </div>}
    </section>
    {removing && <Dialog alert title={ui?.removal?.title ?? m.removeTitle(channel.name)} closeLabel={m.close} onClose={() => setRemoving(false)}
      onSubmit={() => { if (channel.held_count > 0) return; setRemoving(false); remove.mutate(); }}
      footer={<><button type="button" autoFocus onClick={() => setRemoving(false)}>{m.cancel}</button>
        <button type="submit" className="danger-action" disabled={channel.held_count > 0}>{ui?.removal?.confirm ?? m.removeConfirm}</button></>}>
      <p>{ui?.removal?.body(channel) ?? m.removeBody}</p>
      {channel.is_default && <p>{nextDefault ? m.removeDefaultNext(nextDefault.name) : m.removeDefaultNone(ui?.label ?? channel.type)}</p>}
      {channel.held_count > 0 && <p role="alert" className="error-copy">{m.removeHeld(channel.held_count)}</p>}
    </Dialog>}
  </>;
}

// Held is what arrived while the channel was off: open it as conversations, or discard it after a question.
function Held({ channel, isAdmin, onDone }: { channel: ChannelDetail; isAdmin: boolean; onDone: (done: { kind: 'opened' | 'discarded'; count: number; skipped?: number }) => void }) {
  const client = useQueryClient();
  const [asking, setAsking] = useState(false);
  const reload = () => client.invalidateQueries({ predicate: (query) => ['channels', 'channel', 'channel-events', 'conversations'].includes(String(query.queryKey[0])) });
  const replay = useMutation({ mutationFn: () => channelsApi.replayHeld(channel.id),
    // Opened: a new conversation or one added to an existing one; a copy already in the inbox or one the
    // channel's rules drop is counted apart.
    onSuccess: ({ results }) => {
      const opened = results.filter((result) => result.status === 'created' || result.status === 'threaded').length;
      onDone({ kind: 'opened', count: opened, skipped: results.length - opened });
      return reload();
    } });
  const discard = useMutation({ mutationFn: () => channelsApi.discardHeld(channel.id),
    onSuccess: ({ discarded }) => { onDone({ kind: 'discarded', count: discarded }); return reload(); } });
  const busy = replay.isPending || discard.isPending;
  const failed = replay.error ?? discard.error;
  return <div className="notice" role="status">
    <span>{m.held(channel.held_count, channel.name)}</span>
    {isAdmin && <span className="notice-actions">
      <button type="button" disabled={busy} onClick={() => replay.mutate()}>{m.openHeld}</button>
      <button type="button" className="danger-action" disabled={busy} onClick={() => setAsking(true)}>{m.discardHeld(channel.held_count)}</button>
    </span>}
    {failed && <span role="alert" className="error-copy">{failed instanceof InvalidRequestError ? failed.message : m.actionFailed}</span>}
    {asking && <Dialog alert title={m.discardTitle(channel.held_count)} closeLabel={m.close} onClose={() => setAsking(false)}
      onSubmit={() => { setAsking(false); discard.mutate(); }}
      footer={<><button type="button" autoFocus onClick={() => setAsking(false)}>{m.cancel}</button><button type="submit" className="danger-action">{m.discard}</button></>}>
      <p>{m.discardBody}</p>
    </Dialog>}
  </div>;
}

function History({ timezone, channelID }: { timezone: string; channelID?: string }) {
  const events = useInfiniteQuery({
    queryKey: ['channel-events', channelID ?? ''],
    queryFn: ({ pageParam }) => channelsApi.events(channelID, pageParam || undefined),
    initialPageParam: '',
    getNextPageParam: (page) => page.next_cursor ?? undefined,
  });
  const items = events.data?.pages.flatMap((page) => page.items) ?? [];
  return <>
    <section className="panel" aria-label={m.historyHeading}>
      {events.isError ? <LoadFailed onRetry={() => void events.refetch()} />
        : !events.data ? <div className="ch-loading" aria-busy="true" />
          : items.length === 0 ? <p className="ch-empty-copy muted-copy">{m.historyEmpty}</p>
            : <ul className="ch-history">{items.map((event) => <li key={event.id}><time dateTime={event.at}>{formatDate(event.at, timezone)}</time>
                <span><b>{event.actor.name ?? event.actor.token_label ?? m.someone}</b>{event.actor.name && event.actor.token_label ? m.viaToken(event.actor.token_label) : ''} {m.event(event)}</span></li>)}</ul>}
    </section>
    {events.hasNextPage && <span className="button-row"><button type="button" disabled={events.isFetchingNextPage} onClick={() => void events.fetchNextPage()}>
      {events.isFetchingNextPage ? messages.loading : m.showMore}</button></span>}
    <p className="help">{m.historyHelp}</p>
  </>;
}

// ChannelIdentity is the channel's name, which an admin changes here, and whether it is its type's
// default, which an admin moves here.
function ChannelIdentity({ channel, label, defaultHelp, defaultBadge, isAdmin }: {
  channel: ChannelDetail; label: string; defaultHelp?: string; defaultBadge?: string; isAdmin: boolean;
}) {
  const client = useQueryClient();
  const reload = () => client.invalidateQueries({ predicate: (query) => ['channels', 'channel', 'channel-events', 'conversations'].includes(String(query.queryKey[0])) });
  const rename = useMutation({ mutationFn: (name: string) => channelsApi.update(channel.id, { name }), onSuccess: reload });
  const makeDefault = useMutation({ mutationFn: () => channelsApi.update(channel.id, { is_default: true }), onSuccess: reload });
  // The button goes once it worked; the words that say so take the focus it had.
  const done = useRef<HTMLParagraphElement>(null);
  const succeeded = makeDefault.isSuccess;
  // The words show once the channel read again says it is the default.
  const said = succeeded && channel.is_default === true;
  useEffect(() => { if (said) done.current?.focus(); }, [said]);
  return <div className="ch-identity">
    {isAdmin && <NameForm label={m.nameLabel} saveLabel={m.saveName} hint={defaultHelp ?? m.defaultHelp} value={channel.name} canManage maxLength={80} save={saveState(rename)}
      onSave={(name) => rename.mutate(name)} conflictLink={(conflict) => <a href={`${base}/${conflict.channel_id}`}>{m.openTaken(conflict.channel_name)}</a>} />}
    <div className="ch-default-row">
      {channel.is_default
        ? succeeded
          ? <p className="notice notice-plain" role="status" tabIndex={-1} ref={done}><span>{m.madeDefault(channel.name)}</span></p>
          : <p><span className="ch-default">{defaultBadge ?? m.defaultOf(label)}</span></p>
        : isAdmin && <span className="button-row"><button type="button" disabled={makeDefault.isPending} onClick={() => makeDefault.mutate()}>
            {makeDefault.isPending ? m.saving : m.makeDefault}</button></span>}
      {makeDefault.isError && <span role="alert" className="error-copy">{makeDefault.error instanceof InvalidRequestError ? makeDefault.error.message : m.actionFailed}</span>}
    </div>
  </div>;
}
