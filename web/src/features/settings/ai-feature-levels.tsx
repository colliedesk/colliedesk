import { useId, useState } from 'react';
import type { AILevel, AISettings, AISlot, AISlotInput, AISlotName } from '../../api/ai';
import { Choice } from '../../ui/choice';
import { HelpTip } from '../../ui/help-tip';
import { formatDate, formatDay } from '../../lib/format-date';
import { aiMessages as m, slotLabel } from './ai-messages';
import { featureName, levelMeaning, levelName, levelsOf, scopeName } from './ai-levels';
import { money, parts } from './ai-level-dialogs';
import type { Part } from './ai-level-dialogs';
import { slotInput } from './ai-slot-input';
import { useInstantSave } from './settings-edits';

// levelItems are the levels a feature takes, each named for what it does.
const levelItems = (slot: AISlotName) => levelsOf(slot).map((level) => ({ value: level, label: levelName[level] }));

// AutoTriage is what triage does by itself where no channel says otherwise: off, suggest for each new
// conversation, or apply by itself (a dialog says what it does first), with the parts it applies and how
// it went in 30 days.
export function AutoTriage({ slot, settings, isAdmin, locked, hasProvider, onTurnOn, onSet }: { slot: AISlot; settings: AISettings; isAdmin: boolean; locked: boolean; hasProvider: boolean;
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
export function IntakeDefault({ slot, locked, hasProvider, onSet }: { slot: AISlot; locked: boolean; hasProvider: boolean; onSet: (level: AILevel) => void }) {
  const level: AILevel = slot.enabled && slot.default_level !== 'auto' ? slot.default_level : 'off';
  const id = useId();
  const why = !hasProvider ? m.autoNeedsProvider : !slot.enabled ? m.intakeNeedsFeature : undefined;
  return (
    <div className="ai-auto">
      <div className="ai-auto-lead"><strong id={id}>{m.levelHeading}</strong><HelpTip label={m.levelHeading} text={m.intakeChatOnly} /></div>
      {why ? <p className="ai-auto-desc muted-copy" id={`${id}-why`}>{why}</p> : <p className="ai-auto-desc">{levelMeaning.intake[level]}.</p>}
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
export function IntakeSources({ settings, isAdmin, onSourceConsent }: { settings: AISettings; isAdmin: boolean; onSourceConsent?: (source: string, on: boolean) => Promise<unknown> }) {
  const given = new Set(settings.source_consents.map((consent) => consent.source_class));
  const [busy, setBusy] = useState<string>();
  const [problem, setProblem] = useState<string>();
  const instant = useInstantSave();
  // Giving or taking back a consent saves at once and says so; Undo does the opposite in one request. A refusal shows under the switches.
  const give = async (source: string, on: boolean) => {
    if (!onSourceConsent) return;
    setBusy(source);
    setProblem(undefined);
    try {
      await onSourceConsent(source, on);
    } catch (error) {
      setProblem(m.actionFailed);
      throw error;
    } finally {
      setBusy(undefined);
    }
  };
  const toggle = (source: string, on: boolean) => instant((value: boolean) => give(source, value), on, !on);
  return <fieldset className="ai-sources-consent">
    <legend>{m.intakeSourcesLegend}</legend>
    <span className="ai-sources-tip"><HelpTip label={m.intakeSourcesLegend} text={m.intakeSourcesHelp} /></span>
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
export function AutoDraft({ slot, settings, isAdmin, locked, hasProvider, onTurnOn, onOff, onRaise }: { slot: AISlot; settings: AISettings; isAdmin: boolean; locked: boolean;
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
export function ChannelsThatDiffer({ slot, settings }: { slot: AISlot; settings: AISettings }) {
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
export function AutoMovedNotice({ settings, slotName, onTurnOn }: { settings: AISettings; slotName: AISlotName; onTurnOn: (slot: AISlot) => void }) {
  const slot = settings.slots.find((item) => item.slot === slotName);
  const last = settings.events.find((event) => event.kind === 'slot_changed' && event.slot === slotName);
  const detail = last?.detail as { from?: Record<string, unknown>; to?: Record<string, unknown> } | undefined;
  if (!slot || !slot.enabled || slot.automatic || !detail?.from?.automatic || detail.to?.automatic || detail.from.provider_label === detail.to?.provider_label) return null;
  const title = slotName === 'draft' ? m.autoDraftTitle : m.autoTitle;
  return <div className="notice" role="status"><span>{m.autoMovedNotice(title, slotLabel(slotName), String(detail.to?.provider_label ?? ''))}</span>
    <button type="button" onClick={() => onTurnOn(slot)}>{m.autoTurnOnAgain}</button></div>;
}
