import { useEffect, useId, useState } from 'react';
import type { ReactNode } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, InvalidRequestError } from '../../../api/client';
import { channelsApi } from '../../../api/channels';
import { responseTargetsApi, weekdays } from '../../../api/response-targets';
import type { ResponseTargets, ResponseTargetsChange, Weekday } from '../../../api/response-targets';
import { messages } from '../../../messages';
import { useBillingShown } from '../../billing/billing-shown';
import { saveState, useSaveFeedback } from '../save-feedback';
import { SettingsNav } from '../settings-nav';
import type { Shown } from '../settings-nav';
import { TimezoneSelect } from '../timezone-select';
import { targetsMessages as m } from './targets-messages';
import './targets.css';

// Settings → Response targets: one document the server keeps whole. Each card edits its part and saves
// the document with the rest as it was read; admins change it, agents read it.

const priorities = ['urgent', 'high', 'normal', 'low'] as const;
type Priority = (typeof priorities)[number];

function fieldErrors(error: unknown): Record<string, string> {
  if (!(error instanceof InvalidRequestError)) return {};
  const fields = error.detail?.fields;
  return fields && typeof fields === 'object' ? Object.fromEntries(Object.entries(fields).filter((entry): entry is [string, string] => typeof entry[1] === 'string')) : {};
}

// What a PUT takes: the document without what only the server writes or what is the reader's own.
const withoutStamp = ({ updated_at: _updated, enabled_at: _enabled, my_alert_email: _mine, ...rest }: ResponseTargets): ResponseTargetsChange => rest;
// Hours in the form, minutes on the wire; a target is in half hours.
const toHours = (minutes: number | null) => (minutes === null ? '' : String(minutes / 60));
const toMinutes = (hours: string) => (hours.trim() === '' ? null : Math.round(Number(hours) * 60));
const halfHours = (hours: string) => hours.trim() === '' || (Number(hours) > 0 && Number.isInteger(Number(hours) * 2));

function useSave() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (value: ResponseTargetsChange) => responseTargetsApi.set(value),
    onSuccess: (saved) => { client.setQueryData(['response-targets'], saved); void client.invalidateQueries({ queryKey: ['conversations'] }); },
  });
}

function Card({ id, title, lead, children }: { id: string; title: string; lead?: string; children: ReactNode }) {
  return <section className="settings-card" aria-labelledby={id}>
    <div className="section-heading"><h2 id={id}>{title}</h2>{lead && <p>{lead}</p>}</div>
    <div className="settings-body">{children}</div>
  </section>;
}

export function TargetsRoute() {
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const isAdmin = me.data?.agent.role === 'admin';
  const billingShown = useBillingShown(isAdmin);
  const shown: Shown = { general: isAdmin, team: isAdmin, channels: true, savedReplies: true, exportData: isAdmin, billing: billingShown, importData: isAdmin, ai: true };
  const doc = useQuery({ queryKey: ['response-targets'], queryFn: responseTargetsApi.get });
  const channels = useQuery({ queryKey: ['channels'], queryFn: channelsApi.list });
  const workspaceZone = me.data?.workspace.timezone ?? 'UTC';
  const live = channels.data?.items.filter((item) => !item.deleted_at) ?? [];
  return <main className="settings-page panel">
    <header><h1>{m.heading}</h1><p>{m.description}</p></header>
    <div className="settings-layout">
      <SettingsNav page="/settings/targets" shown={shown} />
      <div className="settings-sections">
        {me.data && !isAdmin && <p className="notice notice-plain" role="note"><span>{m.adminsOnly}</span></p>}
        {doc.isPending ? <div className="tg-loading" aria-busy="true"><span className="sr-only">{m.loading}</span></div>
          : doc.isError ? <p role="alert" className="error-copy">{m.loadFailed} <button type="button" className="link-action" onClick={() => void doc.refetch()}>{m.tryAgain}</button></p>
          : <>
              <OnCard doc={doc.data} canManage={isAdmin} />
              <HoursCard doc={doc.data} canManage={isAdmin} workspaceZone={workspaceZone} />
              <HolidaysCard doc={doc.data} canManage={isAdmin} />
              <TargetsCard doc={doc.data} canManage={isAdmin} />
              <AlertsCard doc={doc.data} canManage={isAdmin} />
              {/* One channel: nothing to choose (design rule: only real choices). */}
              {live.length >= 2 && <AppliesCard doc={doc.data} canManage={isAdmin} channels={live.map((item) => ({ id: item.id, name: item.name }))} />}
            </>}
      </div>
    </div>
  </main>;
}

interface CardProps { doc: ResponseTargets; canManage: boolean }

function OnCard({ doc, canManage }: CardProps) {
  const save = useSave();
  const feedback = useSaveFeedback(saveState(save));
  const id = useId();
  return <Card id={id} title={m.onHeading} lead={m.onLead}>
    {canManage
      ? <span className="button-row"><label className="field-inline"><span className="switch"><input type="checkbox" role="switch" checked={save.isPending ? Boolean(save.variables?.enabled) : doc.enabled}
          disabled={save.isPending} onChange={(event) => { feedback.touch(); save.mutate({ ...withoutStamp(doc), enabled: event.target.checked }); }} /></span><span>{m.measure}</span></label>{feedback.status}</span>
      : <p>{doc.enabled ? m.yes : m.no}</p>}
  </Card>;
}

function HoursCard({ doc, canManage, workspaceZone }: CardProps & { workspaceZone: string }) {
  const [zone, setZone] = useState(doc.timezone ?? '');
  const [hours, setHours] = useState(doc.hours);
  useEffect(() => { setZone(doc.timezone ?? ''); setHours(doc.hours); }, [doc.timezone, doc.hours]);
  const [checked, setChecked] = useState(false);
  const save = useSave();
  const feedback = useSaveFeedback(saveState(save));
  const server = fieldErrors(save.error);
  const id = useId();
  const byDay = (day: Weekday) => hours.find((item) => item.day === day) ?? { day, open: false, from: '08:00', to: '17:00' };
  const change = (day: Weekday, part: Partial<ResponseTargets['hours'][number]>) => {
    feedback.touch();
    setHours((all) => weekdays.map((name) => (name === day ? { ...byDay(name), ...part } : all.find((item) => item.day === name) ?? byDay(name))));
  };
  // A day closing at midnight is 24:00 on the wire; the time field shows and takes 00:00 for it.
  const bad = (day: Weekday) => { const item = byDay(day); return item.open && item.to !== '24:00' && item.to <= item.from; };
  if (!canManage) {
    return <Card id={id} title={m.hoursHeading} lead={m.hoursLead}><dl className="tg-dl">
      <dt>{m.timezone}</dt><dd>{doc.timezone ?? m.workspaceZone(workspaceZone)}</dd>
      {weekdays.map((day) => <Row key={day} label={m.days[day]} value={byDay(day).open ? `${byDay(day).from}–${byDay(day).to}` : m.closed} />)}
    </dl></Card>;
  }
  return <Card id={id} title={m.hoursHeading} lead={m.hoursLead}>
    <form className="stacked-form" noValidate onSubmit={(event) => {
      event.preventDefault();
      setChecked(true);
      if (feedback.pending || weekdays.some(bad)) return;
      save.mutate({ ...withoutStamp(doc), timezone: zone || null, hours: weekdays.map(byDay) });
    }}>
      <TimezoneSelect label={m.timezone} value={zone} emptyOption={m.workspaceZone(workspaceZone)} onChange={(next) => { feedback.touch(); setZone(next); }} />
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
        const monday = byDay('mon');
        feedback.touch();
        setHours(weekdays.map((day) => (['tue', 'wed', 'thu', 'fri'].includes(day) ? { ...monday, day } : byDay(day))));
      }}>{m.copyMonday}</button></span>
      <span className="button-row"><button className="primary-action" type="submit" disabled={feedback.pending}>{feedback.saveLabel(m.saveHours)}</button>{feedback.status}</span>
    </form>
  </Card>;
}

function HolidaysCard({ doc, canManage }: CardProps) {
  const [date, setDate] = useState('');
  const [name, setName] = useState('');
  const [checked, setChecked] = useState(false);
  const save = useSave();
  const feedback = useSaveFeedback(saveState(save));
  const server = fieldErrors(save.error);
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

// The form's values of the targets: hours, empty for no target.
const formOf = (targets: ResponseTargets['targets']) => Object.fromEntries(priorities.map((priority) => [priority,
  { first: toHours(targets[priority].first_response_minutes), resolve: toHours(targets[priority].resolution_minutes) }])) as Record<Priority, { first: string; resolve: string }>;

function TargetsCard({ doc, canManage }: CardProps) {
  const [values, setValues] = useState(() => formOf(doc.targets));
  const [warn, setWarn] = useState(toHours(doc.warn_before_minutes));
  useEffect(() => { setValues(formOf(doc.targets)); setWarn(toHours(doc.warn_before_minutes)); }, [doc.targets, doc.warn_before_minutes]);
  const [checked, setChecked] = useState(false);
  const save = useSave();
  const feedback = useSaveFeedback(saveState(save));
  const server = fieldErrors(save.error);
  const id = useId();
  const cellBad = (value: string) => checked && !halfHours(value);
  const warnBad = checked && !(warn.trim() !== '' && Number(warn) >= 0 && Number.isInteger(Number(warn) * 2));
  const priorityName = (priority: Priority) => messages.priorities[priority];
  if (!canManage) {
    return <Card id={id} title={m.targetsHeading} lead={m.targetsLead}><dl className="tg-dl">
      {priorities.map((priority) => <Row key={priority} label={priorityName(priority)}
        value={`${m.firstReply}: ${values[priority].first ? `${values[priority].first} h` : m.none} · ${m.resolve}: ${values[priority].resolve ? `${values[priority].resolve} h` : m.none}`} />)}
      <Row label={m.dueSoon} value={`${warn} ${m.dueSoonUnit(Number(warn))}`} />
    </dl><p className="muted-copy">{m.targetsNote}</p></Card>;
  }
  const cell = (priority: Priority, which: 'first' | 'resolve', field: string) => {
    const error = (cellBad(values[priority][which]) ? m.halfHours : undefined) ?? server[`targets.${priority}.${field}`];
    return <td data-label={which === 'first' ? m.firstReply : m.resolve}><span className="tg-cell">
      <input type="number" min={0.5} step={0.5} inputMode="decimal" aria-label={m.targetFor(which === 'first' ? m.firstReply : m.resolve, priorityName(priority))} value={values[priority][which]}
        aria-invalid={error ? true : undefined} aria-describedby={error ? `${id}-${priority}-${which}` : undefined}
        onChange={(event) => { feedback.touch(); setValues((all) => ({ ...all, [priority]: { ...all[priority], [which]: event.target.value } })); }} />
      <span>{m.hoursUnit}</span></span>
      {error && <span className="error-copy field-note" id={`${id}-${priority}-${which}`}>{error}</span>}</td>;
  };
  return <Card id={id} title={m.targetsHeading} lead={m.targetsLead}>
    <form className="stacked-form" noValidate onSubmit={(event) => {
      event.preventDefault();
      setChecked(true);
      const bad = priorities.some((priority) => !halfHours(values[priority].first) || !halfHours(values[priority].resolve)) || !(warn.trim() !== '' && Number(warn) >= 0 && Number.isInteger(Number(warn) * 2));
      if (bad || feedback.pending) return;
      save.mutate({ ...withoutStamp(doc), warn_before_minutes: toMinutes(warn) ?? 0,
        targets: Object.fromEntries(priorities.map((priority) => [priority, { first_response_minutes: toMinutes(values[priority].first), resolution_minutes: toMinutes(values[priority].resolve) }])) as ResponseTargets['targets'] });
    }}>
      <table className="tg-table">
        <thead><tr><th scope="col">{m.priority}</th><th scope="col">{m.firstReply}</th><th scope="col">{m.resolve}</th></tr></thead>
        <tbody>{priorities.map((priority) => <tr key={priority}><th scope="row">{priorityName(priority)}</th>{cell(priority, 'first', 'first_response_minutes')}{cell(priority, 'resolve', 'resolution_minutes')}</tr>)}</tbody>
      </table>
      <div className={`field${warnBad || server.warn_before_minutes ? ' field-bad' : ''}`}>
        <span className="tg-cell"><label htmlFor={`${id}-warn`}>{m.dueSoon}</label>
          <input id={`${id}-warn`} type="number" min={0} step={0.5} inputMode="decimal" aria-label={m.dueSoonLabel} value={warn}
            onChange={(event) => { feedback.touch(); setWarn(event.target.value); }} /><span>{m.dueSoonUnit(Number(warn))}</span></span>
        {(warnBad || server.warn_before_minutes) && <span className="error-copy field-note">{server.warn_before_minutes ?? m.halfHours}</span>}
      </div>
      <p className="muted-copy">{m.targetsNote}</p>
      <span className="button-row"><button className="primary-action" type="submit" disabled={feedback.pending}>{feedback.saveLabel(m.saveTargets)}</button>{feedback.status}</span>
    </form>
  </Card>;
}

function AppliesCard({ doc, canManage, channels }: CardProps & { channels: { id: string; name: string }[] }) {
  const every = channels.map((item) => item.id);
  // The channels as one value, so a new list of the same channels does not reset the form.
  const everyKey = every.join(',');
  const [chosen, setChosen] = useState<string[]>(doc.channel_ids ?? every);
  useEffect(() => setChosen(doc.channel_ids ?? everyKey.split(',').filter(Boolean)), [doc.channel_ids, everyKey]);
  const [checked, setChecked] = useState(false);
  const save = useSave();
  const feedback = useSaveFeedback(saveState(save));
  const id = useId();
  if (!canManage) return <Card id={id} title={m.appliesHeading} lead={m.appliesLead}><ul className="muted-copy">{channels.filter((item) => chosen.includes(item.id)).map((item) => <li key={item.id}>{item.name}</li>)}</ul></Card>;
  return <Card id={id} title={m.appliesHeading} lead={m.appliesLead}>
    <form className="stacked-form" noValidate onSubmit={(event) => {
      event.preventDefault();
      setChecked(true);
      if (chosen.length === 0 || feedback.pending) return;
      // Every channel is null: a channel added later is measured too.
      save.mutate({ ...withoutStamp(doc), channel_ids: chosen.length === every.length ? null : chosen });
    }}>
      <fieldset className="tg-channels"><legend className="sr-only">{m.appliesHeading}</legend>
        {channels.map((item) => <label className="check-row" key={item.id}><input type="checkbox" checked={chosen.includes(item.id)}
          onChange={(event) => { feedback.touch(); setChosen((all) => (event.target.checked ? [...all, item.id] : all.filter((other) => other !== item.id))); }} /><span>{item.name}</span></label>)}
      </fieldset>
      {checked && chosen.length === 0 && <p className="error-copy" role="alert">{m.pickOne}</p>}
      <span className="button-row"><button className="primary-action" type="submit" disabled={feedback.pending}>{feedback.saveLabel(m.saveApplies)}</button>{feedback.status}</span>
    </form>
  </Card>;
}

// Alerts: whether reminders and overdue alerts go out by email (the workspace's), and to the reader.
function AlertsCard({ doc, canManage }: CardProps) {
  const client = useQueryClient();
  const save = useSave();
  const feedback = useSaveFeedback(saveState(save));
  const mine = useMutation({
    mutationFn: (email: boolean) => responseTargetsApi.setMyAlerts(email),
    onSuccess: (result) => client.setQueryData<ResponseTargets>(['response-targets'], (current) => (current ? { ...current, my_alert_email: result.email } : current)),
  });
  const mineFeedback = useSaveFeedback(saveState(mine));
  const id = useId();
  const workspaceOn = save.isPending ? Boolean(save.variables?.alert_email) : doc.alert_email;
  const meOn = mine.isPending ? Boolean(mine.variables) : doc.my_alert_email !== false;
  return <Card id={id} title={m.alertsHeading} lead={m.alertsLead}>
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
