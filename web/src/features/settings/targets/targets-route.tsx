import { useEffect, useId, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '../../../api/client';
import { channelsApi } from '../../../api/channels';
import { responseTargetsApi, weekdays } from '../../../api/response-targets';
import type { ResponseTargets, Weekday } from '../../../api/response-targets';
import { messages } from '../../../messages';
import { useBillingShown } from '../../billing/billing-shown';
import { saveState, useSaveFeedback } from '../save-feedback';
import { SettingsNav } from '../settings-nav';
import type { Shown } from '../settings-nav';
import { TimezoneSelect, sameZone } from '../timezone-select';
import { targetsMessages as m } from './targets-messages';
import { durationOf, limitIn, maxTargetMinutes, maxWarnMinutes, minutesOf, spoken, targetProblem, units, warnProblem } from './target-duration';
import type { Duration, Unit } from './target-duration';
import { OffCard } from './targets-off-card';
import { Card, ZoneNote, cardState, fieldErrors, useSave, withoutStamp } from './targets-shared';
import { can } from '../../../lib/can';
import '../../../ui/controls.css';
import './targets.css';

// Settings → Response targets: one document the server keeps whole. While measuring is off the page is one
// card; on, each card edits its part and saves the document with the rest as it was read. Admins change it,
// agents read it.

const priorities = ['urgent', 'high', 'normal', 'low'] as const;
type Priority = (typeof priorities)[number];

export function TargetsRoute() {
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const isAdmin = can(me.data, 'workspace:update');
  const billingShown = useBillingShown(isAdmin);
  const shown: Shown = { general: isAdmin, team: isAdmin, channels: true, savedReplies: true, exportData: isAdmin, billing: billingShown, importData: isAdmin, ai: true };
  const doc = useQuery({ queryKey: ['response-targets'], queryFn: responseTargetsApi.get });
  // Targets per channel need the channel list, which only a role that reads channels may ask for.
  const channels = useQuery({ queryKey: ['channels'], queryFn: channelsApi.list, enabled: can(me.data, 'channels:read') });
  const workspaceZone = me.data?.workspace.timezone ?? 'UTC';
  // The zone of the reader's own dates (Reports, the inbox): the clock below may count in another.
  const myZone = me.data?.timezone ?? workspaceZone;
  const live = channels.data?.items.filter((item) => !item.deleted_at) ?? [];
  // What the server refused when measuring was turned on, kept so the editor opens to fix it while still off.
  // Its words go as soon as the document changed (any card saved), the editor stays until measuring is on.
  const [refusal, setRefusal] = useState<{ fields: Record<string, string>; at: string | null }>();
  const measuring = doc.data?.enabled;
  const refused = refusal && refusal.at === (doc.data?.updated_at ?? null) ? refusal.fields : undefined;
  const deleted = channels.data ? (doc.data?.channel_ids ?? []).filter((id) => !live.some((item) => item.id === id)) : [];
  const canChoose = live.length >= 2 || deleted.length > 0 || Boolean(refusal);
  useEffect(() => { if (measuring) setRefusal(undefined); }, [measuring]);
  return <main className="settings-page panel targets-page">
    <header><h1>{m.heading}</h1><p>{m.description}</p></header>
    <div className="settings-layout">
      <SettingsNav page="/settings/targets" shown={shown} />
      <div className="settings-sections">
        {me.data && !isAdmin && <p className="notice notice-plain" role="note"><span>{m.adminsOnly}</span></p>}
        {doc.isPending ? <div className="tg-loading" aria-busy="true"><span className="sr-only">{m.loading}</span></div>
          : doc.isError ? <p role="alert" className="error-copy">{m.loadFailed} <button type="button" className="link-action" onClick={() => void doc.refetch()}>{m.tryAgain}</button></p>
          : !doc.data.enabled && !refusal ? <OffCard doc={doc.data} canManage={isAdmin} workspaceZone={workspaceZone} myZone={myZone} onRefused={(fields) => setRefusal({ fields, at: doc.data.updated_at })} />
          : <>
              {doc.data.enabled ? <OnCard doc={doc.data} canManage={isAdmin} />
                : <OffCard doc={doc.data} canManage={isAdmin} workspaceZone={workspaceZone} myZone={myZone} repairing onRefused={(fields) => setRefusal({ fields, at: doc.data.updated_at })} />}
              <HoursCard doc={doc.data} canManage={isAdmin} workspaceZone={workspaceZone} myZone={myZone} refused={refused} />
              <HolidaysCard doc={doc.data} canManage={isAdmin} refused={refused} />
              <TargetsCard doc={doc.data} canManage={isAdmin} refused={refused} />
              <AlertsCard doc={doc.data} canManage={isAdmin} />
              {/* One channel: nothing to choose (design rule: only real choices), unless the scope names a deleted one. */}
              {canChoose && <AppliesCard doc={doc.data} canManage={isAdmin} channels={live.map((item) => ({ id: item.id, name: item.name }))} refused={refused} />}
            </>}
      </div>
    </div>
  </main>;
}

// refused: what the server refused on the turn-on that opened the editor, by field, shown beside it.
interface CardProps { doc: ResponseTargets; canManage: boolean; refused?: Record<string, string> }

function OnCard({ doc, canManage }: CardProps) {
  const save = useSave();
  const feedback = useSaveFeedback(cardState(save));
  const id = useId();
  return <Card id={id} title={m.onHeading} lead={m.onLead}>
    {canManage
      ? <span className="button-row"><label className="field-inline"><span className="switch"><input type="checkbox" role="switch" checked={save.isPending ? Boolean(save.variables?.enabled) : doc.enabled}
          disabled={save.isPending} onChange={(event) => { feedback.touch(); save.mutate({ ...withoutStamp(doc), enabled: event.target.checked }); }} /></span><span>{m.measure}</span></label>{feedback.status}</span>
      : <p>{doc.enabled ? m.yes : m.no}</p>}
  </Card>;
}

function HoursCard({ doc, canManage, workspaceZone, myZone, refused }: CardProps & { workspaceZone: string; myZone: string }) {
  // What is typed here lies over the document, field by field: a field nobody touched always shows the
  // document as it is now (after another card's save, or someone else's), a touched one keeps its words.
  const [zoneEdit, setZoneEdit] = useState<string>();
  const [dayEdits, setDayEdits] = useState<Partial<Record<Weekday, Partial<ResponseTargets['hours'][number]>>>>({});
  const zone = zoneEdit ?? doc.timezone ?? '';
  const [checked, setChecked] = useState(false);
  const save = useSave();
  const feedback = useSaveFeedback(cardState(save));
  const server = { ...refused, ...fieldErrors(save.error) };
  const id = useId();
  const byDay = (day: Weekday) => ({ ...(doc.hours.find((item) => item.day === day) ?? { day, open: false, from: '08:00', to: '17:00' }), ...dayEdits[day] });
  const change = (day: Weekday, part: Partial<ResponseTargets['hours'][number]>) => {
    feedback.touch();
    setDayEdits((all) => ({ ...all, [day]: { ...all[day], ...part } }));
  };
  // A day closing at midnight is 24:00 on the wire; the time field shows and takes 00:00 for it.
  const bad = (day: Weekday) => { const item = byDay(day); return item.open && item.to !== '24:00' && item.to <= item.from; };
  if (!canManage) {
    return <Card id={id} title={m.hoursHeading} lead={m.hoursLead}><dl className="tg-dl">
      <dt>{m.timezone}</dt><dd>{doc.timezone ?? m.workspaceZone(workspaceZone)}</dd>
      {weekdays.map((day) => <Row key={day} label={m.days[day]} value={byDay(day).open ? `${byDay(day).from}–${byDay(day).to}` : m.closed} />)}
    </dl><ZoneNote clock={doc.timezone ?? workspaceZone} mine={myZone} /></Card>;
  }
  return <Card id={id} title={m.hoursHeading} lead={m.hoursLead}>
    <form className="stacked-form" noValidate onSubmit={(event) => {
      event.preventDefault();
      setChecked(true);
      if (feedback.pending || weekdays.some(bad)) return;
      save.mutate({ ...withoutStamp(doc), timezone: zone || null, hours: weekdays.map(byDay) }, { onSuccess: () => { setZoneEdit(undefined); setDayEdits({}); } });
    }}>
      <TimezoneSelect label={m.timezone} value={zone} emptyOption={m.workspaceZone(workspaceZone)} suggested={sameZone(myZone, workspaceZone) ? undefined : { zone: myZone, label: m.yourZone(myZone) }}
        onChange={(next) => { feedback.touch(); setZoneEdit(next); }} />
      <ZoneNote clock={doc.timezone ?? workspaceZone} mine={myZone} />
      {server.timezone && <span className="error-copy field-note" role="alert">{server.timezone}</span>}
      {server.hours && <p className="error-copy" role="alert">{server.hours}</p>}
      <p className="muted-copy" id={`${id}-midnight`}>{m.midnight}</p>
      <div className="tg-days">
        {weekdays.map((day, index) => {
          const item = byDay(day);
          const error = (checked && bad(day) ? m.endsBefore : undefined) ?? server[`hours.${index}.to`] ?? server[`hours.${index}.from`];
          return <div className={`tg-day${error ? ' field-bad' : ''}`} key={day}>
            <label className="field-inline"><input type="checkbox" checked={item.open} aria-label={m.open(m.days[day])} onChange={(event) => change(day, { open: event.target.checked })} /><span>{m.days[day]}</span></label>
            {item.open
              ? <span className="tg-hours">
                  <input type="time" aria-label={m.from(m.days[day])} value={item.from} aria-invalid={error ? true : undefined} aria-describedby={error ? `${id}-${day}-error` : undefined} onChange={(event) => change(day, { from: event.target.value })} />
                  <span aria-hidden="true">–</span>
                  <input type="time" aria-label={m.to(m.days[day])} value={item.to === '24:00' ? '00:00' : item.to} aria-invalid={error ? true : undefined}
                    aria-describedby={[error ? `${id}-${day}-error` : '', `${id}-midnight`].filter(Boolean).join(' ')} onChange={(event) => change(day, { to: event.target.value === '00:00' ? '24:00' : event.target.value })} />
                  {error && <span className="error-copy field-note" id={`${id}-${day}-error`}>{error}</span>}
                </span>
              : <span className="tg-closed">{m.closed}</span>}
          </div>;
        })}
      </div>
      <span className="button-row"><button type="button" className="link-action" onClick={() => {
        const { open, from, to } = byDay('mon');
        feedback.touch();
        setDayEdits((all) => ({ ...all, tue: { open, from, to }, wed: { open, from, to }, thu: { open, from, to }, fri: { open, from, to } }));
      }}>{m.copyMonday}</button></span>
      <span className="button-row"><button className="primary-action" type="submit" disabled={feedback.pending}>{feedback.saveLabel(m.saveHours)}</button>{feedback.status}</span>
    </form>
  </Card>;
}

function HolidaysCard({ doc, canManage, refused }: CardProps) {
  const [date, setDate] = useState('');
  const [name, setName] = useState('');
  const [checked, setChecked] = useState(false);
  const save = useSave();
  const feedback = useSaveFeedback(cardState(save));
  const server = { ...refused, ...fieldErrors(save.error) };
  const id = useId();
  const sorted = [...doc.holidays].sort((a, b) => a.date.localeCompare(b.date));
  const local = !date ? m.dateRequired : doc.holidays.some((item) => item.date === date) ? m.holidayTaken : undefined;
  const serverError = Object.entries(server).find(([field]) => field.startsWith('holidays'))?.[1];
  return <Card id={id} title={m.holidaysHeading} lead={m.holidaysLead}>
    {sorted.length === 0 ? <p className="muted-copy">{m.noHolidays}</p>
      : <ul className="tg-holidays">{sorted.map((item) => <li key={item.date}><span className="tg-mono">{item.date}</span><span>{item.name}</span>
          {canManage && <button type="button" aria-label={m.removeHoliday(item.name, item.date)} disabled={save.isPending}
            onClick={() => { feedback.touch(); save.mutate({ ...withoutStamp(doc), holidays: doc.holidays.filter((other) => other.date !== item.date) }); }}>{m.remove}</button>}</li>)}</ul>}
    {canManage && <form className="tg-add" noValidate onSubmit={(event) => {
      event.preventDefault();
      setChecked(true);
      if (local || feedback.pending) return;
      save.mutate({ ...withoutStamp(doc), holidays: [...doc.holidays, { date, name: name.trim() }] }, { onSuccess: () => { setDate(''); setName(''); setChecked(false); } });
    }}>
      <label className="field"><span>{m.holidayDate}</span><input type="date" value={date} aria-invalid={checked && local ? true : undefined} aria-describedby={checked && local ? `${id}-date` : undefined}
        onChange={(event) => { feedback.touch(); setDate(event.target.value); }} /></label>
      <label className="field"><span>{m.holidayName}</span><input value={name} maxLength={80} onChange={(event) => { feedback.touch(); setName(event.target.value); }} /></label>
      <button type="submit" disabled={feedback.pending}>{m.addHoliday}</button>
      {checked && local && <span className="error-copy field-note" id={`${id}-date`}>{local}</span>}
      {serverError && <span className="error-copy field-note" role="alert">{serverError}</span>}
      {feedback.status}
    </form>}
  </Card>;
}

// A number and its unit, each with a name of its own (neither name contains the other).
function DurationField({ id, label, unitLabel, value, errorId, onChange }: { id?: string; label: string; unitLabel: string; value: Duration; errorId?: string; onChange: (next: Duration) => void }) {
  return <>
    <input id={id} type="number" min={0} step="any" inputMode="decimal" aria-label={label} value={value.amount}
      aria-invalid={errorId ? true : undefined} aria-describedby={errorId} onChange={(event) => onChange({ ...value, amount: event.target.value })} />
    <select aria-label={unitLabel} value={value.unit} onChange={(event) => onChange({ ...value, unit: event.target.value as Unit })}>
      {units.map((unit) => <option key={unit} value={unit}>{unit}</option>)}
    </select>
  </>;
}

function TargetsCard({ doc, canManage, refused }: CardProps) {
  // Typed values lie over the document box by box ("urgent.first", "warn"), number and unit together;
  // an untouched box follows the document.
  const [edits, setEdits] = useState<Partial<Record<string, Duration>>>({});
  const fromDoc = (priority: Priority, which: 'first' | 'resolve') => durationOf(doc.targets[priority][which === 'first' ? 'first_response_minutes' : 'resolution_minutes']);
  const valueOf = (priority: Priority, which: 'first' | 'resolve') => edits[`${priority}.${which}`] ?? fromDoc(priority, which);
  const warn = edits.warn ?? durationOf(doc.warn_before_minutes);
  const [checked, setChecked] = useState(false);
  const save = useSave();
  const feedback = useSaveFeedback(cardState(save));
  const server = { ...refused, ...fieldErrors(save.error) };
  const id = useId();
  const priorityName = (priority: Priority) => messages.priorities[priority];
  const edit = (key: string) => (next: Duration) => { feedback.touch(); setEdits((all) => ({ ...all, [key]: next })); };
  if (!canManage) {
    const read = (minutes: number | null) => (minutes === null ? m.none : spoken(minutes));
    return <Card id={id} title={m.targetsHeading} lead={m.targetsReadLead} tip={m.targetsNote}><dl className="tg-dl">
      {priorities.map((priority) => <Row key={priority} label={priorityName(priority)}
        value={`${m.firstReply}: ${read(doc.targets[priority].first_response_minutes)} · ${m.resolve}: ${read(doc.targets[priority].resolution_minutes)}`} />)}
      <Row label={m.dueSoon} value={doc.warn_before_minutes === 0 ? m.noReminder : `${spoken(doc.warn_before_minutes)} ${m.beforeTarget}`} />
    </dl></Card>;
  }
  // Checked on save; the limit is named in the unit the box shows.
  const targetError = (value: Duration) => (!checked ? undefined
    : { rule: m.targetRule, max: m.targetMax(limitIn(maxTargetMinutes, value.unit)), none: undefined }[targetProblem(value) ?? 'none']);
  const cell = (priority: Priority, which: 'first' | 'resolve', field: string) => {
    const value = valueOf(priority, which);
    const error = targetError(value) ?? server[`targets.${priority}.${field}`];
    const errorId = `${id}-${priority}-${which}`;
    const what = which === 'first' ? m.firstReply : m.resolve;
    return <td data-label={what}><span className="tg-cell">
      <DurationField label={m.targetFor(what, priorityName(priority))} unitLabel={m.unitFor(what, priorityName(priority))} value={value} errorId={error ? errorId : undefined} onChange={edit(`${priority}.${which}`)} /></span>
      {error && <span className="error-copy field-note" id={errorId}>{error}</span>}</td>;
  };
  const warnError = server.warn_before_minutes ?? (!checked ? undefined
    : { rule: m.warnRule, max: m.warnMax(limitIn(maxWarnMinutes, warn.unit)), none: undefined }[warnProblem(warn) ?? 'none']);
  return <Card id={id} title={m.targetsHeading} lead={m.targetsLead} tip={m.targetsNote}>
    <form className="stacked-form" noValidate onSubmit={(event) => {
      event.preventDefault();
      setChecked(true);
      const bad = priorities.some((priority) => targetProblem(valueOf(priority, 'first')) || targetProblem(valueOf(priority, 'resolve'))) || warnProblem(warn);
      if (bad || feedback.pending) return;
      // Checked above: each is whole minutes, or null for an empty target box.
      const minutes = (value: Duration) => minutesOf(value) ?? null;
      save.mutate({ ...withoutStamp(doc), warn_before_minutes: minutes(warn) ?? 0,
        targets: Object.fromEntries(priorities.map((priority) => [priority, { first_response_minutes: minutes(valueOf(priority, 'first')), resolution_minutes: minutes(valueOf(priority, 'resolve')) }])) as ResponseTargets['targets'] },
        { onSuccess: () => setEdits({}) });
    }}>
      <table className="tg-table">
        <thead><tr><th scope="col">{m.priority}</th><th scope="col">{m.firstReply}</th><th scope="col">{m.resolve}</th></tr></thead>
        <tbody>{priorities.map((priority) => <tr key={priority}><th scope="row">{priorityName(priority)}</th>{cell(priority, 'first', 'first_response_minutes')}{cell(priority, 'resolve', 'resolution_minutes')}</tr>)}</tbody>
      </table>
      <div className={`field${warnError ? ' field-bad' : ''}`}>
        <span className="tg-cell"><label htmlFor={`${id}-warn`}>{m.dueSoon}</label>
          <DurationField id={`${id}-warn`} label={m.dueSoonLabel} unitLabel={m.dueSoonUnit} value={warn} errorId={warnError ? `${id}-warn-error` : undefined} onChange={edit('warn')} /><span>{m.beforeTarget}</span></span>
        {warnError && <span className="error-copy field-note" id={`${id}-warn-error`}>{warnError}</span>}
      </div>
      <span className="button-row"><button className="primary-action" type="submit" disabled={feedback.pending}>{feedback.saveLabel(m.saveTargets)}</button>{feedback.status}</span>
    </form>
  </Card>;
}

function AppliesCard({ doc, canManage, channels, refused }: CardProps & { channels: { id: string; name: string }[] }) {
  const every = channels.map((item) => item.id);
  // Ids of the scope that name no channel any more: listed ticked, for the person to untick. None is dropped on its own.
  const deleted = (doc.channel_ids ?? []).filter((id) => !every.includes(id));
  // The choice follows the document until someone here changes it.
  const [chosenEdit, setChosenEdit] = useState<string[]>();
  const chosen = chosenEdit ?? doc.channel_ids ?? every;
  const [checked, setChecked] = useState(false);
  const save = useSave();
  const feedback = useSaveFeedback(cardState(save));
  const server = { ...refused, ...fieldErrors(save.error) };
  const refusedScope = Object.entries(server).find(([field]) => field.startsWith('channel_ids'))?.[1];
  const id = useId();
  if (!canManage) return <Card id={id} title={m.appliesHeading} lead={m.appliesLead}><ul className="muted-copy">{channels.filter((item) => chosen.includes(item.id)).map((item) => <li key={item.id}>{item.name}</li>)}</ul></Card>;
  return <Card id={id} title={m.appliesHeading} lead={m.appliesLead}>
    <form className="stacked-form" noValidate onSubmit={(event) => {
      event.preventDefault();
      setChecked(true);
      if (chosen.length === 0 || feedback.pending) return;
      // Every channel is null: a channel added later is measured too.
      save.mutate({ ...withoutStamp(doc), channel_ids: chosen.length === every.length && every.every((item) => chosen.includes(item)) ? null : chosen }, { onSuccess: () => setChosenEdit(undefined) });
    }}>
      <fieldset className="tg-channels"><legend className="sr-only">{m.appliesHeading}</legend>
        {[...deleted.map((id) => ({ id, name: m.deletedChannel(id) })), ...channels].map((item) => <label className="check-row" key={item.id}><input type="checkbox" checked={chosen.includes(item.id)}
          onChange={(event) => { feedback.touch(); setChosenEdit(event.target.checked ? [...chosen, item.id] : chosen.filter((other) => other !== item.id)); }} /><span>{item.name}</span></label>)}
      </fieldset>
      {refusedScope && <p className="error-copy" role="alert">{refusedScope}</p>}
      {checked && chosen.length === 0 && <p className="error-copy" role="alert">{m.pickOne}</p>}
      <span className="button-row"><button className="primary-action" type="submit" disabled={feedback.pending}>{feedback.saveLabel(m.saveApplies)}</button>{feedback.status}</span>
    </form>
  </Card>;
}

// Alerts: whether reminders and overdue alerts go out by email (the workspace's), and to the reader.
function AlertsCard({ doc, canManage }: CardProps) {
  const client = useQueryClient();
  const save = useSave();
  const feedback = useSaveFeedback(cardState(save));
  const mine = useMutation({
    mutationFn: (email: boolean) => responseTargetsApi.setMyAlerts(email),
    onSuccess: (result) => client.setQueryData<ResponseTargets>(['response-targets'], (current) => (current ? { ...current, my_alert_email: result.email } : current)),
  });
  const mineFeedback = useSaveFeedback(saveState(mine));
  const id = useId();
  const workspaceOn = save.isPending ? Boolean(save.variables?.alert_email) : doc.alert_email;
  const meOn = mine.isPending ? Boolean(mine.variables) : doc.my_alert_email !== false;
  return <Card id={id} title={m.alertsHeading} lead={m.alertsLead} tip={m.alertsWho}>
    {canManage
      ? <span className="button-row"><label className="field-inline"><span className="switch"><input type="checkbox" role="switch" checked={workspaceOn} disabled={save.isPending}
          onChange={(event) => { feedback.touch(); save.mutate({ ...withoutStamp(doc), alert_email: event.target.checked }); }} /></span><span>{m.alertEmail}</span></label>{feedback.status}</span>
      : <p>{m.alertEmail}: {doc.alert_email ? m.yes : m.no}</p>}
    <span className="button-row"><label className="field-inline"><span className="switch"><input type="checkbox" role="switch" checked={meOn} disabled={mine.isPending || !doc.alert_email}
      aria-describedby={doc.alert_email ? undefined : `${id}-off`} onChange={(event) => { mineFeedback.touch(); mine.mutate(event.target.checked); }} /></span><span>{m.alertMe}</span></label>{mineFeedback.status}</span>
    {!doc.alert_email && <p className="muted-copy" id={`${id}-off`}>{m.alertsOff}</p>}
  </Card>;
}

function Row({ label, value }: { label: string; value: string }) {
  return <><dt>{label}</dt><dd>{value}</dd></>;
}
