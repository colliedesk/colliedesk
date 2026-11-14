import { useRef, useState } from 'react';
import type { AISettings, AISlot, AISlotInput, AISlotName } from '../../api/ai';
import { formatDate, formatDay } from '../../lib/format-date';
import { selfHostGuide } from '../../lib/self-host-guide';
import { HelpTip } from '../../ui/help-tip';
import { aiMessages as m } from './ai-messages';
import { InfoIcon, PlusIcon, ServerIcon } from './ai-icons';
import { Meter } from './ai-meter';
import { AutoMovedNotice } from './ai-feature-levels';
import { FeatureRow } from './ai-feature-row';
import { IntakeConsent } from './ai-intake-consent';
import { slotChange, slotChoices } from './ai-slot-choice';
import { useInstantSave } from './settings-edits';
import { FeatureSheet } from './ai-feature-sheet';
import type { SheetActions } from './ai-feature-sheet';
import type { AIPageProps } from './ai-page-props';

// Features is the first tab: what the workspace's AI does, one row per feature, and the notices that need an
// answer (no provider, a paused or used-up allowance, the one-time consent).
export function Features(props: AIPageProps & SheetActions & { settings: AISettings; onOpenAdd: () => void }) {
  const { settings, isAdmin } = props;
  const [open, setOpen] = useState<AISlotName>();
  const [asking, setAsking] = useState<AISlotName>();
  const instant = useInstantSave();
  const server = settings.server;
  const nothing = !server && settings.providers.length === 0;
  const models = (slot: AISlotName) => server?.models[slot]?.[0] ?? '';
  const shown = settings.slots.filter((slot) => slot.slot !== 'intake' || props.chatChannels);
  const opened = shown.find((slot) => slot.slot === open);
  const consenting = shown.find((slot) => slot.slot === asking);
  // The feature turns on only once the person agrees; the switch then says Saved, and Undo turns only it off again, built from
  // the feature as it is then.
  const latest = useRef(settings);
  latest.current = settings;
  const agree = (slot: AISlot) => {
    setAsking(undefined);
    const now = (name: AISlotName) => latest.current.slots.find((item) => item.slot === name) ?? slot;
    const input = (value: boolean) => slotChange(now(slot.slot), slotChoices(now(slot.slot), latest.current).chosen, { enabled: value });
    void instant((value: boolean) => props.onSetSlot(slot.slot, input(value)), true, slot.enabled);
  };
  // The sheet's own choices (provider, model) show a refusal in the page's notice; nothing here waits for them.
  const setInSheet = (slot: AISlotName, input: AISlotInput) => { void Promise.resolve(props.onSetSlot(slot, input)).catch(() => undefined); };
  return <div className="ai-panel-stack">
    {!isAdmin && <div className="notice notice-plain" role="note"><InfoIcon /><span><strong>{m.agentNotice}</strong> {m.agentNoticeMore}</span></div>}
    {isAdmin && <AutoMovedNotice settings={settings} slotName="triage" onTurnOn={props.onConfirmAuto} />}
    {isAdmin && <AutoMovedNotice settings={settings} slotName="draft" onTurnOn={props.onConfirmAuto} />}
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
    {nothing && <div className="notice" role="status">
      <span><strong>{m.noProviderYet}</strong> {m.noProviderBody} <HelpTip label={m.noProviderYet} text={m.noProviderWhy} /></span>
      <div className="notice-actions">
        {isAdmin && <button type="button" className="primary-action ai-add-inline" onClick={props.onOpenAdd}><PlusIcon />{m.addProvider}</button>}
        <a className="button" href={selfHostGuide('environment-variables.md', 'ai-provider')} target="_blank" rel="noopener noreferrer">{m.setupGuide}</a>
      </div>
    </div>}
    <ul className="panel ai-rows" aria-label={m.features}>
      {shown.map((slot) => <FeatureRow key={slot.slot} slot={slot} settings={settings} isAdmin={isAdmin} busySlot={props.busySlot} onSetSlot={props.onSetSlot} onOpen={setOpen} onAskToTurnOn={setAsking} />)}
    </ul>
    <p className="ai-rows-foot">{m.rowsFoot}</p>
    {opened && <FeatureSheet slot={opened} settings={settings} isAdmin={isAdmin} busy={props.busySlot === opened.slot} onClose={() => setOpen(undefined)}
      onSetSlot={setInSheet} onOpenAutomatic={props.onOpenAutomatic} onConfirmAuto={props.onConfirmAuto} onSetAuto={props.onSetAuto} onSourceConsent={props.onSourceConsent} />}
    {consenting && <IntakeConsent slot={consenting} settings={settings} onClose={() => setAsking(undefined)} onAgree={() => agree(consenting)} />}
  </div>;
}
