import { useId, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { aiApi } from '../../../api/ai';
import type { AISettings, ChannelAIInput, IntakeReadiness } from '../../../api/ai';
import { Dialog } from '../../../ui/dialog';
import { money } from '../ai-level-dialogs';
import { scopeName } from '../ai-levels';
import { channelsMessages as m } from './channels-messages';

type Action = IntakeReadiness['actions'][number];
type Options = NonNullable<ChannelAIInput['options']>;

const percent = (value: number) => `${Math.round(value * 100)}%`;

// gateText says what one action does at Auto: on its own when the labels show it is right often enough,
// otherwise a suggestion, and what is missing.
function gateText(action: Action) {
  const name = m.intakeAction[action.action];
  if (action.gate === 'open') return m.intakeGateReady(name, action.correct, action.labels);
  if (action.gate === 'overridden') return m.intakeGateOverridden(name);
  if (action.missing.includes('labels')) return m.intakeGateLabels(name, action.needed, action.labels);
  if (action.missing.includes('severe')) return m.intakeGateSevere(name, action.severe);
  if (action.missing.includes('junk')) return m.intakeGateJunk(name);
  return m.intakeGateAccuracy(name, action.wrong, action.labels, percent(action.accuracy_needed));
}

interface Props {
  channelID: string;
  scope: string;
  // The scope's settings as they are: the server takes them whole with the level.
  options: Options | null;
  // The workspace's AI settings: on Colliedesk AI, the estimate is a share of the AI included each month.
  settings?: AISettings;
  onClose: () => void;
  onTurnOn: (input: ChannelAIInput) => void;
}

// IntakeAutoDialog asks before intake decides on its own on a scope (docs/user/ai.md "Intake"): each action
// runs alone only where explicit labels show it is ready, the rest stays a suggestion; an admin may let a
// closed one run anyway with a reason. It says what 30 days would cost.
export function IntakeAutoDialog({ channelID, scope, options, settings, onClose, onTurnOn }: Props) {
  const id = useId();
  const readiness = useQuery({ queryKey: ['intake-readiness', channelID, scope], queryFn: () => aiApi.readiness(channelID, scope) });
  const [override, setOverride] = useState<Action['action'][]>(() => options?.gate_override ?? []);
  const [reason, setReason] = useState(options?.gate_override_reason ?? '');
  const [problem, setProblem] = useState<string>();
  const data = readiness.data;
  const submit = () => {
    if (override.length > 0 && reason.trim() === '') {
      setProblem(m.intakeOverrideNeedsReason);
      document.getElementById(`${id}-why`)?.focus();
      return;
    }
    const { gate_override: _gate, gate_override_reason: _why, ...rest } = options ?? {};
    onTurnOn({ scope, level: 'auto', options: { ...rest, ...(override.length > 0 ? { gate_override: override, gate_override_reason: reason.trim() } : {}) } });
  };
  const toggle = (action: Action['action'], on: boolean) => setOverride((current) => (on ? [...current, action] : current.filter((item) => item !== action)));
  // What 30 days cost: in dollars where the server knows its provider's price (with the share of the AI
  // included each month on Colliedesk AI), in the provider's own credits where it reports those.
  const estimateCost = () => {
    if (!data) return null;
    const { ai_calls: calls, cost_per_call: each } = data.estimate;
    if (each.micro_usd !== null) {
      const total = each.micro_usd * calls;
      const allowance = settings?.slots.find((item) => item.slot === 'intake')?.source === 'server' ? settings?.allowance : null;
      return allowance && allowance.limit_micro_usd > 0 ? m.intakeCostShare(money(total), Math.max(1, Math.round((total / allowance.limit_micro_usd) * 100))) : money(total);
    }
    return each.provider_cost !== null ? m.intakeCostCredits(Number((each.provider_cost * calls).toPrecision(2))) : null;
  };
  return <Dialog title={m.intakeAutoTitle(scopeName(scope))} help={m.intakeAutoLead} onClose={onClose} closeLabel={m.close} className="ai-dialog"
    onSubmit={submit} footer={<>
      {/* Cancel takes the focus: Turn on Auto waits for readiness, and a dialog keeps its keys inside. */}
      <button type="button" className="ghost" autoFocus onClick={onClose}>{m.cancel}</button>
      <button type="submit" className="primary-action" disabled={!data}>{m.intakeAutoOn}</button>
    </>}>
    {readiness.isPending && <p role="status" className="muted-copy">{m.loading}</p>}
    {readiness.isError && <p role="alert" className="error-copy">{m.intakeReadinessFailed}</p>}
    {data && <>
      <ul className="ch-intake-gates">{data.actions.map((action) => <li key={action.action} data-gate={action.gate}>{gateText(action)}</li>)}</ul>
      <p className="help">{m.intakeGateHelp}</p>
      {data.actions.some((action) => action.gate !== 'open') && <fieldset className="ch-intake-group">
        <legend>{m.intakeOverride}</legend>
        {data.actions.filter((action) => action.gate !== 'open').map((action) => <label key={action.action} className="ch-intake-check">
          <input type="checkbox" checked={override.includes(action.action)} onChange={(event) => toggle(action.action, event.target.checked)} />
          <span>{m.intakeOverrideAction(m.intakeActionLower[action.action])}</span></label>)}
        {override.length > 0 && <div className="field"><label htmlFor={`${id}-why`}>{m.intakeOverrideReason}</label>
          <textarea id={`${id}-why`} rows={2} maxLength={500} value={reason} aria-invalid={problem ? true : undefined} aria-describedby={problem ? `${id}-problem` : undefined} onChange={(event) => { setReason(event.target.value); setProblem(undefined); }} /></div>}
      </fieldset>}
      <p className="ch-intake-estimate">{data.estimate.ai_calls === 0 ? m.intakeEstimateNone
        : m.intakeEstimate(data.estimate.ai_calls, data.estimate.days, estimateCost())}</p>
    </>}
    {problem && <p role="alert" className="error-copy" id={`${id}-problem`}>{problem}</p>}
  </Dialog>;
}
