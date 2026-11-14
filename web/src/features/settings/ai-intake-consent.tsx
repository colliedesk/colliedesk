import { useId, useState } from 'react';
import type { AISettings, AISlot } from '../../api/ai';
import { Sheet } from '../../ui/sheet';
import { aiMessages as m } from './ai-messages';
import { slotChoices } from './ai-slot-choice';

const text = {
  title: 'Turn on Chat intake',
  lead: 'Consent needed before it turns on',
  intro: 'Chat intake reads the messages of the chat channels you link and opens a conversation for each request.',
  goesHeading: 'What goes to the model',
  goes: 'The new message and the recent messages of that chat, with names, passwords, codes, phone numbers and email addresses hidden or masked first.',
  goesMore: 'Pictures and the customer’s profile go only if you agree to them under the feature’s settings.',
  whoHeading: 'Who processes it',
  who: (provider: string, model: string) => (model ? `${provider}, model ${model}.` : `${provider}.`),
  offHeading: 'Turning it off',
  off: 'Any time, from this row. Conversations already opened stay. The history says who turned it on.',
  agree: (provider: string) => `I agree that messages in linked chat channels are sent to ${provider} to open conversations.`,
  stays: 'Stays off until you agree',
  confirm: 'Agree and turn on',
};

// providerOf names where the feature runs and on which model, in the words the person chose.
function providerOf(slot: AISlot, settings: AISettings): { label: string; model: string } {
  const { choice, serverModels } = slotChoices(slot, settings);
  if (slot.source === 'server') return { label: choice?.label ?? settings.server?.label ?? '', model: serverModels[0] ?? '' };
  return { label: choice?.label ?? '', model: slot.model ?? '' };
}

// IntakeConsent is the step before chat intake turns on: what it reads, who processes it and how to stop it. The
// feature stays off until the person agrees. Agreeing is the act of turning it on: the server holds no other record of
// it for the conversation's own text (pictures and the profile have their own switches), and the history names who did it.
export function IntakeConsent({ slot, settings, onClose, onAgree }: { slot: AISlot; settings: AISettings; onClose: () => void; onAgree: () => void }) {
  const [agreed, setAgreed] = useState(false);
  const id = useId();
  const { label, model } = providerOf(slot, settings);
  return <Sheet title={text.title} closeLabel={m.close} onClose={onClose}
    footer={<><span>{text.stays}</span><button type="button" className="primary-action" disabled={!agreed} onClick={onAgree}>{text.confirm}</button></>}>
    <p className="ai-intake-lead"><strong>{text.lead}</strong></p>
    <p className="ai-sheet-intro">{text.intro}</p>
    <dl className="ai-intake-facts">
      <div><dt>{text.goesHeading}</dt><dd>{text.goes} {text.goesMore}</dd></div>
      <div><dt>{text.whoHeading}</dt><dd>{text.who(label, model)}</dd></div>
      <div><dt>{text.offHeading}</dt><dd>{text.off}</dd></div>
    </dl>
    <label className="ai-intake-agree" htmlFor={id}>
      <input id={id} type="checkbox" checked={agreed} onChange={(event) => setAgreed(event.target.checked)} />
      <span>{text.agree(label)}</span>
    </label>
  </Sheet>;
}
