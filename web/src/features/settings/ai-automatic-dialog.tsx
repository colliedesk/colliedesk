import { useId, useState } from 'react';
import type { AISettings } from '../../api/ai';
import { Field } from '../../ui/dialog';
import { TipField } from './tip-field';
import { aiMessages as m } from './ai-messages';
import { AIDialog as Dialog } from './ai-level-dialogs';
import type { AIPageProps } from './ai-page-props';

export function AutomaticDialog({ settings, onClose, onSave }: { settings: AISettings; onClose: () => void; onSave: AIPageProps['onSetAutomatic'] }) {
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
        <TipField label={m.perDay} tip={m.automaticWhy} id={`${id}-day`} error={bad.day}>{(attrs) => <input {...attrs} type="number" inputMode="numeric" min={0} max={10000} value={perDay} onChange={(event) => setPerDay(event.target.value)} autoFocus />}</TipField>
        <Field label={m.perMinute} id={`${id}-minute`} error={bad.minute}>{(attrs) => <input {...attrs} type="number" inputMode="numeric" min={0} max={1000} value={perMinute} onChange={(event) => setPerMinute(event.target.value)} />}</Field>
      </div>
      <TipField label={m.draftsPerDay} tip={m.draftsPerDayWhy} id={`${id}-drafts`} help={m.draftsPerDayHelp} error={bad.drafts}>{(attrs) => <input {...attrs} type="number" inputMode="numeric" min={0} max={10000} value={draftsPerDay}
        onChange={(event) => setDraftsPerDay(event.target.value)} />}</TipField>
      <p>{m.usedToday(settings.automatic.used_today)}</p>
    </Dialog>
  );
}
