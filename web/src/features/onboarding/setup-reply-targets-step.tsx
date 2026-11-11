import { useId, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { responseTargetsApi } from '../../api/response-targets';
import type { ResponseTargets, ResponseTargetsChange } from '../../api/response-targets';
import type { ReplyTargetPreset, ReplyTargetPresetKey, SetupProgress } from '../../api/schemas';
import { messages } from '../../messages';
import { MailIcon } from '../../ui/icons';
import { failureReason } from './failure-reason';
import { FlowActions } from './setup-flow-actions';
import type { FlowMoves } from './setup-flow-actions';
import { replyTargetsMessages as m } from './setup-ai-and-targets-messages';
import { exampleDue, spokenMinutes, targetText, workedExample } from './reply-target-time';
import type { BusinessHours } from './reply-target-time';
import './setup-ai-and-targets.css';

const priorities = ['urgent', 'high', 'normal', 'low'] as const;

// The document a preset makes: its targets, on, the workspace's own hours when the document has an open day (read
// with its version, so a setup read from before a change in Settings cannot save older hours), else the hours the
// server names (Monday to Friday, 09:00 to 18:00); due soon 30 minutes before; the rest as it was read, with the
// version it was read at, so someone else's save in between is refused rather than overwritten.
function presetDocument(doc: ResponseTargets, preset: ReplyTargetPreset, fallback: ResponseTargetsChange['hours']): ResponseTargetsChange {
  const { enabled_at: _enabled, my_alert_email: _mine, ...rest } = doc;
  const hours = doc.hours.some((day) => day.open) ? doc.hours : fallback;
  return { ...rest, enabled: true, hours, targets: preset.targets, warn_before_minutes: 30 };
}

function Preset({ preset, day, selected, tag, onSelect }: { preset: ReplyTargetPreset; day: number; selected: boolean; tag?: string; onSelect: () => void }) {
  const promiseID = useId();
  const promise = m.promise[preset.key];
  return <div className="setup-preset" data-selected={selected || undefined}>
    <label className="setup-preset-head">
      <input type="radio" name="reply-target-preset" checked={selected} onChange={onSelect} aria-describedby={promiseID} />
      <b>{preset.name}</b>{tag && <span className="setup-preset-tag">{tag}</span>}
    </label>
    <p id={promiseID} className="setup-preset-promise">{promise.normal} {promise.who}</p>
    <table className="setup-preset-table" aria-label={preset.name}>
      <thead><tr><td /><th scope="col">{m.reply}</th><th scope="col">{m.resolve}</th></tr></thead>
      <tbody>{priorities.map((priority) => {
        const target = preset.targets[priority];
        return <tr key={priority}><th scope="row">{m.priority[priority]}</th>
          <td>{target.first_response_minutes ? targetText(target.first_response_minutes, day) : '—'}</td>
          <td>{target.resolution_minutes ? targetText(target.resolution_minutes, day) : '—'}</td></tr>;
      })}</tbody>
    </table>
  </div>;
}

// TodayExample is three rows of Today with the chosen preset, counted in the hours it applies with: one late, two
// due, a High that came in at 09:30 and a Normal at 09:40 on a Tuesday.
function TodayExample({ preset, hours }: { preset: ReplyTargetPreset; hours: BusinessHours[] }) {
  const [late, high, normal] = m.example;
  const due = (minute: number, priority: 'high' | 'normal') => m.due(exampleDue(hours, minute, preset.targets[priority].first_response_minutes ?? 0, m.tomorrow));
  const rows = [{ ...late, when: m.late(12), late: true }, { ...high, when: due(9 * 60 + 30, 'high'), late: false }, { ...normal, when: due(9 * 60 + 40, 'normal'), late: false }];
  const labelID = useId();
  return <figure className="setup-example" aria-labelledby={labelID}>
    <figcaption id={labelID} className="setup-example-label">{m.exampleLabel(preset.name)}</figcaption>
    <ul className="setup-example-card setup-today">
      {rows.map((row) => <li key={row.name} className="setup-example-row">
        <span className="setup-example-waiting" data-late={row.late || undefined}>{row.when}</span>
        <span className="setup-example-priority" data-priority={row.priority.toLowerCase()}>{row.priority}</span>
        <strong>{row.name}</strong><span className="setup-example-subject">{row.subject}</span><MailIcon />
      </li>)}
    </ul>
  </figure>;
}

// ReplyTargetsStep offers three presets, picks the one the team's history fits (the server's suggestion), and
// saves the chosen one through Settings → Response targets' own call, then opens the next step.
export function ReplyTargetsStep({ state, setup, zone, importState, moves }: {
  state: string; setup: SetupProgress; zone: string; importState?: string; moves: FlowMoves;
}) {
  const client = useQueryClient();
  const doc = useQuery({ queryKey: ['response-targets'], queryFn: responseTargetsApi.get });
  // Null once targets are on, or for someone who may not change them: neither sees this choice.
  const suggestion = setup.reply_target_suggestion ?? { preset: 'same_day', basis: 'no_history', median_first_reply_minutes: null };
  // The choice follows the server's suggestion (it changes when an import ends) until the person picks one.
  const [picked, setPicked] = useState<ReplyTargetPresetKey>();
  const chosen = picked ?? suggestion.preset;
  const presets = setup.reply_target_presets;
  const preset = presets.find((item) => item.key === chosen) ?? presets[0];
  const { hours, day_minutes: day, kept: ownHours } = setup.reply_target_hours;
  // Every save, refused or not, reads the targets and the setup again: after someone else's save (409) the next
  // press sends the version now stored.
  const reread = () => Promise.all(['setup', 'response-targets'].map((key) => client.invalidateQueries({ queryKey: [key] })));
  const save = useMutation({
    mutationFn: (value: ResponseTargetsChange) => responseTargetsApi.set(value),
    onSuccess: async () => { await reread(); moves.next(); },
    onError: () => void reread(),
  });
  const suggested = presets.find((item) => item.key === suggestion.preset)?.name ?? '';
  const median = suggestion.median_first_reply_minutes;
  const basis = suggestion.basis === 'history' && median !== null ? m.history(spokenMinutes(median), suggested)
    : suggestion.basis === 'import_running' || importState === 'running' ? m.importRunning(suggested)
    : importState === 'done' ? m.noRecentHistory(suggested) : m.noHistory(suggested);
  const tag = suggestion.basis === 'history' ? m.fits : m.suggested;
  const timezone = doc.data?.timezone ?? zone;
  const worked = workedExample(hours, preset.targets.normal.first_response_minutes ?? 0, m.worked);
  const fromBrowser = Intl.DateTimeFormat().resolvedOptions().timeZone === timezone;
  if (state === 'on') {
    const normal = doc.data?.targets.normal.first_response_minutes;
    return <>
      <p className="setup-done" role="status"><span className="setup-done-mark">{m.onText(normal ? targetText(normal, day) : '—')}</span>
        <a href="/settings/targets">{m.changeInSettings}</a></p>
      <FlowActions back={moves.back} primary={<button type="button" className="primary-action" onClick={moves.next}>{moves.nextLabel}</button>} pinned />
    </>;
  }
  return <>
    <div className="setup-flow-body setup-steps-body">
      <p className="setup-basis">{basis}</p>
      <fieldset className="setup-presets">
        <legend className="sr-only">{m.presetsLabel}</legend>
        {presets.map((item) => <Preset key={item.key} preset={item} day={day} selected={item.key === chosen} tag={item.key === suggestion.preset ? tag : undefined} onSelect={() => setPicked(item.key)} />)}
      </fieldset>
      <p className="setup-small">{m.dayNote(spokenMinutes(day))}</p>
      <p className="setup-hours"><span>{ownHours ? m.ownHours(timezone) : m.hours(timezone, fromBrowser)}</span><a href="/settings/targets">{m.change}</a></p>
      <TodayExample preset={preset} hours={hours} />
      {worked && <p className="setup-small">{worked}</p>}
      <p className="setup-small">{m.startsNote}</p>
      {save.isError && <p role="alert" className="error-copy">{m.failed}</p>}
      {doc.isError && !doc.data && <p role="alert" className="error-copy">{m.readFailed(failureReason(doc.error))}{' '}
        <button type="button" className="link-action" onClick={() => void doc.refetch()}>{messages.tryAgain}</button></p>}
    </div>
    <FlowActions back={moves.back} later={moves.later} pinned note={m.consentLine}
      primary={<button type="button" className="primary-action" disabled={!doc.data || save.isPending} onClick={() => doc.data && save.mutate(presetDocument(doc.data, preset, hours))}>{m.use(preset.name)}</button>} />
  </>;
}
