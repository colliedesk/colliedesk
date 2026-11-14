import { useId } from 'react';
import type { AISettings, AISlot, AISlotInput, AISlotName } from '../../api/ai';
import { LockedReason } from '../../ui/help-tip';
import { aiMessages as m, slotLabel } from './ai-messages';
import { levelName } from './ai-levels';
import { slotChange, slotChoices } from './ai-slot-choice';

// stateOf says what a feature is doing, in the words of its row: the level it runs at where it has one.
function stateOf(slot: AISlot, settings: AISettings): { text: string; tone: 'ok' | 'err' | 'warn' | 'off' } {
  if (slot.enabled && !slot.active) return { text: m.statusStopped, tone: 'err' };
  const allowanceUsed = slot.source === 'server' && settings.allowance !== null && settings.allowance.used_micro_usd >= settings.allowance.limit_micro_usd;
  if (slot.enabled && allowanceUsed) return { text: m.statusAllowanceUsed, tone: 'err' };
  if (slot.enabled && slot.slot === 'triage' && settings.automatic.paused_until !== null) return { text: m.statusPaused, tone: 'warn' };
  if (!slot.enabled) return { text: m.statusOff, tone: 'off' };
  const level = slot.slot === 'chat' || slot.default_level === 'off' ? undefined : levelName[slot.default_level];
  return { text: level ? m.stateOnLevel(level) : m.statusOn, tone: 'ok' };
}

// FeatureRow is one feature: its name, one short line, its state and its switch. The switch saves at once; the rest
// of the row opens the feature's sheet (the row's button covers it, the switch and its tip sit above that button).
export function FeatureRow({ slot, settings, isAdmin, busySlot, onSetSlot, onOpen }: { slot: AISlot; settings: AISettings; isAdmin: boolean; busySlot?: AISlotName;
  onSetSlot: (slot: AISlotName, input: AISlotInput) => void; onOpen: (slot: AISlotName) => void }) {
  const name = slotLabel(slot.slot);
  const line = useId();
  const { chosen, choice, choices } = slotChoices(slot, settings);
  const saving = busySlot === slot.slot;
  const state = saving ? undefined : stateOf(slot, settings);
  // A switch is off for one of three reasons, and each says which: not an admin, no provider at all, or none picked for this feature.
  const why = !isAdmin ? m.autoAdminsOnly : choices.length === 0 ? m.addFirst : !choice ? m.pickProviderFirst : undefined;
  const whyID = `${line}-why`;
  return (
    <li className="ai-row">
      <button type="button" className="ai-row-open" aria-label={m.openFeature(name)} aria-haspopup="dialog" aria-describedby={line} onClick={() => onOpen(slot.slot)} />
      <div className="ai-row-text">
        <strong>{name}</strong>
        <span className="ai-row-line" id={line}>{m.slotShort[slot.slot]}</span>
        {!saving && slot.enabled && !slot.active && slot.off_reason && <span className="ai-row-why error-copy">{m.offReason[slot.off_reason]}</span>}
      </div>
      {state ? <span className={`status ai-state ai-st-${state.tone}`}>{state.text}</span> : <span className="ai-state" role="status">{m.saving}</span>}
      <span className="ai-row-switch">
        {why && <LockedReason label={name} text={why} id={whyID} />}
        <label className="switch"><input type="checkbox" role="switch" aria-label={m.switchFor(name)} aria-describedby={why ? whyID : undefined} checked={slot.enabled} disabled={!isAdmin || saving || !choice}
          onChange={(event) => onSetSlot(slot.slot, slotChange(slot, chosen, { enabled: event.target.checked }))} /></label>
      </span>
    </li>
  );
}
