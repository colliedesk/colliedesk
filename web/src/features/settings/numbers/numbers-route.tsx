import { useEffect, useId, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, InvalidRequestError } from '../../../api/client';
import { codeOf, numberSeriesApi, seriesName, yearIn } from '../../../api/number-series';
import type { NumberSeries, NumberSeriesChange } from '../../../api/number-series';
import { Dialog, Field } from '../../../ui/dialog';
import { PlusIcon } from '../../../ui/icons';
import { useBillingShown } from '../../billing/billing-shown';
import { SettingsNav } from '../settings-nav';
import type { Shown } from '../settings-nav';
import { numbersMessages as m } from './numbers-messages';
import './numbers.css';

// Settings → Conversation numbers: the workspace's number series, each with its code format and next
// number. Admins add and change them; agents read them.

const formatShape = /^[A-Za-z0-9#\-_/.]*(\{yyyy\}[A-Za-z0-9#\-_/.]*)?\{n(:0[2-8])?\}[A-Za-z0-9#\-_/.]*(\{yyyy\}[A-Za-z0-9#\-_/.]*)?$/;
const whole = (value: string) => (/^\d+$/.test(value.trim()) ? Number(value.trim()) : Number.NaN);

function fieldErrors(error: unknown): Record<string, string> {
  if (!(error instanceof InvalidRequestError)) return {};
  const fields = error.detail?.fields;
  return fields && typeof fields === 'object' ? Object.fromEntries(Object.entries(fields).filter((entry): entry is [string, string] => typeof entry[1] === 'string')) : {};
}

export function NumbersRoute() {
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const isAdmin = me.data?.agent.role === 'admin';
  const billingShown = useBillingShown(isAdmin);
  const timezone = me.data?.timezone ?? 'UTC';
  const shown: Shown = { general: isAdmin, team: isAdmin, channels: true, savedReplies: true, exportData: isAdmin, billing: billingShown, importData: isAdmin, ai: true };
  const series = useQuery({ queryKey: ['number-series'], queryFn: numberSeriesApi.list });
  const [editing, setEditing] = useState<NumberSeries | 'new'>();
  const [done, setDone] = useState<string>();
  const doneNote = useRef<HTMLParagraphElement>(null);
  useEffect(() => { if (done) doneNote.current?.focus(); }, [done]);
  const year = yearIn(timezone);
  const label = (item: NumberSeries) => (item.series === 'default' ? m.defaultName : item.series);
  return <main className="settings-page panel">
    <header><h1>{m.heading}</h1><p>{m.description}</p></header>
    <div className="settings-layout">
      <SettingsNav page="/settings/numbers" shown={shown} />
      <div className="settings-sections">
        <section className="settings-card" aria-labelledby="numbers-heading">
          <div className="section-heading"><h2 id="numbers-heading">{m.listLabel}</h2><p>{m.lead}</p></div>
          <div className="settings-body">
            {me.data && !isAdmin && <p className="notice notice-plain" role="note"><span>{m.adminsOnly}</span></p>}
            {series.isPending ? <div className="num-loading" aria-busy="true"><span className="sr-only">{m.loading}</span></div>
              : series.isError ? <p role="alert" className="error-copy">{m.loadFailed} <button type="button" className="link-action" onClick={() => void series.refetch()}>{m.tryAgain}</button></p>
              : <ul className="num-rows" aria-label={m.listLabel}>
                  {series.data.items.map((item) => <li className="num-row" key={item.series}>
                    <div><strong>{label(item)}</strong><small className="num-mono">{item.format}</small></div>
                    <div><span className="num-mono">{m.next(codeOf(item.format, item.next, year))}</span>
                      {item.start > 1 && <small>{m.takenOver(item.start, item.start_year)}</small>}</div>
                    <div className="num-actions">{isAdmin && <button type="button" aria-label={m.changeName(label(item))} onClick={() => { setDone(undefined); setEditing(item); }}>{m.change}</button>}</div>
                  </li>)}
                </ul>}
            {isAdmin && series.isSuccess && <span className="button-row"><button type="button" onClick={() => { setDone(undefined); setEditing('new'); }}><PlusIcon />{m.add}</button></span>}
            {done && <p className="notice notice-plain" role="status" tabIndex={-1} ref={doneNote}><span>{done}</span></p>}
          </div>
        </section>
      </div>
    </div>
    {editing && <SeriesDialog current={editing === 'new' ? undefined : editing} taken={series.data?.items.map((item) => item.series) ?? []} year={year}
      onClose={() => setEditing(undefined)} onSaved={(name) => { setEditing(undefined); setDone(m.saved(name)); }} />}
  </main>;
}

// SeriesDialog adds a series (its name and format at least) or changes one: only what changed is sent.
function SeriesDialog({ current, taken, year, onClose, onSaved }: { current?: NumberSeries; taken: string[]; year: number; onClose: () => void; onSaved: (name: string) => void }) {
  const client = useQueryClient();
  const [name, setName] = useState(current?.series ?? '');
  const [format, setFormat] = useState(current?.format ?? '');
  const [next, setNext] = useState(String(current?.next ?? 1));
  const [start, setStart] = useState(current && current.start > 1 ? String(current.start) : '');
  const [startYear, setStartYear] = useState(current?.start_year ? String(current.start_year) : '');
  const [checked, setChecked] = useState(false);
  const id = useId();
  const change = (): NumberSeriesChange => {
    const body: NumberSeriesChange = {};
    if (!current || format.trim() !== current.format) body.format = format.trim();
    if (!current || whole(next) !== current.next) body.next = whole(next);
    if (start.trim() && (!current || whole(start) !== current.start)) body.start = whole(start);
    if (startYear.trim() && (!current || whole(startYear) !== current.start_year)) body.start_year = whole(startYear);
    return body;
  };
  const save = useMutation({
    mutationFn: () => numberSeriesApi.set(current?.series ?? name.trim(), change()),
    onSuccess: async (saved) => { await client.invalidateQueries({ queryKey: ['number-series'] }); onSaved(saved.series === 'default' ? m.defaultName : saved.series); },
  });
  const server = fieldErrors(save.error);
  const least = current?.next ?? 1;
  const local = {
    name: current ? undefined : !name.trim() ? m.required : !seriesName.test(name.trim()) ? m.nameBad : taken.includes(name.trim()) ? m.nameTaken : undefined,
    format: !format.trim() ? m.required : !formatShape.test(format.trim()) ? m.formatBad : undefined,
    next: !(whole(next) >= 1) ? m.wholeNumber : whole(next) < least ? m.nextLow(least) : undefined,
    start: start.trim() && !(whole(start) >= 1) ? m.wholeNumber : undefined,
    start_year: startYear.trim() && !(whole(startYear) >= 1970 && whole(startYear) <= 9999) ? m.yearBad : undefined,
  };
  const error = (field: keyof typeof local) => (checked ? local[field] : undefined) ?? server[field === 'name' ? 'series' : field];
  const preview = !local.format && whole(next) >= 1 ? codeOf(format.trim(), whole(next), year) : undefined;
  const nothing = current && Object.keys(change()).length === 0;
  return <Dialog title={current ? m.editTitle(current.series === 'default' ? m.defaultName : current.series) : m.addTitle} help={current ? m.editHelp : undefined} closeLabel={m.close} onClose={onClose}
    onSubmit={() => {
      setChecked(true);
      if (Object.values(local).some(Boolean) || save.isPending) return;
      if (nothing) { onClose(); return; }
      save.mutate();
    }}
    footer={<><button type="button" onClick={onClose}>{m.cancel}</button><button type="submit" className="primary-action" disabled={save.isPending}>{save.isPending ? m.saving : m.save}</button></>}>
    {!current && <Field id={`${id}-name`} label={m.name} error={error('name')} help={m.nameHint}>{(props) => <input {...props} className="num-mono" autoFocus autoComplete="off" maxLength={20}
      value={name} onChange={(event) => { save.reset(); setName(event.target.value); }} />}</Field>}
    <Field id={`${id}-format`} label={m.format} error={error('format')} help={m.formatHint}>{(props) => <input {...props} className="num-mono" autoFocus={Boolean(current)} autoComplete="off" maxLength={40}
      placeholder="REF-{yyyy}-{n:04}" value={format} onChange={(event) => { save.reset(); setFormat(event.target.value); }} />}</Field>
    <Field id={`${id}-next`} label={m.nextNumber} error={error('next')} help={preview ? m.nextHint(preview) : undefined}>{(props) => <input {...props} type="number" min={least} step={1} inputMode="numeric"
      value={next} onChange={(event) => { save.reset(); setNext(event.target.value); }} />}</Field>
    <div className="num-pair">
      <Field id={`${id}-start`} label={m.start} error={error('start')} help={m.startHint}>{(props) => <input {...props} type="number" min={1} step={1} inputMode="numeric"
        value={start} onChange={(event) => { save.reset(); setStart(event.target.value); }} />}</Field>
      <Field id={`${id}-year`} label={m.startYear} error={error('start_year')}>{(props) => <input {...props} type="number" min={1970} max={9999} step={1} inputMode="numeric"
        value={startYear} onChange={(event) => { save.reset(); setStartYear(event.target.value); }} />}</Field>
    </div>
    {save.isError && Object.keys(server).length === 0 && <p role="alert" className="error-copy">{save.error instanceof InvalidRequestError ? save.error.message : m.failed}</p>}
  </Dialog>;
}
