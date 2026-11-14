import type { AISettings, AISlot, AISlotInput, AISlotName } from '../../api/ai';
import { Sheet } from '../../ui/sheet';
import { Tooltip } from '../../ui/tooltip';
import { aiMessages as m, slotLabel } from './ai-messages';
import { InfoIcon } from './ai-icons';
import { slotChange, slotChoices } from './ai-slot-choice';
import { slotInput } from './ai-slot-input';
import { AutoDraft, AutoTriage, ChannelsThatDiffer, IntakeDefault, IntakeSources } from './ai-feature-levels';

export interface SheetActions {
  onSetSlot: (slot: AISlotName, input: AISlotInput) => void;
  onOpenAutomatic: () => void;
  onConfirmAuto: (slot: AISlot) => void;
  onSetAuto: (slot: AISlotName, input: AISlotInput) => void;
  onSourceConsent?: (source: string, on: boolean) => Promise<unknown>;
}

// FeatureSheet holds everything a feature has beyond its switch: where it runs (provider and model), what it
// does by itself and how often, its default for channels, and the channels that differ.
export function FeatureSheet({ slot, settings, isAdmin, busy, onClose, onSetSlot, onOpenAutomatic, onConfirmAuto, onSetAuto, onSourceConsent }: SheetActions
  & { slot: AISlot; settings: AISettings; isAdmin: boolean; busy: boolean; onClose: () => void }) {
  const name = slotLabel(slot.slot);
  const { chosen, choices, choice, models, serverModels, serverOff } = slotChoices(slot, settings);
  const locked = !isAdmin || busy;
  const save = (next: { source?: string; model?: string }) => onSetSlot(slot.slot, slotChange(slot, chosen, next));
  const limits = slot.slot === 'triage' ? m.automaticRuns(settings.automatic.per_day, settings.automatic.per_minute)
    : slot.slot === 'intake' ? m.intakeLimits(settings.automatic.intake_per_day) : slot.automatic ? m.draftLimits(settings.automatic.draft_per_day) : undefined;
  return (
    <Sheet title={name} closeLabel={m.close} onClose={onClose}
      footer={<><span>{m.sheetSaves}</span><button type="button" onClick={onClose}>{m.sheetDone}</button></>}>
      <p className="ai-sheet-intro">{m.slotHelp[slot.slot]}</p>
      <label className="field"><span className="field-label">{m.columnProvider}</span>
        {serverOff
          // A disabled select gets no pointer events: the tooltip hangs on its wrapper, which takes focus then.
          ? <Tooltip text={m.serverOffTip(serverOff.label)}>
            <span className="ai-tip-anchor" tabIndex={locked ? 0 : undefined}>
              <select aria-label={m.providerFor(name)} className="ai-select-off" value="server" disabled={locked} onChange={(event) => save({ source: event.target.value })}>
                <option value="server" disabled>{serverOff.label}</option>
                {choices.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}
              </select></span></Tooltip>
          : <select aria-label={m.providerFor(name)} value={chosen} disabled={locked || choices.length === 0} onChange={(event) => save({ source: event.target.value })}>
            {choices.length === 0 && <option value="">{m.noProvider}</option>}
            {choices.length > 0 && !choice && <option value="">{m.pickProvider}</option>}
            {choices.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}
          </select>}</label>
      {chosen === 'server' && serverOff
        ? <div className="field"><span className="field-label">{m.columnModel}</span><span className="ai-off-text">—</span></div>
        : chosen === 'server'
          ? <div className="field"><span className="field-label">{m.columnModel}</span>
            <span><span className="ai-model-name">{serverModels[0]}</span> <span className="muted-copy">{m.modelByServer}</span></span></div>
          : <label className="field"><span className="field-label">{m.columnModel}</span>
            <select aria-label={m.modelFor(name)} value={slot.model ?? ''} disabled={locked || !choice} onChange={(event) => save({ model: event.target.value })}>
              {models.length === 0 && <option value="">—</option>}
              {models.map((model) => <option key={model} value={model}>{model}</option>)}
            </select></label>}
      <div className="field"><span className="field-label">{m.columnAutomatic}</span>
        {limits ? <span className="ai-limits"><span>{limits}</span>
          {isAdmin && <button type="button" className="link-button" onClick={onOpenAutomatic} aria-label={m.changeAutomatic}>{m.change}</button>}</span>
          : <span className="muted-copy">{slot.slot === 'chat' ? m.chatRuns : m.noAutomatic}</span>}
      </div>
      {slot.slot === 'draft' && <AutoDraft slot={slot} settings={settings} isAdmin={isAdmin} locked={locked} hasProvider={choices.length > 0}
        onTurnOn={() => onConfirmAuto(slot)} onOff={() => onSetAuto('draft', { ...slotInput(slot), default_level: 'off' })} onRaise={onOpenAutomatic} />}
      {slot.slot === 'triage' && <AutoTriage slot={slot} settings={settings} isAdmin={isAdmin} locked={locked} hasProvider={choices.length > 0}
        onTurnOn={() => onConfirmAuto(slot)} onSet={(input) => onSetAuto('triage', { ...slotInput(slot), ...input })} />}
      {slot.slot === 'intake' && <IntakeDefault slot={slot} locked={locked} hasProvider={choices.length > 0}
        onSet={(level) => onSetAuto('intake', { ...slotInput(slot), default_level: level })} />}
      {slot.slot === 'intake' && <IntakeSources settings={settings} isAdmin={isAdmin} onSourceConsent={onSourceConsent} />}
      {/* The help center's chat has no level per channel. */}
      {slot.slot !== 'chat' && <ChannelsThatDiffer slot={slot} settings={settings} />}
      <div className="panel ai-sends"><InfoIcon /><span>{m.sends} <b>{m.sendsStrong}</b></span></div>
    </Sheet>
  );
}
