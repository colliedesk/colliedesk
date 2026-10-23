import { useId, useRef, useState } from 'react';
import type { FormEvent, ReactNode } from 'react';
import type { AIEvent, AIProvider, AIProviderInput, AISettings, AISlot, AISlotInput, AISlotName, AIUsage } from '../../api/ai';
import { formatDate, formatDay } from '../../lib/format-date';
import { dialogKeys, keepDialogFocus, useReturnFocus } from '../../ui/dialog-keys';
import { aiMessages as m } from './ai-messages';
import { SettingsNav } from './settings-nav';
import type { Shown } from './settings-nav';
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

export type AITab = 'features' | 'providers' | 'usage' | 'history';
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
  onSetKey: (id: string, key: string) => Promise<FieldError | undefined>;
  onRemove: (id: string) => Promise<FieldError | undefined>;
  onSetAutomatic: (perDay: number, perMinute: number) => Promise<FieldError | undefined>;
  // The usage of the month shown in the Usage tab, asked for when the tab opens or the month changes.
  usage?: AIUsage;
  usageFailed?: boolean;
  onUsageMonth: (month: string) => void;
}

// money writes micro-dollars as dollars and cents.
export const money = (micro: number) => `$${(micro / 1e6).toFixed(2)}`;
const tokens = (n: number) => (n >= 1e6 ? `${(n / 1e6).toFixed(2)}M` : n >= 1e3 ? `${(n / 1e3).toFixed(1)}k` : String(n));

// recentMonths lists this UTC month and the five before it, as YYYY-MM.
export function recentMonths(now: Date = new Date()): string[] {
  return Array.from({ length: 6 }, (_, back) => {
    const at = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth() - back, 1));
    return `${at.getUTCFullYear()}-${String(at.getUTCMonth() + 1).padStart(2, '0')}`;
  });
}

const slotLabel = (slot: string) => m.slotName[slot as AISlotName] ?? slot;

export function AIPage(props: AIPageProps) {
  const { settings, isAdmin } = props;
  const [tab, setTab] = useState<AITab>('features');
  const [dialog, setDialog] = useState<{ kind: 'add' } | { kind: 'automatic' } | { kind: 'auto-triage'; slot: AISlot } | { kind: 'key' | 'delete'; provider: AIProvider }>();
  const ids = { tabs: useId() };
  const full = settings ? settings.providers.length >= settings.provider_limit : false;
  const tabs: { id: AITab; label: string; count?: number }[] = [
    { id: 'features', label: m.tabFeatures }, { id: 'providers', label: m.tabProviders, count: settings ? settings.providers.length + (settings.server ? 1 : 0) : undefined },
    ...(isAdmin ? [{ id: 'usage' as const, label: m.tabUsage }] : []), { id: 'history', label: m.tabHistory },
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
              <div className="ai-tabs" role="tablist" aria-label={m.tabs}>
                {tabs.map((item) => <button key={item.id} type="button" role="tab" id={`${ids.tabs}-${item.id}`} aria-selected={tab === item.id}
                  aria-controls={`${ids.tabs}-${item.id}-panel`} tabIndex={tab === item.id ? 0 : -1} onClick={() => show(item.id)}
                  onKeyDown={(event) => {
                    if (event.key !== 'ArrowRight' && event.key !== 'ArrowLeft') return;
                    const next = tabs[(tabs.findIndex((t) => t.id === tab) + (event.key === 'ArrowRight' ? 1 : tabs.length - 1)) % tabs.length];
                    show(next.id);
                    document.getElementById(`${ids.tabs}-${next.id}`)?.focus();
                  }}>
                  {item.label}{item.count !== undefined && <span className="ai-count">{item.count}</span>}
                </button>)}
              </div>
              {isAdmin && <button type="button" className="ai-add" aria-label={m.addProvider} onClick={openAdd} disabled={full}
                aria-describedby={full ? 'ai-limit-why' : undefined}><PlusIcon /><span className="ai-wide">{m.addProvider}</span><span className="ai-narrow">{m.addShort}</span></button>}
            </div>
            {props.error && <p role="alert" className="error-copy">{props.error}</p>}
            <div role="tabpanel" id={`${ids.tabs}-${tab}-panel`} aria-labelledby={`${ids.tabs}-${tab}`} className="ai-panel-stack">
              {tab === 'features' && <Features {...props} settings={settings} onOpenAdd={openAdd} onOpenAutomatic={() => setDialog({ kind: 'automatic' })}
                onConfirmAuto={(slot) => setDialog({ kind: 'auto-triage', slot })} />}
              {tab === 'usage' && <Usage usage={props.usage} failed={props.usageFailed} month={month} onMonth={(next) => { setMonth(next); props.onUsageMonth(next); }} />}
              {tab === 'providers' && <Providers {...props} settings={settings}
                onKey={(provider) => setDialog({ kind: 'key', provider })} onDelete={(provider) => setDialog({ kind: 'delete', provider })} />}
              {tab === 'history' && <History events={settings.events} timezone={props.timezone} />}
            </div>
          </>}
        </div>
      </div>
      {dialog?.kind === 'add' && settings && <AddDialog settings={settings} onClose={() => setDialog(undefined)} onAdd={props.onAdd} />}
      {dialog?.kind === 'key' && <KeyDialog provider={dialog.provider} onClose={() => setDialog(undefined)} onSetKey={props.onSetKey} />}
      {dialog?.kind === 'automatic' && settings && <AutomaticDialog settings={settings} onClose={() => setDialog(undefined)} onSave={props.onSetAutomatic} />}
      {dialog?.kind === 'delete' && settings && <DeleteDialog provider={dialog.provider} settings={settings} onClose={() => setDialog(undefined)} onRemove={props.onRemove} />}
      {dialog?.kind === 'auto-triage' && <AutoTriageDialog slot={dialog.slot} onClose={() => setDialog(undefined)}
        onTurnOn={() => { setDialog(undefined); props.onSetSlot('triage', { ...slotInput(dialog.slot), automatic: true }); }} />}
    </main>
  );
}

type FeatureActions = { onOpenAutomatic: () => void; onConfirmAuto: (slot: AISlot) => void };

function Features(props: AIPageProps & FeatureActions & { settings: AISettings; onOpenAdd: () => void }) {
  const { settings, isAdmin } = props;
  const server = settings.server;
  const nothing = !server && settings.providers.length === 0;
  const models = (slot: AISlotName) => server?.models[slot]?.[0] ?? '';
  return <div className={`ai-panel-stack${nothing ? ' ai-nothing' : ''}`}>
    {!isAdmin && <div className="ai-notice ai-notice-plain" role="note"><InfoIcon /><span><strong>{m.agentNotice}</strong> {m.agentNoticeMore}</span></div>}
    {props.isAdmin && <AutoMovedNotice settings={settings} onTurnOn={props.onConfirmAuto} />}
    {settings.automatic.paused_until && settings.slots.some((slot) => slot.slot === 'triage' && slot.enabled) && <div className="ai-notice" role="status">
      <span><strong>{m.pausedHeading(formatDate(settings.automatic.paused_until, props.timezone))}</strong> {m.pausedBody(settings.automatic.per_day)}</span>
      {isAdmin && <div className="ai-notice-actions"><button type="button" onClick={props.onOpenAutomatic}>{m.raiseLimit}</button></div>}
    </div>}
    {settings.allowance && settings.allowance.used_micro_usd >= settings.allowance.limit_micro_usd && settings.slots.some((slot) => slot.source === 'server' && slot.enabled)
      && <div className="ai-notice ai-notice-danger" role="alert">
        <span><strong>{m.allowanceUsedHeading(server?.label ?? '')}</strong> {m.allowanceUsedBody(formatDay(settings.allowance.resets_at, 'UTC'))}</span>
        {isAdmin && <div className="ai-notice-actions"><button type="button" className="primary-action" onClick={props.onOpenAdd}>{m.addProvider}</button></div>}
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
        <pre>{'AI_SERVER_PROVIDER=anthropic\nAI_SERVER_API_KEY=…\nAI_SERVER_MODELS=triage=…,draft=…'}</pre>
        <div className="button-row"><a className="button" href="https://github.com/colliedesk/colliedesk/blob/main/docs/self-host/environment-variables.md#ai-provider" target="_blank" rel="noopener noreferrer">{m.setupGuide}</a></div></div>
    </section>}
    <section className="panel ai-slots" aria-label={m.features}>
      <div className="ai-slot-head" aria-hidden="true"><span>{m.columnFeature}</span><span>{m.columnProvider}</span><span>{m.columnModel}</span><span>{m.columnAutomatic}</span><span>{m.columnOn}</span></div>
      {settings.slots.map((slot) => <SlotRow key={slot.slot} slot={slot} {...props} />)}
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

type Part = AISlot['auto_parts'][number];
const parts: Part[] = ['tags', 'priority', 'assignee'];
const partList = (list: Part[]) => list.map((part) => m.autoPartLower[part]).join(' and ').replace(/ and (?=.* and )/g, ', ');

function SlotRow({ slot, settings, isAdmin, busySlot, onSetSlot, onOpenAutomatic, onConfirmAuto }: AIPageProps & FeatureActions & { slot: AISlot; settings: AISettings }) {
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
        {slot.slot === 'triage' ? <><span>{m.automaticRuns(settings.automatic.per_day, settings.automatic.per_minute)}</span>
          {isAdmin && <button type="button" className="link-button" onClick={onOpenAutomatic} aria-label={m.changeAutomatic}>{m.change}</button>}</>
          : <span className="muted-copy">{m.noAutomatic}</span>}
      </div>
      <label className="ai-switch"><input type="checkbox" role="switch" aria-label={m.switchFor(name)} checked={slot.enabled} disabled={locked || !choice}
        onChange={(event) => save({ enabled: event.target.checked })} /></label>
      {saving && <p className="ai-slot-why" role="status">{m.saving}</p>}
      {!saving && choices.length === 0 && <p className="ai-slot-why help">{m.addFirst}</p>}
      {!saving && stopped && slot.off_reason && <p className="ai-slot-why error-copy">{m.offReason[slot.off_reason]}</p>}
      {slot.slot === 'triage' && <AutoTriage slot={slot} settings={settings} isAdmin={isAdmin} locked={locked} hasProvider={choices.length > 0}
        onTurnOn={() => onConfirmAuto(slot)} onSet={(input) => onSetSlot('triage', { ...slotInput(slot), ...input })} />}
    </div>
  );
}

// AutoTriage is the automatic mode of triage suggestions: off until an admin turns it on (a dialog says
// what it does), the parts it applies, and how it went in 30 days.
function AutoTriage({ slot, settings, isAdmin, locked, hasProvider, onTurnOn, onSet }: { slot: AISlot; settings: AISettings; isAdmin: boolean; locked: boolean; hasProvider: boolean;
  onTurnOn: () => void; onSet: (input: Partial<AISlotInput>) => void }) {
  const on = slot.enabled && slot.automatic;
  const id = useId();
  const why = !hasProvider ? m.autoNeedsProvider : !slot.enabled ? m.autoNeedsFeature : undefined;
  const chosen = slot.auto_parts.length > 0 ? slot.auto_parts : (['tags', 'priority'] as Part[]);
  const toggle = (part: Part) => {
    const next = chosen.includes(part) ? chosen.filter((item) => item !== part) : [...chosen, part];
    if (next.length > 0) onSet({ auto_parts: parts.filter((item) => next.includes(item)) });
  };
  return (
    <div className="ai-auto">
      <div className="ai-auto-lead"><strong id={id}>{m.autoTitle}</strong>
        {on ? <span className="status ai-st-ok">{m.statusOn}</span> : <span className="status ai-st-off">{m.statusOff}</span>}</div>
      {why ? <p className="ai-auto-desc muted-copy">{why}</p>
        : <p className="ai-auto-desc"><span className="ai-long">{m.autoDesc} </span><b>{m.autoDescStrong}</b>{m.autoDescRest}</p>}
      {!why && <fieldset className="ai-auto-parts"><legend className="sr-only">{m.autoParts}</legend>
        {parts.map((part) => <label key={part} className="ai-pick"><input type="checkbox" checked={chosen.includes(part)}
          disabled={!on || locked || (chosen.length === 1 && chosen.includes(part))} title={chosen.length === 1 && chosen.includes(part) ? m.autoLastPart : undefined}
          onChange={() => toggle(part)} />{m.autoPart[part]}</label>)}
        {on ? <span className="ai-auto-stat">{m.autoStat(settings.auto_triage.applied, settings.auto_triage.undone)} <span className="muted-copy">{m.autoStatWhen}</span></span>
          : isAdmin && <span className="ai-auto-stat muted-copy ai-long">{m.autoPickLater}</span>}
      </fieldset>}
      <label className="ai-switch ai-auto-switch"><input type="checkbox" role="switch" aria-labelledby={id} checked={on} disabled={locked || !slot.enabled}
        onChange={(event) => (event.target.checked ? onTurnOn() : onSet({ automatic: false }))} /></label>
    </div>
  );
}

// AutoMovedNotice says when triage's automatic mode turned off because the feature moved to another
// provider: the admin's consent was for the provider it left.
function AutoMovedNotice({ settings, onTurnOn }: { settings: AISettings; onTurnOn: (slot: AISlot) => void }) {
  const slot = settings.slots.find((item) => item.slot === 'triage');
  const last = settings.events.find((event) => event.kind === 'slot_changed' && event.slot === 'triage');
  const detail = last?.detail as { from?: Record<string, unknown>; to?: Record<string, unknown> } | undefined;
  if (!slot || !slot.enabled || slot.automatic || !detail?.from?.automatic || detail.to?.automatic || detail.from.provider_label === detail.to?.provider_label) return null;
  return <div className="ai-notice" role="status"><span>{m.autoMovedNotice(slotLabel('triage'), String(detail.to?.provider_label ?? ''))}</span>
    <button type="button" onClick={() => onTurnOn(slot)}>{m.autoTurnOnAgain}</button></div>;
}

function AutoTriageDialog({ slot, onClose, onTurnOn }: { slot: AISlot; onClose: () => void; onTurnOn: () => void }) {
  const chosen = slot.auto_parts.length > 0 ? slot.auto_parts : (['tags', 'priority'] as Part[]);
  return (
    <Dialog title={m.autoConfirmTitle} help={m.autoConfirmLead(partList(chosen))} onClose={onClose} onSubmit={onTurnOn}
      footer={<><button type="button" className="ghost" onClick={onClose}>{m.cancel}</button><button type="submit" className="primary-action" autoFocus>{m.autoConfirmOn}</button></>}>
      <ul className="ai-facts">{m.autoConfirmFacts.map((fact) => <li key={fact}>{fact}</li>)}</ul>
    </Dialog>
  );
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
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);
  const id = useId();
  const submit = async () => {
    const day = Number(perDay);
    const minute = Number(perMinute);
    if (!Number.isInteger(day) || day < 0 || day > 10000 || !Number.isInteger(minute) || minute < 0 || minute > 1000) { setError(m.automaticInvalid); return; }
    setBusy(true);
    const refused = await onSave(day, minute);
    setBusy(false);
    if (refused) setError(refused.text); else onClose();
  };
  return (
    <Dialog title={m.automaticHeading} help={m.automaticHelp} onClose={onClose} onSubmit={() => void submit()}
      footer={<><button type="button" onClick={onClose}>{m.cancel}</button><button type="submit" className="primary-action" disabled={busy}>{m.save}</button></>}>
      {error && <p role="alert" className="error-copy">{error}</p>}
      <div className="ai-two">
        <Field label={m.perDay} id={`${id}-day`}>{(attrs) => <input {...attrs} type="number" inputMode="numeric" min={0} max={10000} value={perDay} onChange={(event) => setPerDay(event.target.value)} autoFocus />}</Field>
        <Field label={m.perMinute} id={`${id}-minute`}>{(attrs) => <input {...attrs} type="number" inputMode="numeric" min={0} max={1000} value={perMinute} onChange={(event) => setPerMinute(event.target.value)} />}</Field>
      </div>
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
  }
  if (event.kind === 'automatic_limits_changed') {
    const to = detail.to as { per_day?: number; per_minute?: number } | undefined;
    return m.eventAutomatic(to?.per_day ?? 0, to?.per_minute ?? 0);
  }
  if (detail.reason === 'provider_removed') return m.eventSlotRemoved(slot, label);
  const fromAuto = detail.from?.automatic === true;
  const toAuto = detail.to?.automatic === true;
  const autoParts = String(detail.to?.auto_parts ?? '').split(',').filter(Boolean).map((part) => m.autoPartLower[part as Part] ?? part).join(', ');
  const fromOn = detail.from?.enabled === true;
  const toOn = detail.to?.enabled === true;
  const fromLabel = String(detail.from?.provider_label ?? '');
  const toLabel = String(detail.to?.provider_label ?? '');
  if (!fromOn && toOn) return m.eventSlotOn(slot, toLabel);
  if (fromOn && !toOn) return m.eventSlotOff(slot);
  if (fromLabel && toLabel && fromLabel !== toLabel) return fromAuto && !toAuto ? m.eventAutoMoved(slot, toLabel) : m.eventSlotMoved(slot, fromLabel, toLabel);
  if (!fromAuto && toAuto) return m.eventAutoOn(autoParts);
  if (fromAuto && !toAuto) return m.eventAutoOff;
  if (toAuto && String(detail.from?.auto_parts ?? '') !== String(detail.to?.auto_parts ?? '')) return m.eventAutoParts(autoParts);
  const fromModel = String(detail.from?.model ?? '');
  const toModel = String(detail.to?.model ?? '');
  if (fromModel && toModel && fromModel !== toModel) return m.eventSlotModel(slot, fromModel, toModel);
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

// Dialog is the frame every dialog of this page shares: Escape closes, Tab stays inside, focus returns.
function Dialog({ title, help, onClose, children, footer, onSubmit, alert }: { title: string; help?: string; onClose: () => void; children: ReactNode; footer: ReactNode; onSubmit?: (event: FormEvent) => void; alert?: boolean }) {
  useReturnFocus();
  const root = useRef<HTMLFormElement>(null);
  const titleId = useId();
  return (
    <div className="dialog-backdrop" onMouseDown={keepDialogFocus}>
      <form className="dialog ai-dialog" role={alert ? 'alertdialog' : 'dialog'} aria-modal="true" aria-labelledby={titleId} ref={root}
        onKeyDown={dialogKeys(root, onClose)} onSubmit={(event) => { event.preventDefault(); onSubmit?.(event); }} noValidate>
        <header><div><h2 id={titleId}>{title}</h2>{help && <p>{help}</p>}</div>
          {!alert && <button type="button" className="icon-button" aria-label={m.close} onClick={onClose}>×</button>}</header>
        <div className="ai-dialog-body">{children}</div>
        <footer className="button-row ai-dialog-footer">{footer}</footer>
      </form>
    </div>
  );
}

function Field({ label, error, help, children, id }: { label: string; error?: string; help?: string; children: (props: { id: string; 'aria-invalid'?: true; 'aria-describedby'?: string }) => ReactNode; id: string }) {
  const note = `${id}-note`;
  return <div className={`field${error ? ' ai-field-bad' : ''}`}><label className="field-label" htmlFor={id}>{label}</label>
    {children({ id, ...(error ? { 'aria-invalid': true } : {}), ...(error || help ? { 'aria-describedby': note } : {}) })}
    {error ? <span className="error-copy ai-field-note" id={note}>{error}</span> : help && <span className="help ai-field-note" id={note}>{help}</span>}
  </div>;
}

function AddDialog({ settings, onClose, onAdd }: { settings: AISettings; onClose: () => void; onAdd: AIPageProps['onAdd'] }) {
  const [kind, setKind] = useState<'anthropic' | 'openai_compatible'>('anthropic');
  const [label, setLabel] = useState('');
  const [baseURL, setBaseURL] = useState('');
  const [key, setKey] = useState('');
  const [models, setModels] = useState('');
  const [error, setError] = useState<FieldError>();
  const [busy, setBusy] = useState(false);
  const id = useId();
  const keyOptional = kind === 'openai_compatible' && settings.allow_private_base_urls;
  const noKeys = !settings.can_store_keys && !keyOptional;
  const left = settings.provider_limit - settings.providers.length;
  const focus = (field: FieldError['field']) => document.getElementById(`${id}-${field}`)?.focus();
  const submit = async () => {
    const list = models.split(',').map((model) => model.trim()).filter(Boolean);
    const refuse = (field: FieldError['field'], text: string) => { setError({ field, text }); focus(field); };
    if (!label.trim()) return refuse('label', m.labelMissing);
    if (kind === 'openai_compatible' && !baseURL.trim()) return refuse('base_url', m.baseURLMissing);
    if (!key.trim() && !keyOptional) return refuse('api_key', m.keyMissing);
    if (list.length === 0) return refuse('models', m.modelsMissing);
    setBusy(true);
    setError(undefined);
    const input: AIProviderInput = { kind, label: label.trim(), models: list, ...(key.trim() ? { api_key: key.trim() } : {}), ...(kind === 'openai_compatible' ? { base_url: baseURL.trim() } : {}) };
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
      {noKeys && <div className="ai-notice ai-notice-danger" role="alert"><span><strong>{m.noKeysNotice}</strong> {m.noKeysBody}</span></div>}
      {error?.field === 'form' && <p role="alert" className="error-copy">{error.text}</p>}
      <fieldset className="ai-kind"><legend className="sr-only">{m.columnProvider}</legend>
        {(['anthropic', 'openai_compatible'] as const).map((value) => <label key={value}>
          <input type="radio" name={`${id}-kind`} value={value} checked={kind === value} onChange={() => { setKind(value); setError(undefined); }} />
          <strong>{value === 'anthropic' ? m.kindAnthropic : m.kindOpenAI}</strong><span>{value === 'anthropic' ? m.kindAnthropicHelp : m.kindOpenAIHelp}</span></label>)}
      </fieldset>
      <Field label={m.labelField} id={`${id}-label`} error={fieldError('label')}>{(attrs) => <input {...attrs} value={label} maxLength={80} onChange={(event) => setLabel(event.target.value)} autoFocus />}</Field>
      {kind === 'openai_compatible' && <Field label={m.baseURLField} id={`${id}-base_url`} error={fieldError('base_url')} help={m.baseURLHelp}>
        {(attrs) => <input {...attrs} type="url" inputMode="url" value={baseURL} placeholder="https://openrouter.ai/api/v1" onChange={(event) => setBaseURL(event.target.value)} />}</Field>}
      <Field label={keyOptional ? m.keyFieldOptional : m.keyField} id={`${id}-api_key`} error={fieldError('api_key')} help={m.keyHelpStored}>
        {(attrs) => <input {...attrs} type="password" autoComplete="off" value={key} placeholder={keyOptional ? m.keyOptionalPlaceholder : undefined} onChange={(event) => setKey(event.target.value)} />}</Field>
      <Field label={m.modelsField} id={`${id}-models`} error={fieldError('models')} help={m.modelsHelp}>
        {(attrs) => <input {...attrs} value={models} placeholder={kind === 'anthropic' ? 'claude-sonnet-5-5, claude-haiku-5-5' : 'openai/gpt-5'} onChange={(event) => setModels(event.target.value)} />}</Field>
    </Dialog>
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
