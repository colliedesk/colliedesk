import { useEffect, useId, useRef, useState } from 'react';
import type { FormEvent, ReactNode } from 'react';
import type { AIEvent, AILevel, AIModelsInput, AIProvider, AIProviderInput, AISettings, AISlot, AISlotInput, AISlotName, AIUsage } from '../../api/ai';
import { formatDate, formatDay } from '../../lib/format-date';
import { Choice } from '../../ui/choice';
import { Dialog as SharedDialog, Field } from '../../ui/dialog';
import { panelID, tabID, Tabs } from '../../ui/tabs';
import { aiMessages as m } from './ai-messages';
import { aiChatMessages as chatMessages } from './ai-chat-messages';
import { AIChatPanel } from './ai-chat-panel';
import { featureName, levelMeaning, levelName, levelsOf, scopeName } from './ai-levels';
import { AutoDraftDialog, AutoTriageDialog, money, parts } from './ai-level-dialogs';
import type { Part } from './ai-level-dialogs';
import { SettingsNav } from './settings-nav';
import type { Shown } from './settings-nav';
import { selfHostGuide } from '../../lib/self-host-guide';
import './ai-settings.css';

// Icons only this page uses stay in its chunk.
function Glyph({ children }: { children: ReactNode }) {
  return <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false">{children}</svg>;
}
const PlusIcon = () => <Glyph><path d="M12 5v14M5 12h14" /></Glyph>;
const KeyIcon = () => <Glyph><circle cx="7.5" cy="15.5" r="4.5" /><path d="m10.7 12.3 9.8-9.8M17 6l3 3M14.5 8.5l2 2" /></Glyph>;
const TrashIcon = () => <Glyph><path d="M3 6h18M8 6V4h8v2M6 6l1 14h10l1-14" /></Glyph>;
const ServerIcon = () => <Glyph><rect x="3" y="4" width="18" height="7" rx="1.5" /><rect x="3" y="13" width="18" height="7" rx="1.5" /><path d="M7 7.5h.01M7 16.5h.01" /></Glyph>;
const InfoIcon = () => <Glyph><circle cx="12" cy="12" r="9" /><path d="M12 11v5M12 8h.01" /></Glyph>;

export type AITab = 'features' | 'providers' | 'chat' | 'usage' | 'history';
// FieldError places a refusal under the field it is about.
export interface FieldError { field: 'label' | 'base_url' | 'api_key' | 'models' | 'form'; text: string }

export interface AIPageProps {
  isAdmin: boolean;
  workspaceName: string;
  timezone: string;
  shown: Shown;
  settings?: AISettings;
  loadFailed?: boolean;
  // showConsent offers the one-time banner that turns both features on with the server's provider.
  showConsent: boolean;
  // busySlot is the feature being saved; busyProvider the provider being checked.
  busySlot?: AISlotName;
  busyProvider?: string;
  // error is the answer of the last page action (a feature or a check) that failed.
  error?: string;
  onSetSlot: (slot: AISlotName, input: AISlotInput) => void;
  onConsent: () => void;
  onDismissConsent: () => void;
  onCheck: (id: string) => void;
  // The dialogs' actions resolve to a refusal to show, or nothing when they worked.
  onAdd: (input: AIProviderInput) => Promise<FieldError | undefined>;
  // The models a key may use, before the provider is saved, or why they could not be listed.
  onListModels: (input: AIModelsInput) => Promise<string[] | FieldError>;
  onSetKey: (id: string, key: string) => Promise<FieldError | undefined>;
  onRemove: (id: string) => Promise<FieldError | undefined>;
  onSetAutomatic: (perDay: number, perMinute: number, draftPerDay?: number) => Promise<FieldError | undefined>;
  // The usage of the month shown in the Usage tab, asked for when the tab opens or the month changes.
  usage?: AIUsage;
  usageFailed?: boolean;
  onUsageMonth: (month: string) => void;
  // onOpenChat opens a chat's transcript (the AI chat tab); initialTab is the tab the page opens on.
  onOpenChat?: (id: string) => void;
  initialTab?: AITab;
  // onSetChatCap sets the AI chat feature's own monthly spend limit (null removes it).
  onSetChatCap?: (cap: number | null) => Promise<unknown>;
  // chatChannels: the workspace has a chat channel that collects messages, so intake has a row.
  chatChannels?: boolean;
  // Gives (true) or takes back (false) the consent that intake may send a kind of context.
  onSourceConsent?: (source: string, on: boolean) => Promise<unknown>;
}

const tokens = (n: number) => (n >= 1e6 ? `${(n / 1e6).toFixed(2)}M` : n >= 1e3 ? `${(n / 1e3).toFixed(1)}k` : String(n));

// recentMonths lists this UTC month and the five before it, as YYYY-MM.
export function recentMonths(now: Date = new Date()): string[] {
  return Array.from({ length: 6 }, (_, back) => {
    const at = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth() - back, 1));
    return `${at.getUTCFullYear()}-${String(at.getUTCMonth() + 1).padStart(2, '0')}`;
  });
}

const slotLabel = (slot: string) => m.slotName[slot as AISlotName] ?? slot;

// focusSwitch puts focus back on a feature's default level once its dialog closes (the dialog is gone
// from the page, so the browser would leave it on the body).
const focusSwitch = (slot: AISlotName) => requestAnimationFrame(() => document.querySelector<HTMLInputElement>(`#ai-auto-${slot} input:checked`)?.focus());

// useFocusAfterSave puts focus back on an automatic mode's switch once the change it started is saved:
// the row is drawn again meanwhile, and focus would otherwise land on the body.
function useFocusAfterSave(busySlot: AISlotName | undefined) {
  const pending = useRef<AISlotName>(undefined);
  useEffect(() => {
    if (busySlot === undefined && pending.current) {
      focusSwitch(pending.current);
      pending.current = undefined;
    }
  }, [busySlot]);
  return (slot: AISlotName) => { pending.current = slot; };
}

export function AIPage(props: AIPageProps) {
  const { settings, isAdmin } = props;
  const focusAfterSave = useFocusAfterSave(props.busySlot);
  const setSlot = (slot: AISlotName, input: AISlotInput) => { focusAfterSave(slot); props.onSetSlot(slot, input); };
  const [tab, setTab] = useState<AITab>(props.initialTab ?? 'features');
  const [dialog, setDialog] = useState<{ kind: 'add' } | { kind: 'automatic' } | { kind: 'auto-triage' | 'auto-draft'; slot: AISlot } | { kind: 'key' | 'delete'; provider: AIProvider }>();
  const ids = { tabs: useId() };
  const full = settings ? settings.providers.length >= settings.provider_limit : false;
  const tabs: { id: AITab; label: string; count?: number }[] = [
    { id: 'features', label: m.tabFeatures }, { id: 'providers', label: m.tabProviders, count: settings ? settings.providers.length + (settings.server ? 1 : 0) : undefined },
    { id: 'chat', label: chatMessages.tab }, ...(isAdmin ? [{ id: 'usage' as const, label: m.tabUsage }] : []), { id: 'history', label: m.tabHistory },
  ];
  const [month, setMonth] = useState(() => recentMonths()[0]);
  const show = (next: AITab) => { setTab(next); if (next === 'usage') props.onUsageMonth(month); };
  const openAdd = () => { setTab('providers'); setDialog({ kind: 'add' }); };
  return (
    <main className="settings-page panel ai-settings">
      <header><h1>{m.heading}</h1><p>{m.description}</p></header>
      <div className="settings-layout">
        <SettingsNav page="/settings/ai" shown={props.shown} />
        <div className="settings-sections ai-page">
          {props.loadFailed && <p role="alert" className="error-copy">{m.loadFailed}</p>}
          {!settings ? (!props.loadFailed && <div className="ai-loading" aria-busy="true" />) : <>
            <div className="ai-tabs-row">
              <Tabs label={m.tabs} tabs={tabs} value={tab} onChange={show} prefix={ids.tabs} />
              {isAdmin && <button type="button" className="ai-add" aria-label={m.addProvider} onClick={openAdd} disabled={full}
                aria-describedby={full ? 'ai-limit-why' : undefined}><PlusIcon /><span className="ai-wide">{m.addProvider}</span><span className="ai-narrow">{m.addShort}</span></button>}
            </div>
            {props.error && <p role="alert" className="error-copy">{props.error}</p>}
            <div role="tabpanel" id={panelID(ids.tabs, tab)} aria-labelledby={tabID(ids.tabs, tab)} className="ai-panel-stack">
              {tab === 'features' && <Features {...props} settings={settings} onOpenAdd={openAdd} onOpenAutomatic={() => setDialog({ kind: 'automatic' })}
                onConfirmAuto={(slot) => setDialog({ kind: slot.slot === 'draft' ? 'auto-draft' : 'auto-triage', slot })} onSetAuto={setSlot} />}
              {tab === 'chat' && <AIChatPanel timezone={props.timezone} onOpen={(id) => props.onOpenChat?.(id)} slot={settings.slots.find((slot) => slot.slot === 'chat')}
                onSetCap={props.onSetChatCap} isAdmin={isAdmin} serverPrices={settings.server !== null} />}
              {tab === 'usage' && <Usage usage={props.usage} failed={props.usageFailed} month={month} onMonth={(next) => { setMonth(next); props.onUsageMonth(next); }} />}
              {tab === 'providers' && <Providers {...props} settings={settings}
                onKey={(provider) => setDialog({ kind: 'key', provider })} onDelete={(provider) => setDialog({ kind: 'delete', provider })} />}
              {tab === 'history' && <History events={settings.events} timezone={props.timezone} />}
            </div>
          </>}
        </div>
      </div>
      {dialog?.kind === 'add' && settings && <AddDialog settings={settings} onClose={() => setDialog(undefined)} onAdd={props.onAdd} onListModels={props.onListModels} />}
      {dialog?.kind === 'key' && <KeyDialog provider={dialog.provider} onClose={() => setDialog(undefined)} onSetKey={props.onSetKey} />}
      {dialog?.kind === 'automatic' && settings && <AutomaticDialog settings={settings} onClose={() => setDialog(undefined)} onSave={props.onSetAutomatic} />}
      {dialog?.kind === 'delete' && settings && <DeleteDialog provider={dialog.provider} settings={settings} onClose={() => setDialog(undefined)} onRemove={props.onRemove} />}
      {dialog?.kind === 'auto-draft' && settings && <AutoDraftDialog slot={dialog.slot} settings={settings} onClose={() => { setDialog(undefined); focusSwitch('draft'); }}
        onTurnOn={() => { setDialog(undefined); setSlot('draft', { ...slotInput(dialog.slot), default_level: 'suggest' }); }} />}
      {dialog?.kind === 'auto-triage' && <AutoTriageDialog slot={dialog.slot} onClose={() => { setDialog(undefined); focusSwitch('triage'); }}
        onTurnOn={() => { setDialog(undefined); setSlot('triage', { ...slotInput(dialog.slot), default_level: 'auto' }); }} />}
    </main>
  );
}

type FeatureActions = { onOpenAutomatic: () => void; onConfirmAuto: (slot: AISlot) => void; onSetAuto: (slot: AISlotName, input: AISlotInput) => void };

function Features(props: AIPageProps & FeatureActions & { settings: AISettings; onOpenAdd: () => void }) {
  const { settings, isAdmin } = props;
  const server = settings.server;
  const nothing = !server && settings.providers.length === 0;
  const models = (slot: AISlotName) => server?.models[slot]?.[0] ?? '';
  return <div className={`ai-panel-stack${nothing ? ' ai-nothing' : ''}`}>
    {!isAdmin && <div className="notice notice-plain" role="note"><InfoIcon /><span><strong>{m.agentNotice}</strong> {m.agentNoticeMore}</span></div>}
    {props.isAdmin && <AutoMovedNotice settings={settings} slotName="triage" onTurnOn={props.onConfirmAuto} />}
    {props.isAdmin && <AutoMovedNotice settings={settings} slotName="draft" onTurnOn={props.onConfirmAuto} />}
    {settings.automatic.paused_until && settings.slots.some((slot) => slot.slot === 'triage' && slot.enabled) && <div className="notice" role="status">
      <span><strong>{m.pausedHeading(formatDate(settings.automatic.paused_until, props.timezone))}</strong> {m.pausedBody(settings.automatic.per_day)}</span>
      {isAdmin && <div className="notice-actions"><button type="button" onClick={props.onOpenAutomatic}>{m.raiseLimit}</button></div>}
    </div>}
    {settings.allowance && settings.allowance.used_micro_usd >= settings.allowance.limit_micro_usd && settings.slots.some((slot) => slot.source === 'server' && slot.enabled)
      && <div className="notice notice-danger" role="alert">
        <span><strong>{m.allowanceUsedHeading(server?.label ?? '')}</strong> {m.allowanceUsedBody(formatDay(settings.allowance.resets_at, 'UTC'))}</span>
        {isAdmin && <div className="notice-actions"><button type="button" className="primary-action" onClick={props.onOpenAdd}>{m.addProvider}</button></div>}
      </div>}
    {props.showConsent && server && isAdmin && <section className="panel ai-consent" aria-labelledby="ai-consent-heading">
      <div><h2 id="ai-consent-heading">{m.consentHeading(props.workspaceName)}</h2>
        <p>{m.consentBody(server.label, server.kind === 'anthropic' ? m.kindRunsAnthropic : m.kindRunsOther, m.serverModels(models('triage'), models('draft')))} <a href="https://github.com/colliedesk/colliedesk/blob/main/docs/user/ai.md#what-a-provider-receives" target="_blank" rel="noopener noreferrer">{m.consentLink}</a></p></div>
      <div className="ai-consent-actions">
        <button type="button" onClick={props.onDismissConsent}>{m.consentLater}</button>
        <button type="button" className="primary-action" onClick={props.onConsent} disabled={Boolean(props.busySlot)}>{m.consentTurnOn}</button>
      </div>
    </section>}
    {server && <section className={`panel ai-server${settings.allowance ? ' ai-server-meter' : ''}`} aria-label={m.serverCardLabel}>
      <ServerIcon />
      <div><h2>{server.label} <span className="badge">{m.serverProviderBadge}</span></h2>
        <p>{[server.kind === 'anthropic' ? m.kindAnthropic : m.kindOpenAI, m.serverModels(models('triage'), models('draft'))].filter(Boolean).join(' · ')}</p></div>
      {settings.allowance && <Meter allowance={settings.allowance} />}
    </section>}
    {nothing && <section className="ai-empty" aria-labelledby="ai-empty-heading">
      <header><h2 id="ai-empty-heading">{m.emptyHeading}</h2><p>{m.emptyBody}</p></header>
      <div className="panel ai-way"><h3>{m.emptyOwnHeading}</h3><p>{m.emptyOwnBody}</p>
        {isAdmin && <div className="button-row"><button type="button" className="primary-action" onClick={props.onOpenAdd}><PlusIcon />{m.addProvider}</button></div>}</div>
      <div className="panel ai-way"><h3>{m.emptyOperatorHeading}</h3><p>{m.emptyOperatorBody}</p>
        <div className="button-row"><a className="button" href={selfHostGuide('environment-variables.md', 'ai-provider')} target="_blank" rel="noopener noreferrer">{m.setupGuide}</a></div></div>
    </section>}
    <section className="panel ai-slots" aria-label={m.features}>
      <div className="ai-slot-head" aria-hidden="true"><span>{m.columnFeature}</span><span>{m.columnProvider}</span><span>{m.columnModel}</span><span>{m.columnAutomatic}</span><span>{m.columnOn}</span></div>
      {/* Intake runs only on chat channels; its row comes with them. */}
      {settings.slots.filter((slot) => slot.slot !== 'intake' || props.chatChannels).map((slot) => <SlotRow key={slot.slot} slot={slot} {...props} />)}
    </section>
    <div className="panel ai-sends"><InfoIcon /><span><span className="ai-long">{m.sends} </span><b>{m.sendsStrong}</b></span></div>
  </div>;
}

// A choice of provider is "server" or a provider id.
const sourceOf = (slot: AISlot) => (slot.source === 'server' ? 'server' : slot.provider_id ?? '');

// slotInput is the slot as it is, to change one thing of it.
function slotInput(slot: AISlot): AISlotInput {
  const input: AISlotInput = slot.source === 'server' ? { source: 'server', enabled: slot.enabled }
    : slot.provider_id ? { source: 'provider', provider_id: slot.provider_id, enabled: slot.enabled } : { source: 'none', enabled: false };
  if (slot.model && input.source !== 'none') input.model = slot.model;
  input.effort = slot.effort;
  return input;
}


function SlotRow({ slot, settings, isAdmin, busySlot, onSetSlot, onOpenAutomatic, onConfirmAuto, onSetAuto, onSourceConsent }: AIPageProps & FeatureActions & { slot: AISlot; settings: AISettings }) {
  const name = slotLabel(slot.slot);
  const serverModels = settings.server?.models[slot.slot] ?? [];
  const choices = [
    ...(serverModels.length > 0 && settings.server ? [{ value: 'server', label: settings.server.label, models: serverModels }] : []),
    ...settings.providers.map((provider) => ({ value: provider.id, label: provider.label, models: provider.models })),
  ];
  const chosen = sourceOf(slot);
  const choice = choices.find((item) => item.value === chosen);
  const models = choice?.models ?? (slot.model ? [slot.model] : []);
  const saving = busySlot === slot.slot;
  const locked = !isAdmin || saving;
  const save = (next: { source?: string; model?: string; enabled?: boolean }) => {
    const source = next.source ?? chosen;
    const input: AISlotInput = source === 'server' ? { source: 'server', enabled: next.enabled ?? slot.enabled }
      : source ? { source: 'provider', provider_id: source, enabled: next.enabled ?? slot.enabled } : { source: 'none', enabled: false };
    const model = next.model ?? (next.source ? undefined : slot.model ?? undefined);
    if (model && input.source !== 'none') input.model = model;
    input.effort = slot.effort;
    onSetSlot(slot.slot, input);
  };
  const stopped = slot.enabled && !slot.active;
  const allowanceUsed = slot.enabled && slot.source === 'server' && settings.allowance !== null && settings.allowance.used_micro_usd >= settings.allowance.limit_micro_usd;
  const paused = slot.enabled && slot.slot === 'triage' && settings.automatic.paused_until !== null;
  return (
    <div className="ai-slot">
      <div className="ai-slot-what">
        <strong>{name} {stopped ? <span className="status ai-st-err">{m.statusStopped}</span>
          : allowanceUsed ? <span className="status ai-st-err">{m.statusAllowanceUsed}</span>
            : paused ? <span className="status ai-st-warn">{m.statusPaused}</span>
              : slot.enabled ? <span className="status ai-st-ok">{m.statusOn}</span> : <span className="status ai-st-off">{m.statusOff}</span>}</strong>
        <span>{m.slotHelp[slot.slot]}</span>
      </div>
      <label className="ai-slot-provider"><span className="ai-cell-label">{m.columnProvider}</span>
        <select aria-label={m.providerFor(name)} value={chosen} disabled={locked || choices.length === 0} onChange={(event) => save({ source: event.target.value })}>
          {choices.length === 0 && <option value="">{m.noProvider}</option>}
          {choices.length > 0 && !choice && <option value="">{m.pickProvider}</option>}
          {choices.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}
        </select></label>
      <label className="ai-slot-model"><span className="ai-cell-label">{m.columnModel}</span>
        <select aria-label={m.modelFor(name)} value={slot.model ?? ''} disabled={locked || !choice} onChange={(event) => save({ model: event.target.value })}>
          {models.length === 0 && <option value="">—</option>}
          {models.map((model) => <option key={model} value={model}>{model}</option>)}
        </select></label>
      <div className="ai-slot-limit"><span className="ai-cell-label">{m.columnAutomatic}</span>
        {slot.slot === 'triage' || slot.slot === 'intake' || slot.automatic ? <><span>{slot.slot === 'triage' ? m.automaticRuns(settings.automatic.per_day, settings.automatic.per_minute)
          : slot.slot === 'intake' ? m.intakeLimits(settings.automatic.intake_per_day) : m.draftLimits(settings.automatic.draft_per_day)}</span>
          {isAdmin && <button type="button" className="link-button" onClick={onOpenAutomatic} aria-label={m.changeAutomatic}>{m.change}</button>}</>
          : <span className="muted-copy">{slot.slot === 'chat' ? m.chatRuns : m.noAutomatic}</span>}
      </div>
      <label className="switch"><input type="checkbox" role="switch" aria-label={m.switchFor(name)} checked={slot.enabled} disabled={locked || !choice}
        onChange={(event) => save({ enabled: event.target.checked })} /></label>
      {saving && <p className="ai-slot-why" role="status">{m.saving}</p>}
      {!saving && choices.length === 0 && <p className="ai-slot-why help">{m.addFirst}</p>}
      {!saving && stopped && slot.off_reason && <p className="ai-slot-why error-copy">{m.offReason[slot.off_reason]}</p>}
      {slot.slot === 'draft' && <AutoDraft slot={slot} settings={settings} isAdmin={isAdmin} locked={locked} hasProvider={choices.length > 0}
        onTurnOn={() => onConfirmAuto(slot)} onOff={() => onSetAuto('draft', { ...slotInput(slot), default_level: 'off' })} onRaise={onOpenAutomatic} />}
      {slot.slot === 'triage' && <AutoTriage slot={slot} settings={settings} isAdmin={isAdmin} locked={locked} hasProvider={choices.length > 0}
        onTurnOn={() => onConfirmAuto(slot)} onSet={(input) => onSetAuto('triage', { ...slotInput(slot), ...input })} />}
      {slot.slot === 'intake' && <IntakeDefault slot={slot} locked={locked} hasProvider={choices.length > 0}
        onSet={(level) => onSetAuto('intake', { ...slotInput(slot), default_level: level })} />}
      {slot.slot === 'intake' && <IntakeSources settings={settings} isAdmin={isAdmin} onSourceConsent={onSourceConsent} />}
      {/* The help center's chat has no level per channel. */}
      {slot.slot !== 'chat' && <ChannelsThatDiffer slot={slot} settings={settings} />}
    </div>
  );
}

// levelItems are the levels a feature takes, each named for what it does.
const levelItems = (slot: AISlotName) => levelsOf(slot).map((level) => ({ value: level, label: levelName[level] }));

// AutoTriage is what triage does by itself where no channel says otherwise: off, suggest for each new
// conversation, or apply by itself (a dialog says what it does first), with the parts it applies and how
// it went in 30 days.
function AutoTriage({ slot, settings, isAdmin, locked, hasProvider, onTurnOn, onSet }: { slot: AISlot; settings: AISettings; isAdmin: boolean; locked: boolean; hasProvider: boolean;
  onTurnOn: () => void; onSet: (input: Partial<AISlotInput>) => void }) {
  const level: AILevel = slot.enabled ? slot.default_level : 'off';
  const on = level === 'auto';
  const id = useId();
  const why = !hasProvider ? m.autoNeedsProvider : !slot.enabled ? m.autoNeedsFeature : undefined;
  const chosen = slot.auto_parts.length > 0 ? slot.auto_parts : (['tags', 'priority'] as Part[]);
  const toggle = (part: Part) => {
    const next = chosen.includes(part) ? chosen.filter((item) => item !== part) : [...chosen, part];
    if (next.length > 0) onSet({ auto_parts: parts.filter((item) => next.includes(item)) });
  };
  return (
    <div className="ai-auto">
      <div className="ai-auto-lead"><strong id={id}>{m.levelHeading}</strong></div>
      {why ? <p className="ai-auto-desc muted-copy" id={`${id}-why`}>{why}</p>
        : on ? <p className="ai-auto-desc"><span className="ai-long">{m.autoDesc} </span><b>{m.autoDescStrong}</b>{m.autoDescRest}</p>
          : <p className="ai-auto-desc">{levelMeaning.triage[level]}.</p>}
      {!why && on && <fieldset className="ai-auto-parts"><legend className="sr-only">{m.autoParts}</legend>
        {parts.map((part) => <label key={part} className="ai-pick"><input type="checkbox" checked={chosen.includes(part)}
          disabled={locked || (chosen.length === 1 && chosen.includes(part))} title={chosen.length === 1 && chosen.includes(part) ? m.autoLastPart : undefined}
          onChange={() => toggle(part)} />{m.autoPart[part]}</label>)}
        {chosen.length === 1 && <span className="muted-copy ai-auto-hint">{m.autoLastPart}</span>}
        <span className="ai-auto-stat">{m.autoStat(settings.auto_triage.applied, settings.auto_triage.undone)} <span className="muted-copy">{m.autoStatWhen}</span></span>
      </fieldset>}
      {!why && !on && isAdmin && <p className="ai-auto-note muted-copy ai-long">{m.autoPickLater}</p>}
      <div className="ai-auto-switch" id="ai-auto-triage">
        <Choice legend={m.levelLegend(featureName.triage)} hideLegend items={levelItems('triage')} value={level} disabled={locked || !slot.enabled}
          describedBy={why ? `${id}-why` : id} onChange={(next) => (next === 'auto' ? onTurnOn() : onSet({ default_level: next }))} />
      </div>
    </div>
  );
}

// IntakeDefault is what intake does where no chat channel says otherwise: the channel's rules alone, or
// the rules with AI's suggestion in Review. Deciding by itself (Auto) is set on each channel, where its
// readiness is known.
function IntakeDefault({ slot, locked, hasProvider, onSet }: { slot: AISlot; locked: boolean; hasProvider: boolean; onSet: (level: AILevel) => void }) {
  const level: AILevel = slot.enabled && slot.default_level !== 'auto' ? slot.default_level : 'off';
  const id = useId();
  const why = !hasProvider ? m.autoNeedsProvider : !slot.enabled ? m.intakeNeedsFeature : undefined;
  return (
    <div className="ai-auto">
      <div className="ai-auto-lead"><strong id={id}>{m.levelHeading}</strong></div>
      {why ? <p className="ai-auto-desc muted-copy" id={`${id}-why`}>{why}</p> : <p className="ai-auto-desc">{levelMeaning.intake[level]}.</p>}
      <p className="ai-auto-note muted-copy">{m.intakeChatOnly}</p>
      <div className="ai-auto-switch">
        <Choice legend={m.levelLegend(featureName.intake)} hideLegend items={levelItems('intake').filter((item) => item.value !== 'auto')} value={level}
          disabled={locked || !slot.enabled} describedBy={why ? `${id}-why` : id} onChange={onSet} />
      </div>
    </div>
  );
}

const intakeSources = ['images', 'contact_profile'] as const;

// IntakeSources is the consent that chat intake may send more than the conversation's text: the pictures
// customers sent and the customer's profile, each given and taken back here (docs/user/ai.md). A channel
// then turns each on for its scopes.
function IntakeSources({ settings, isAdmin, onSourceConsent }: { settings: AISettings; isAdmin: boolean; onSourceConsent?: (source: string, on: boolean) => Promise<unknown> }) {
  const given = new Set(settings.source_consents.map((consent) => consent.source_class));
  const [busy, setBusy] = useState<string>();
  const [problem, setProblem] = useState<string>();
  const toggle = async (source: string, on: boolean) => {
    if (!onSourceConsent) return;
    setBusy(source);
    setProblem(undefined);
    try {
      await onSourceConsent(source, on);
    } catch {
      setProblem(m.actionFailed);
    } finally {
      setBusy(undefined);
    }
  };
  return <fieldset className="ai-sources-consent">
    <legend>{m.intakeSourcesLegend}</legend>
    <p className="muted-copy">{m.intakeSourcesHelp}</p>
    {intakeSources.map((source) => <label key={source} className="ai-pick">
      <span className="switch"><input type="checkbox" role="switch" aria-label={m.intakeSource[source]} checked={given.has(source)} disabled={!isAdmin || busy !== undefined}
        onChange={(event) => void toggle(source, event.target.checked)} /></span>
      <span>{m.intakeSource[source]}</span></label>)}
    {problem && <p role="alert" className="error-copy">{problem}</p>}
  </fieldset>;
}

// AutoDraft is what reply drafts do by themselves where no channel says otherwise: only when asked, or a
// draft for every customer message (a dialog estimates what it costs); paused for the day at its limit,
// or until next month once it used half of the included AI.
function AutoDraft({ slot, settings, isAdmin, locked, hasProvider, onTurnOn, onOff, onRaise }: { slot: AISlot; settings: AISettings; isAdmin: boolean; locked: boolean;
  hasProvider: boolean; onTurnOn: () => void; onOff: () => void; onRaise: () => void }) {
  const level: AILevel = slot.enabled ? slot.default_level : 'off';
  const on = level === 'suggest';
  const limits = settings.automatic;
  const why = !hasProvider ? m.autoNeedsProvider : !slot.enabled ? m.autoDraftNeedsFeature : undefined;
  const half = settings.allowance ? money(Math.floor(settings.allowance.limit_micro_usd / 2)) : null;
  const pausedDay = on && limits.drafts_paused_until !== null;
  const pausedHalf = on && !pausedDay && limits.drafts_reserved_until !== null;
  const status = pausedDay ? <span className="status ai-st-warn">{m.autoDraftPausedDay(formatDate(limits.drafts_paused_until ?? '', 'UTC'))}</span>
    : pausedHalf ? <span className="status ai-st-warn">{m.autoDraftPausedDay(formatDay(limits.drafts_reserved_until ?? '', 'UTC'))}</span> : null;
  return (
    <div className="ai-auto">
      <div className="ai-auto-lead"><strong id="ai-auto-draft-what">{m.levelHeading}</strong>{status && <span aria-live="polite">{status}</span>}</div>
      {why ? <p className="ai-auto-desc muted-copy" id="ai-auto-draft-why">{why}</p>
        : pausedDay ? <p className="ai-auto-desc ai-auto-warn"><b>{m.autoDraftPausedDayDesc(limits.drafts_today, limits.draft_per_day)}</b>{m.autoDraftPausedDayRest}</p>
          : pausedHalf ? <p className="ai-auto-desc ai-auto-warn"><b>{m.autoDraftPausedHalfDesc(half)}</b>{m.autoDraftPausedHalfRest}</p>
            : on ? <p className="ai-auto-desc"><span className="ai-long">{m.autoDraftDesc} </span><b>{m.autoDraftStrong}</b>{m.autoDraftCost(limits.draft_per_day)}</p>
              : <p className="ai-auto-desc">{levelMeaning.draft.off}. <b>{m.autoDraftStrong}</b></p>}
      <div className="ai-auto-side">
        {on && !pausedDay && !pausedHalf && <span className="ai-auto-stat">{m.autoDraftToday(limits.drafts_today, limits.draft_per_day)}</span>}
        {pausedDay && isAdmin && <button type="button" onClick={onRaise}>{m.autoDraftRaise}</button>}
        {pausedHalf && isAdmin && <a className="button ai-link-button" href="/settings/ai?add=1">{m.autoDraftAddKey}</a>}
        {locked && !why && <span className="sr-only" id="ai-auto-draft-why">{m.autoAdminsOnly}</span>}
        <div id="ai-auto-draft"><Choice legend={m.levelLegend(featureName.draft)} hideLegend items={levelItems('draft')} value={level} disabled={locked || !slot.enabled}
          describedBy={why || locked ? 'ai-auto-draft-why' : 'ai-auto-draft-what'} onChange={(next) => (next === 'suggest' ? onTurnOn() : onOff())} /></div>
      </div>
    </div>
  );
}

// ChannelsThatDiffer lists the channels whose level of the feature is their own, each a link to its AI
// tab; a feature the workspace has off runs on none of them, and each says so.
function ChannelsThatDiffer({ slot, settings }: { slot: AISlot; settings: AISettings }) {
  // A level equal to what the scope would follow anyway (the channel's, then the workspace's) differs from nothing.
  const own = (channel: string, scope: string) => settings.channels.find((row) => row.channel_id === channel && row.scope === scope && row.slot === slot.slot)?.level;
  const rows = settings.channels.filter((row) => row.slot === slot.slot
    && row.level !== (row.scope === '' ? slot.default_level : own(row.channel_id, '') ?? slot.default_level));
  if (rows.length === 0) return <p className="ai-differ muted-copy">{m.everyChannelFollows}</p>;
  const byChannel = new Map<string, { name: string; parts: string[] }>();
  for (const row of rows) {
    const entry = byChannel.get(row.channel_id) ?? { name: row.channel_name, parts: [] };
    entry.parts.push(`${row.scope === '' ? '' : `${scopeName(row.scope)}: `}${levelName[row.level]}`);
    byChannel.set(row.channel_id, entry);
  }
  const off = !slot.enabled;
  return <div className="ai-differ">
    <span className="ai-differ-label">{m.channelsThatDiffer}</span>
    <ul className="ai-chips">{[...byChannel].map(([id, entry]) => {
      const href = `/settings/channels/${id}?tab=ai`;
      const text = off ? m.chipOff(entry.name, featureName[slot.slot]) : `${entry.name} · ${entry.parts.join(' · ')}`;
      return <li key={id}><a className="ai-chip" href={href}>{text}</a></li>;
    })}</ul>
  </div>;
}

// AutoMovedNotice says when triage's automatic mode turned off because the feature moved to another
// provider: the admin's consent was for the provider it left.
function AutoMovedNotice({ settings, slotName, onTurnOn }: { settings: AISettings; slotName: AISlotName; onTurnOn: (slot: AISlot) => void }) {
  const slot = settings.slots.find((item) => item.slot === slotName);
  const last = settings.events.find((event) => event.kind === 'slot_changed' && event.slot === slotName);
  const detail = last?.detail as { from?: Record<string, unknown>; to?: Record<string, unknown> } | undefined;
  if (!slot || !slot.enabled || slot.automatic || !detail?.from?.automatic || detail.to?.automatic || detail.from.provider_label === detail.to?.provider_label) return null;
  const title = slotName === 'draft' ? m.autoDraftTitle : m.autoTitle;
  return <div className="notice" role="status"><span>{m.autoMovedNotice(title, slotLabel(slotName), String(detail.to?.provider_label ?? ''))}</span>
    <button type="button" onClick={() => onTurnOn(slot)}>{m.autoTurnOnAgain}</button></div>;
}

function Meter({ allowance }: { allowance: NonNullable<AISettings['allowance']> }) {
  if (allowance.limit_micro_usd === 0) return <div className="ai-meter"><strong className="ai-meter-none">{m.noneIncluded}</strong></div>;
  const used = Math.min(allowance.used_micro_usd, allowance.limit_micro_usd);
  const full = allowance.used_micro_usd >= allowance.limit_micro_usd;
  const percent = allowance.limit_micro_usd > 0 ? Math.round((used / allowance.limit_micro_usd) * 100) : 100;
  return <div className="ai-meter">
    <div className="ai-meter-top"><strong className={full ? 'error-copy' : undefined}>{m.allowanceUsed(money(allowance.used_micro_usd), money(allowance.limit_micro_usd))}</strong>
      <span>{m.resets(formatDay(allowance.resets_at, 'UTC'))}</span></div>
    <div className={`ai-bar${full ? ' ai-bar-full' : ''}`} role="meter" aria-label={m.includedUsed} aria-valuemin={0} aria-valuemax={allowance.limit_micro_usd}
      aria-valuenow={used} aria-valuetext={m.allowanceUsed(money(allowance.used_micro_usd), money(allowance.limit_micro_usd))}><i style={{ width: `${percent}%` }} /></div>
    {allowance.drafts_left !== null && !full && <span className="ai-meter-left">{m.draftsLeft(allowance.drafts_left)}</span>}
  </div>;
}

function Usage({ usage, failed, month, onMonth }: { usage?: AIUsage; failed?: boolean; month: string; onMonth: (month: string) => void }) {
  const id = useId();
  return <>
    <section className="panel ai-usage-top" aria-label={m.thisMonth}>
      <label className="field" htmlFor={id}><span className="sr-only">{m.month}</span>
        <select id={id} value={month} onChange={(event) => onMonth(event.target.value)}>
          {recentMonths().map((value) => <option key={value} value={value}>{monthName(value)}</option>)}
        </select></label>
      {usage?.allowance ? <Meter allowance={usage.allowance} /> : <p className="muted-copy">{m.noAllowance}</p>}
    </section>
    {failed && <p role="alert" className="error-copy">{m.usageFailed}</p>}
    <section className="panel ai-usage" aria-label={m.usageByFeature} aria-busy={!usage && !failed}>
      {usage && (usage.rows.length === 0 ? <p className="ai-prov-empty muted-copy">{m.noUsage}</p> : <table className="ai-usage-table">
        <thead><tr><th scope="col">{m.columnFeature}</th><th scope="col" className="ai-n">{m.calls}</th><th scope="col" className="ai-n ai-hide-sm">{m.failed}</th>
          <th scope="col" className="ai-n ai-hide-sm">{m.tokens}</th><th scope="col" className="ai-n">{m.cost}</th><th scope="col" className="ai-hide-sm">{m.paidBy}</th></tr></thead>
        <tbody>{usage.rows.map((row) => <tr key={`${row.feature}-${row.billed_to}-${row.provider_label}`}>
          <td>{m.feature[row.feature]}<small>{row.provider_label}{row.automatic > 0 ? ` · ${m.automaticCalls(row.automatic)}` : ''}</small></td>
          <td className="ai-n">{row.calls}</td><td className="ai-n ai-hide-sm">{row.failed}</td>
          <td className="ai-n ai-hide-sm">{tokens(row.input_tokens)} · {tokens(row.output_tokens)}</td>
          <td className="ai-n">{row.cost_micro_usd === null ? '—' : money(row.cost_micro_usd)}</td>
          <td className="ai-hide-sm">{row.billed_to === 'server' ? row.provider_label : m.yourKey}</td></tr>)}</tbody>
      </table>)}
    </section>
    <p className="help ai-help">{m.usageHelp}</p>
  </>;
}

const monthName = (value: string) => new Date(`${value}-01T00:00:00Z`).toLocaleDateString('en', { month: 'long', year: 'numeric', timeZone: 'UTC' });

function AutomaticDialog({ settings, onClose, onSave }: { settings: AISettings; onClose: () => void; onSave: AIPageProps['onSetAutomatic'] }) {
  const [perDay, setPerDay] = useState(String(settings.automatic.per_day));
  const [perMinute, setPerMinute] = useState(String(settings.automatic.per_minute));
  const [draftsPerDay, setDraftsPerDay] = useState(String(settings.automatic.draft_per_day));
  const [error, setError] = useState<string>();
  const [bad, setBad] = useState<{ day?: string; minute?: string; drafts?: string }>({});
  const [busy, setBusy] = useState(false);
  const id = useId();
  const submit = async () => {
    const day = Number(perDay);
    const minute = Number(perMinute);
    const drafts = Number(draftsPerDay);
    const out = (value: number, max: number) => (!Number.isInteger(value) || value < 0 || value > max ? m.wholeNumber(max) : undefined);
    const marked = { day: perDay === '' ? m.wholeNumber(10000) : out(day, 10000), minute: perMinute === '' ? m.wholeNumber(1000) : out(minute, 1000),
      drafts: draftsPerDay === '' ? m.wholeNumber(10000) : out(drafts, 10000) };
    setBad(marked);
    if (marked.day || marked.minute || marked.drafts) {
      setError(m.automaticInvalid);
      return;
    }
    setBusy(true);
    const refused = await onSave(day, minute, drafts);
    setBusy(false);
    if (refused) setError(refused.text); else onClose();
  };
  return (
    <Dialog title={m.automaticHeading} help={m.automaticHelp} onClose={onClose} onSubmit={() => void submit()}
      footer={<><button type="button" onClick={onClose}>{m.cancel}</button><button type="submit" className="primary-action" disabled={busy}>{m.save}</button></>}>
      {error && <p role="alert" className="error-copy">{error}</p>}
      <div className="ai-two">
        <Field label={m.perDay} id={`${id}-day`} error={bad.day}>{(attrs) => <input {...attrs} type="number" inputMode="numeric" min={0} max={10000} value={perDay} onChange={(event) => setPerDay(event.target.value)} autoFocus />}</Field>
        <Field label={m.perMinute} id={`${id}-minute`} error={bad.minute}>{(attrs) => <input {...attrs} type="number" inputMode="numeric" min={0} max={1000} value={perMinute} onChange={(event) => setPerMinute(event.target.value)} />}</Field>
      </div>
      <Field label={m.draftsPerDay} id={`${id}-drafts`} help={m.draftsPerDayHelp} error={bad.drafts}>{(attrs) => <input {...attrs} type="number" inputMode="numeric" min={0} max={10000} value={draftsPerDay}
        onChange={(event) => setDraftsPerDay(event.target.value)} />}</Field>
      <p>{m.usedToday(settings.automatic.used_today)}</p>
    </Dialog>
  );
}

function checkWhy(code: string | null) {
  return (code && m.checkWhy[code]) || m.checkWhyOther;
}

function Providers({ settings, isAdmin, timezone, busyProvider, onCheck, onKey, onDelete }: AIPageProps & { settings: AISettings; onKey: (p: AIProvider) => void; onDelete: (p: AIProvider) => void }) {
  const left = settings.provider_limit - settings.providers.length;
  return (
    <section className="panel ai-providers" aria-label={m.tabProviders}>
      {settings.server && <div className="ai-prov">
        <div className="ai-prov-name"><strong>{settings.server.label} <span className="badge">{m.serverProviderBadge}</span></strong>
          <span>{settings.server.kind === 'anthropic' ? m.kindAnthropic : m.kindOpenAI}</span></div>
        <div className="ai-prov-meta"><span>{m.serverSetBy}</span>
          <span>{m.usedBy(settings.slots.filter((slot) => slot.source === 'server').map((slot) => slotLabel(slot.slot)).join(', '))}</span></div>
        <div className="ai-prov-state" />
        <div className="ai-prov-actions" />
      </div>}
      {settings.providers.length === 0 && !settings.server && <p className="ai-prov-empty muted-copy">{m.noProviders}</p>}
      {settings.providers.map((provider) => {
        const checking = busyProvider === provider.id;
        const when = formatDate(provider.checked_at, timezone);
        return <div className="ai-prov" key={provider.id}>
          <div className="ai-prov-name"><strong>{provider.label}</strong>
            <span>{provider.kind === 'anthropic' ? m.kindAnthropic : m.kindOpenAI} · {provider.key_hint ? <span className="ai-mono">{m.keyHint(provider.key_hint)}</span> : m.noKey}</span></div>
          <div className="ai-prov-meta">{provider.base_url && <span className="ai-mono">{provider.base_url}</span>}
            <span>{m.usedBy(provider.used_by.map(slotLabel).join(', '))}</span></div>
          <div className="ai-prov-state" aria-live="polite">{checking ? <span className="status ai-st-off">{m.checking}</span>
            : provider.check_status === 'ok' ? <><span className="status ai-st-ok">{m.working}</span><p>{m.checked(when)}</p></>
              : <><span className="status ai-st-err">{m.notWorking}</span><p className="error-copy">{m.failedCheck(checkWhy(provider.check_error_code), when)}</p></>}</div>
          {isAdmin && <div className="ai-prov-actions">
            <button type="button" onClick={() => onCheck(provider.id)} disabled={checking}>{m.check}</button>
            <button type="button" className="ai-icon-text" onClick={() => onKey(provider)} aria-label={m.changeKey}><KeyIcon /><span>{m.changeKey}</span></button>
            <button type="button" className="danger-action ai-icon-only" onClick={() => onDelete(provider)} aria-label={m.deleteProvider(provider.label)}><TrashIcon /></button>
          </div>}
        </div>;
      })}
      <p className="ai-foot" id="ai-limit-why">{left > 0 ? <span>{m.providersFoot(left)}</span> : <><strong>{m.providersFull(settings.provider_limit)}</strong> <span>{m.providersFullHelp}</span></>}
        <span>{m.keysFoot}</span></p>
    </section>
  );
}

function eventText(event: AIEvent): string {
  const label = event.provider_label ?? '';
  const detail = event.detail as { from?: Record<string, unknown>; to?: Record<string, unknown>; from_hint?: string; to_hint?: string; reason?: string };
  const slot = event.slot ? slotLabel(event.slot) : '';
  switch (event.kind) {
    case 'provider_added': return m.eventAdded(label);
    case 'provider_changed': return m.eventChanged(label);
    case 'provider_key_changed': return m.eventKey(label, detail.from_hint ?? '', detail.to_hint ?? '');
    case 'provider_removed': return m.eventRemoved(label);
    case 'chat_deleted': return m.eventChatDeleted(Number((event.detail as { turns?: number }).turns ?? 0));
  }
  if (event.kind === 'automatic_limits_changed') {
    const to = detail.to as { per_day?: number; per_minute?: number; draft_per_day?: number } | undefined;
    const from = detail.from as { per_day?: number; per_minute?: number; draft_per_day?: number } | undefined;
    if (from && to && to.draft_per_day !== undefined && from.draft_per_day !== to.draft_per_day && from.per_day === to.per_day && from.per_minute === to.per_minute) {
      return m.eventAutomaticDrafts(from.draft_per_day ?? 0, to.draft_per_day);
    }
    return m.eventAutomatic(to?.per_day ?? 0, to?.per_minute ?? 0);
  }
  if (event.kind === 'channel_level_changed') {
    const change = event.detail as { channel_name?: string; scope?: string; before?: AILevel | null; after?: AILevel | null; reason?: string; options_changed?: boolean };
    const where = `${change.channel_name ?? ''}${change.scope ? ` · ${scopeName(change.scope)}` : ''}`;
    const named = (level?: AILevel | null) => (level ? levelName[level] : m.levelFollowing);
    if (change.reason === 'recipient_changed') return m.eventLevelLowered(slot, where, named(change.after));
    if (change.reason === 'model_changed') return m.eventLevelLoweredModel(slot, where, named(change.after));
    if (change.before === change.after && change.options_changed) return m.eventLevelOptions(slot, where);
    return m.eventLevel(slot, where, named(change.before), named(change.after));
  }
  if (event.kind === 'source_consent_changed') {
    const change = event.detail as { source_class?: string; consented?: boolean };
    return change.consented ? m.eventConsentOn(change.source_class ?? '') : m.eventConsentOff(change.source_class ?? '');
  }
  if (detail.reason === 'provider_removed') return m.eventSlotRemoved(slot, label);
  const fromLevel = detail.from?.default_level as AILevel | undefined;
  const toLevel = detail.to?.default_level as AILevel | undefined;
  // Triage between off and suggest; auto, and drafts' suggest, are the older automatic switch below.
  if (event.slot === 'triage' && fromLevel && toLevel && fromLevel !== 'auto' && toLevel !== 'auto' && fromLevel !== toLevel
    && detail.from?.enabled === true && detail.to?.enabled === true && detail.from?.provider_label === detail.to?.provider_label) {
    return m.eventDefaultLevel(slot, levelName[fromLevel], levelName[toLevel]);
  }
  const fromAuto = detail.from?.automatic === true;
  const toAuto = detail.to?.automatic === true;
  const autoParts = String(detail.to?.auto_parts ?? '').split(',').filter(Boolean).map((part) => m.autoPartLower[part as Part] ?? part).join(', ');
  const fromOn = detail.from?.enabled === true;
  const toOn = detail.to?.enabled === true;
  const fromLabel = String(detail.from?.provider_label ?? '');
  const toLabel = String(detail.to?.provider_label ?? '');
  if (!fromOn && toOn) return m.eventSlotOn(slot, toLabel);
  if (fromOn && !toOn) return m.eventSlotOff(slot);
  const drafts = event.slot === 'draft';
  if (fromLabel && toLabel && fromLabel !== toLabel) {
    if (fromAuto && !toAuto) return drafts ? m.eventAutoDraftMoved(slot, toLabel) : m.eventAutoMoved(slot, toLabel);
    return m.eventSlotMoved(slot, fromLabel, toLabel);
  }
  if (!fromAuto && toAuto) return drafts ? m.eventAutoDraftOn(toLabel) : m.eventAutoOn(autoParts);
  if (fromAuto && !toAuto) return drafts ? m.eventAutoDraftOff : m.eventAutoOff;
  if (toAuto && String(detail.from?.auto_parts ?? '') !== String(detail.to?.auto_parts ?? '')) return m.eventAutoParts(autoParts);
  const fromModel = String(detail.from?.model ?? '');
  const toModel = String(detail.to?.model ?? '');
  if (fromModel && toModel && fromModel !== toModel) return m.eventSlotModel(slot, fromModel, toModel);
  const fromCap = detail.from?.monthly_cap_micro_usd as number | undefined;
  const toCap = detail.to?.monthly_cap_micro_usd as number | undefined;
  if (fromCap !== toCap) return toCap === undefined ? m.eventChatCapRemoved : m.eventChatCap(money(toCap));
  if (!fromLabel && toLabel) return m.eventSlotSet(slot, toLabel);
  return m.eventSlotChanged(slot);
}

function History({ events, timezone }: { events: AIEvent[]; timezone: string }) {
  return <>
    <section className="panel" aria-label={m.tabHistory}>
      {events.length === 0 ? <p className="ai-prov-empty muted-copy">{m.historyEmpty}</p>
        : <ul className="ai-history">{events.map((event) => <li key={event.id}><time dateTime={event.created_at}>{formatDate(event.created_at, timezone)}</time>
          <span><b>{event.actor_name ?? m.someone}</b>{event.via_token && m.viaToken} {eventText(event)}</span></li>)}</ul>}
    </section>
    <p className="help ai-help">{m.historyHelp}</p>
  </>;
}

// Dialog is the shared form dialog with this page's close label.
function Dialog(props: Omit<Parameters<typeof SharedDialog>[0], 'closeLabel'>) {
  return <SharedDialog {...props} closeLabel={m.close} className="ai-dialog" />;
}

// The most models a provider keeps (contracts/openapi.yaml, AIProviderInput).
const maxProviderModels = 20;
// Above this many listed models, the list gets a search box.
const modelSearchFrom = 8;

// What Add provider offers: Anthropic, two OpenAI-compatible services whose address is filled in, and
// any other OpenAI-compatible server, whose address the person types.
type Service = 'anthropic' | 'openai' | 'openrouter' | 'self_hosted';
const services: Service[] = ['anthropic', 'openai', 'openrouter', 'self_hosted'];
const serviceURL: Partial<Record<Service, string>> = { openai: 'https://api.openai.com/v1', openrouter: 'https://openrouter.ai/api/v1' };
const serviceText: Record<Service, [string, string]> = {
  anthropic: [m.kindAnthropic, m.kindAnthropicHelp], openai: [m.serviceOpenAI, m.serviceOpenAIHelp],
  openrouter: [m.serviceOpenRouter, m.serviceOpenRouterHelp], self_hosted: [m.serviceSelfHosted, m.serviceSelfHostedHelp],
};

type ModelList = { state: 'idle' } | { state: 'loading' } | { state: 'loaded'; models: string[] } | { state: 'typing' };

function AddDialog({ settings, onClose, onAdd, onListModels }: { settings: AISettings; onClose: () => void; onAdd: AIPageProps['onAdd']; onListModels: AIPageProps['onListModels'] }) {
  const [service, setService] = useState<Service>('anthropic');
  const kind = service === 'anthropic' ? 'anthropic' : 'openai_compatible';
  const [label, setLabel] = useState('');
  const [typedURL, setTypedURL] = useState('');
  const baseURL = serviceURL[service] ?? typedURL;
  const [key, setKey] = useState('');
  // The models picked from the provider's list, in the order picked: the first is the default.
  const [picked, setPicked] = useState<string[]>([]);
  // The names typed when the provider keeps no list (or the person prefers to type).
  const [typed, setTyped] = useState('');
  const [list, setList] = useState<ModelList>({ state: 'idle' });
  // Each load is numbered; an answer for a kind, address or key changed since is dropped.
  const loads = useRef(0);
  const [error, setError] = useState<FieldError>();
  const [busy, setBusy] = useState(false);
  const id = useId();
  const keyOptional = service === 'self_hosted' && settings.allow_private_base_urls;
  const noKeys = !settings.can_store_keys && !keyOptional;
  const left = settings.provider_limit - settings.providers.length;
  const focus = (field: FieldError['field']) => document.getElementById(`${id}-${field}`)?.focus();
  const canLoad = (key.trim() !== '' || keyOptional) && (kind === 'anthropic' || baseURL.trim() !== '') && list.state !== 'loading';
  // asked is true when the person pressed Load: only then does a refusal take the focus to its field.
  const loadModels = async (asked: boolean) => {
    if (!canLoad) return;
    const load = ++loads.current;
    setList({ state: 'loading' });
    setError(undefined);
    const answer = await onListModels({ kind, ...(key.trim() ? { api_key: key.trim() } : {}), ...(kind === 'openai_compatible' ? { base_url: baseURL.trim() } : {}) });
    if (load !== loads.current) return;
    if (Array.isArray(answer)) {
      setList({ state: 'loaded', models: answer });
      // Claude's newest Sonnet is a sound, middle-priced default; the newest model overall is often the
      // dearest. Other lists are sorted by name, so nothing is ticked for the person.
      const kept = picked.filter((model) => answer.includes(model));
      const sonnet = kind === 'anthropic' ? answer.find((model) => model.startsWith('claude-sonnet-')) : undefined;
      setPicked(kept.length > 0 ? kept : sonnet ? [sonnet] : []);
      return;
    }
    // A provider that keeps no list: the names are typed instead, with the reason under the field.
    setList(answer.field === 'models' ? { state: 'typing' } : { state: 'idle' });
    setError(answer);
    if (asked && answer.field !== 'form') focus(answer.field);
  };
  // A new kind, address or key makes the loaded list stale, and an answer still on its way with it.
  const changed = (next: () => void) => {
    next();
    setError(undefined);
    if (list.state === 'loaded' || list.state === 'loading') { loads.current += 1; setList({ state: 'idle' }); }
  };
  const submit = async () => {
    // Only what is on screen is sent: models ticked in a list loaded for this kind, address and key, or the names typed.
    const models = list.state === 'typing' ? typed.split(',').map((model) => model.trim()).filter(Boolean) : list.state === 'loaded' ? picked : [];
    const refuse = (field: FieldError['field'], text: string) => { setError({ field, text }); focus(field); };
    if (!label.trim()) return refuse('label', m.labelMissing);
    if (kind === 'openai_compatible' && !baseURL.trim()) return refuse('base_url', m.baseURLMissing);
    if (!key.trim() && !keyOptional) return refuse('api_key', m.keyMissing);
    if (models.length === 0) return refuse('models', list.state === 'typing' ? m.modelsMissing : m.modelsPick);
    setBusy(true);
    setError(undefined);
    const input: AIProviderInput = { kind, label: label.trim(), models, ...(key.trim() ? { api_key: key.trim() } : {}), ...(kind === 'openai_compatible' ? { base_url: baseURL.trim() } : {}) };
    const refused = await onAdd(input);
    setBusy(false);
    if (!refused) { onClose(); return; }
    setError(refused);
    if (refused.field !== 'form') focus(refused.field);
  };
  const fieldError = (field: FieldError['field']) => (error?.field === field ? error.text : undefined);
  return (
    <Dialog title={m.addHeading} help={m.addHelp} onClose={onClose} onSubmit={() => void submit()}
      footer={<><span className="ai-dialog-left">{left > 0 ? m.providersFoot(left) : ''}</span><button type="button" onClick={onClose}>{m.cancel}</button>
        <button type="submit" className="primary-action" disabled={busy || noKeys}>{busy ? m.checking : m.checkAndSave}</button></>}>
      {noKeys && <div className="notice notice-danger" role="alert"><span><strong>{m.noKeysNotice}</strong> {m.noKeysBody}{' '}
        <a href={selfHostGuide('environment-variables.md', 'variables')} target="_blank" rel="noopener noreferrer">{m.noKeysGuide}</a></span></div>}
      {error?.field === 'form' && <p role="alert" className="error-copy">{error.text}</p>}
      <fieldset className="ai-kind"><legend className="sr-only">{m.columnProvider}</legend>
        {services.map((value) => <label key={value}>
          <input type="radio" name={`${id}-kind`} value={value} checked={service === value} onChange={() => changed(() => setService(value))} />
          <strong>{serviceText[value][0]}</strong><span>{serviceText[value][1]}</span></label>)}
      </fieldset>
      <Field label={m.labelField} id={`${id}-label`} error={fieldError('label')}>{(attrs) => <input {...attrs} value={label} maxLength={80} onChange={(event) => setLabel(event.target.value)} autoFocus />}</Field>
      {service === 'self_hosted' && <Field label={m.baseURLField} id={`${id}-base_url`} error={fieldError('base_url')} help={m.baseURLHelp}>
        {(attrs) => <input {...attrs} type="url" inputMode="url" value={typedURL} placeholder="http://localhost:11434/v1" onChange={(event) => changed(() => setTypedURL(event.target.value))} />}</Field>}
      {service !== 'self_hosted' && fieldError('base_url') && <p role="alert" className="error-copy">{fieldError('base_url')}</p>}
      <Field label={keyOptional ? m.keyFieldOptional : m.keyField} id={`${id}-api_key`} error={fieldError('api_key')} help={m.keyHelpStored}>
        {(attrs) => <input {...attrs} type="password" autoComplete="off" value={key} placeholder={keyOptional ? m.keyOptionalPlaceholder : undefined}
          onChange={(event) => changed(() => setKey(event.target.value))}
          // Leaving the key with nothing loaded yet loads the list, so the next thing to do is pick.
          onBlur={() => { if (list.state === 'idle' && key.trim() !== '') void loadModels(false); }} />}</Field>
      {list.state === 'typing'
        ? <Field label={m.modelsTyped} id={`${id}-models`} error={fieldError('models')} help={m.modelsHelp}>
            {(attrs) => <input {...attrs} value={typed} placeholder={kind === 'anthropic' ? 'claude-sonnet-5-5, claude-haiku-5-5' : 'llama3.1, qwen3'}
              onChange={(event) => { setTyped(event.target.value); if (error?.field === 'models') setError(undefined); }} />}</Field>
        : <ModelPicker id={`${id}-models`} list={list} picked={picked} onPick={setPicked} error={fieldError('models')} canLoad={canLoad}
            needsAddress={service === 'self_hosted'} onLoad={() => void loadModels(true)} />}
      {list.state === 'typing'
        ? <button type="button" className="link-button ai-models-switch" disabled={!canLoad} onClick={() => void loadModels(true)}>{m.modelsPickInstead}</button>
        : <button type="button" className="link-button ai-models-switch" onClick={() => { setError(undefined); setTyped(picked.join(', ')); setList({ state: 'typing' }); }}>{m.modelsTypeInstead}</button>}
    </Dialog>
  );
}

// modelMatches finds a model whatever the punctuation, spacing, case or word order typed:
// "gpt5 mini", "GPT-5-mini" and "mini gpt5" all find gpt-5-mini; "sonnet 4.6" finds claude-sonnet-4-6.
function modelMatches(model: string, query: string): boolean {
  const compact = (text: string) => text.toLowerCase().replace(/[^\p{L}\p{N}]+/gu, '');
  const name = compact(model);
  const words = query.toLowerCase().split(/[^\p{L}\p{N}]+/u).filter(Boolean);
  return name.includes(compact(query)) || words.every((word) => name.includes(word));
}

// ModelPicker lists the models the provider offers this key, to tick instead of typing ids. The picked
// ones stay on top in the order picked; the first is the default.
function ModelPicker({ id, list, picked, onPick, error, canLoad, needsAddress, onLoad }: { id: string; list: ModelList; picked: string[]; onPick: (models: string[]) => void;
  error?: string; canLoad: boolean; needsAddress: boolean; onLoad: () => void }) {
  const [query, setQuery] = useState('');
  const note = `${id}-note`;
  const models = list.state === 'loaded' ? list.models : [];
  const shown = models.filter((model) => modelMatches(model, query));
  const full = picked.length >= maxProviderModels;
  const toggle = (model: string) => onPick(picked.includes(model) ? picked.filter((item) => item !== model) : full ? picked : [...picked, model]);
  return (
    <fieldset className={`field ai-models${error ? ' field-bad' : ''}`} aria-describedby={note}>
      <legend className="field-label">{m.modelsField}</legend>
      <div className="ai-models-head">
        {list.state === 'loaded' && models.length > modelSearchFrom
          ? <input id={id} type="search" className="ai-models-search" aria-label={m.modelsSearch} placeholder={m.modelsSearch} value={query} onChange={(event) => setQuery(event.target.value)} />
          : <span className="help">{list.state === 'loaded' ? '' : needsAddress ? m.modelsBeforeLoadAddress : m.modelsBeforeLoad}</span>}
        <button type="button" id={list.state === 'loaded' && models.length > modelSearchFrom ? undefined : id} disabled={!canLoad} onClick={onLoad}>
          {list.state === 'loading' ? m.loadingModels : list.state === 'loaded' ? m.reloadModels : m.loadModels}</button>
      </div>
      {list.state === 'loaded' && <ul className="ai-models-list">
        {models.length === 0 && <li className="ai-models-empty">{m.modelsNone}</li>}
        {models.length > 0 && shown.length === 0 && <li className="ai-models-empty">{m.modelsNoMatch}</li>}
        {/* The picked models first, in the order picked, then the rest as the provider lists them. */}
        {[...picked.filter((model) => shown.includes(model)), ...shown.filter((model) => !picked.includes(model))].map((model) => (
          <li key={model}><label>
            <input type="checkbox" checked={picked.includes(model)} disabled={!picked.includes(model) && full} onChange={() => toggle(model)} />
            <span className="ai-mono" title={model}>{model}</span>{picked[0] === model && <span className="ai-models-default">{m.modelsDefault}</span>}
          </label></li>
        ))}
      </ul>}
      {error ? <span className="error-copy field-note" id={note} role="alert">{error}</span>
        : <span className="help field-note" id={note}>{full ? m.modelsLimit(maxProviderModels) : list.state === 'loaded' ? m.modelsPicked(picked.length, maxProviderModels) : ''}</span>}
    </fieldset>
  );
}

function KeyDialog({ provider, onClose, onSetKey }: { provider: AIProvider; onClose: () => void; onSetKey: AIPageProps['onSetKey'] }) {
  const [key, setKey] = useState('');
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);
  const id = useId();
  const submit = async () => {
    if (!key.trim()) { setError(m.keyMissing); return; }
    setBusy(true);
    const refused = await onSetKey(provider.id, key.trim());
    setBusy(false);
    if (refused) setError(provider.key_hint ? `${refused.text} ${m.oldKeyStays(provider.key_hint)}` : refused.text); else onClose();
  };
  return (
    <Dialog title={m.rotateHeading(provider.label)} help={m.rotateHelp} onClose={onClose} onSubmit={() => void submit()}
      footer={<><button type="button" onClick={onClose}>{m.cancel}</button><button type="submit" className="primary-action" disabled={busy}>{busy ? m.checking : m.checkAndReplace}</button></>}>
      {provider.key_hint && <p className="ai-current"><KeyIcon />{m.currentKey} <span className="ai-mono">••••{provider.key_hint}</span> ·
        {provider.check_status === 'ok' ? <span className="status ai-st-ok">{m.working}</span> : <span className="status ai-st-err">{m.notWorking}</span>}</p>}
      {provider.base_url && <p className="ai-address"><span>{m.addressStays(provider.base_url)}</span></p>}
      <Field label={m.newKey} id={`${id}-key`} error={error}>{(attrs) => <input {...attrs} type="password" autoComplete="off" value={key} onChange={(event) => setKey(event.target.value)} autoFocus />}</Field>
    </Dialog>
  );
}

function DeleteDialog({ provider, settings, onClose, onRemove }: { provider: AIProvider; settings: AISettings; onClose: () => void; onRemove: AIPageProps['onRemove'] }) {
  const automatic = settings.slots.some((slot) => slot.automatic && provider.used_by.includes(slot.slot));
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    setBusy(true);
    const refused = await onRemove(provider.id);
    setBusy(false);
    if (refused) setError(refused.text); else onClose();
  };
  return (
    <Dialog alert title={m.deleteHeading(provider.label)} onClose={onClose} onSubmit={() => void submit()}
      footer={<><button type="button" onClick={onClose} autoFocus>{m.cancel}</button><button type="submit" className="ai-danger-solid" disabled={busy}>{m.deleteConfirm}</button></>}>
      {error && <p role="alert" className="error-copy">{error}</p>}
      {provider.used_by.length > 0 ? <><p>{m.deleteUsed}</p><ul className="ai-delete-list">{provider.used_by.map((slot) => <li key={slot}><b>{slotLabel(slot)}</b></li>)}</ul></>
        : <p>{m.deleteUnused}</p>}
      <p>{m.deleteBody}</p>
      {automatic && <p>{m.deleteAutomatic}</p>}
    </Dialog>
  );
}
