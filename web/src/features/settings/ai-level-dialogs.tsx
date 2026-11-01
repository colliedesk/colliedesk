import type { AISettings, AISlot } from '../../api/ai';
import { Dialog as SharedDialog } from '../../ui/dialog';
import { aiMessages as m } from './ai-messages';
import './ai-settings.css';

// The dialogs that ask before an AI feature does more by itself, shared by Settings → Built-in AI and a
// channel's AI tab: the same words wherever the level is raised.

// money writes micro-dollars as dollars and cents.
export const money = (micro: number) => `$${(micro / 1e6).toFixed(2)}`;

export type Part = AISlot['auto_parts'][number];
export const parts: Part[] = ['tags', 'priority', 'assignee'];
const partList = (list: Part[]) => list.map((part) => m.autoPartLower[part]).join(' and ').replace(/ and (?=.* and )/g, ', ');

function Dialog(props: Omit<Parameters<typeof SharedDialog>[0], 'closeLabel'>) {
  return <SharedDialog {...props} closeLabel={m.close} className="ai-dialog" />;
}

export function AutoTriageDialog({ slot, onClose, onTurnOn }: { slot: AISlot; onClose: () => void; onTurnOn: () => void }) {
  const chosen = slot.auto_parts.length > 0 ? slot.auto_parts : (['tags', 'priority'] as Part[]);
  return (
    <Dialog title={m.autoConfirmTitle} help={m.autoConfirmLead(partList(chosen))} onClose={onClose} onSubmit={onTurnOn}
      footer={<><button type="button" className="ghost" onClick={onClose}>{m.cancel}</button><button type="submit" className="primary-action" autoFocus>{m.autoConfirmOn}</button></>}>
      <ul className="ai-facts">{m.autoConfirmFacts.map((fact) => <li key={fact}>{fact}</li>)}</ul>
    </Dialog>
  );
}

export function AutoDraftDialog({ slot, settings, onClose, onTurnOn }: { slot: AISlot; settings: AISettings; onClose: () => void; onTurnOn: () => void }) {
  const estimate = settings.auto_draft_estimate;
  const perDay = settings.automatic.draft_per_day;
  const server = slot.source === 'server';
  const each = estimate.draft_cost_micro_usd;
  const allowance = settings.allowance;
  const provider = settings.providers.find((item) => item.id === slot.provider_id);
  const half = allowance ? Math.floor(allowance.limit_micro_usd / 2) : null;
  return (
    <Dialog title={m.autoDraftConfirmTitle} help={m.autoDraftConfirmLead} onClose={onClose} onSubmit={onTurnOn}
      footer={<><button type="button" className="ghost" onClick={onClose}>{m.cancel}</button><button type="submit" className="primary-action" autoFocus>{m.autoConfirmOn}</button></>}>
      <dl className="ai-estimate">
        <div><dt>{m.estLast30}</dt><dd>{m.estMessages(estimate.customer_messages)}</dd></div>
        {each !== null && <div><dt>{m.estAbout}</dt><dd>{m.estMonth(money(each * estimate.customer_messages), money(each))}</dd></div>}
        {server && allowance && <div><dt>{m.estPlan}</dt><dd>{m.estPlanMonth(money(allowance.limit_micro_usd))}</dd></div>}
        {!server && <div><dt>{m.estAtMost}</dt><dd>{m.estAtMostDay(perDay)}</dd></div>}
      </dl>
      {server && half !== null ? <p className="ai-estimate-note"><b>{m.estHalf(money(half), each ? Math.floor(half / each) : null)}</b>{m.estHalfRest}</p>
        : !server && <p className="ai-estimate-note"><b>{m.estByo(provider?.label ?? '')}</b>{m.estByoRest}</p>}
      <ul className="ai-facts">{m.autoDraftFacts(perDay).map((fact) => <li key={fact}>{fact}</li>)}</ul>
      <p><a href="https://github.com/colliedesk/colliedesk/blob/main/docs/user/ai.md#write-a-draft-when-a-customer-writes" target="_blank" rel="noopener noreferrer">{m.estHow}</a></p>
    </Dialog>
  );
}
