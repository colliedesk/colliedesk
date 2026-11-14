import type { AIEvent, AILevel } from '../../api/ai';
import { formatDate } from '../../lib/format-date';
import { aiMessages as m, slotLabel } from './ai-messages';
import { levelName, scopeName } from './ai-levels';
import { money } from './ai-level-dialogs';
import type { Part } from './ai-level-dialogs';

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

export function History({ events, timezone }: { events: AIEvent[]; timezone: string }) {
  return <>
    <section className="panel" aria-label={m.tabHistory}>
      {events.length === 0 ? <p className="ai-prov-empty muted-copy">{m.historyEmpty}</p>
        : <ul className="ai-history">{events.map((event) => <li key={event.id}><time dateTime={event.created_at}>{formatDate(event.created_at, timezone)}</time>
          <span><b>{event.actor_name ?? m.someone}</b>{event.via_token && m.viaToken} {eventText(event)}</span></li>)}</ul>}
    </section>
    <p className="help ai-help">{m.historyHelp}</p>
  </>;
}
