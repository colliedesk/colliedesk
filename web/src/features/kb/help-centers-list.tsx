import { Fragment, useId, useState } from 'react';
import type { FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ConflictError } from '../../api/client';
import type { RequestBody } from '../../api/client';
import { useCreateMutation } from '../../api/use-create';
import { messages } from '../../messages';
import { helpCenterName, helpCentersApi, helpCentersKey } from './help-centers-api';
import type { HelpCenterSummary } from './help-centers-api';
import { helpCentersMessages as text } from './help-centers-messages';
import { reason } from './kb-tree';

// HelpCentersList lists the workspace's help centers, the primary first; admins add one by name and
// handle, turn the others on or off, and delete any but the primary, which the server refuses to delete.
// The primary is turned on in its own settings below the list.
export function HelpCentersList({ canManage }: { canManage: boolean }) {
  const list = useQuery({ queryKey: helpCentersKey, queryFn: helpCentersApi.list });
  const [adding, setAdding] = useState(false);
  const [added, setAdded] = useState(false);
  if (list.isError) return <p role="alert" className="error-copy">{text.loadFailed}</p>;
  if (!list.data) return <p role="status" className="muted-copy">{messages.loading}</p>;
  const { help_centers: centers, limit } = list.data;
  const primary = centers.find((center) => center.primary);
  return <>
    <ul className="hc-list" aria-label={text.heading}>
      {centers.map((center) => <HelpCenterRow key={center.id} center={center} canManage={canManage} />)}
    </ul>
    {added && <p role="status" className="muted-copy">{text.createdNext}</p>}
    {canManage && (centers.length >= limit
      ? <p className="muted-copy">{text.atLimit(limit)}</p>
      : adding
        ? <NewHelpCenterForm home={primary?.url ?? ''} locale={primary?.locale ?? 'en'} onDone={(made) => { setAdding(false); setAdded(made); }} />
        : <span className="button-row"><button type="button" onClick={() => { setAdded(false); setAdding(true); }}>{text.newHelpCenter}</button></span>)}
  </>;
}

function HelpCenterRow({ center, canManage }: { center: HelpCenterSummary; canManage: boolean }) {
  const client = useQueryClient();
  const [confirming, setConfirming] = useState(false);
  const refresh = () => client.invalidateQueries({ queryKey: helpCentersKey });
  const remove = useMutation({ mutationFn: () => helpCentersApi.remove(center.id), onSuccess: refresh });
  const toggle = useMutation({ mutationFn: () => helpCentersApi.update(center.id, { enabled: !center.enabled }), onSuccess: refresh });
  const failed = remove.error ?? toggle.error;
  const name = helpCenterName(center, text.primaryName);
  return <li className="hc-list-row">
    <span className="hc-list-name"><strong>{name}</strong>{center.primary && <span className="hc-list-primary">{text.primary}</span>}
      <span className="saved-state" data-on={center.enabled ? 'true' : 'false'}>{center.enabled ? text.on : text.off}</span></span>
    <a className="muted-copy" href={center.url} target="_blank" rel="noopener noreferrer">{center.url}</a>
    {failed && (servingChannels(failed)
      ? <ServedBy channels={servingChannels(failed)!} />
      : <p role="alert" className="error-copy">{reason(failed, text.actionFailed)}</p>)}
    {canManage && !center.primary && (confirming
      ? <div role="group" aria-label={text.deleteName(name)} className="kb-confirm">
          <span>{text.confirmDelete(name)}</span>
          <span className="button-row">
            <button type="button" className="danger-action" disabled={remove.isPending} onClick={() => remove.mutate()}>{text.deleteButton}</button>
            <button type="button" disabled={remove.isPending} onClick={() => setConfirming(false)}>{text.cancel}</button>
          </span>
        </div>
      : <span className="button-row">
          <button type="button" disabled={toggle.isPending} aria-label={center.enabled ? text.turnOffName(name) : text.turnOnName(name)}
            onClick={() => { remove.reset(); toggle.mutate(); }}>{center.enabled ? text.turnOff : text.turnOn}</button>
          <button type="button" className="danger-action" aria-label={text.deleteName(name)} onClick={() => { toggle.reset(); setConfirming(true); }}>{text.delete}</button>
        </span>)}
    {!center.primary && <a className="button-link" href={`/knowledge/settings/help-centers/${center.id}`} aria-label={text.settingsOf(name)}>{text.settings}</a>}
  </li>;
}

interface ServingChannel { id: string; name: string }

// servingChannels reads the channels that still serve a help center from a refused delete (409
// help_center_in_use); undefined for any other error.
function servingChannels(error: unknown): ServingChannel[] | undefined {
  if (!(error instanceof ConflictError) || error.detail?.code !== 'help_center_in_use' || !Array.isArray(error.detail.channels)) return undefined;
  return (error.detail.channels as unknown[]).filter((item): item is ServingChannel =>
    typeof item === 'object' && item !== null && typeof (item as ServingChannel).id === 'string' && typeof (item as ServingChannel).name === 'string');
}

// ServedBy names each channel a delete was refused for, linked to its settings, where its help center is chosen.
function ServedBy({ channels }: { channels: ServingChannel[] }) {
  return <p role="alert" className="error-copy">
    {channels.map((channel, index) => <Fragment key={channel.id}>
      {index > 0 && (index === channels.length - 1 ? text.and : ', ')}
      <a href={`/settings/channels/${channel.id}`}>{channel.name}</a>
    </Fragment>)}
    {text.servedBy(channels.length)}
  </p>;
}

function NewHelpCenterForm({ home, locale, onDone }: { home: string; locale: string; onDone: (made: boolean) => void }) {
  const id = useId();
  const client = useQueryClient();
  const [name, setName] = useState('');
  const [handle, setHandle] = useState('');
  const create = useCreateMutation({
    create: (value: RequestBody<'createHelpCenter'>, key: string) => helpCentersApi.create(value, key),
    onSuccess: async () => { await client.invalidateQueries({ queryKey: helpCentersKey }); onDone(true); },
  });
  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (!create.isPending && name.trim() && handle.trim()) create.mutate({ name: name.trim(), handle: handle.trim(), locale });
  };
  return <form className="stacked-form" aria-label={text.newHelpCenter} onSubmit={submit}>
    {create.isError && <p role="alert" className="error-copy">{reason(create.error, text.actionFailed)}</p>}
    <div className="field"><label htmlFor={`${id}-name`}>{text.name}</label>
      <input id={`${id}-name`} autoFocus required maxLength={100} value={name} aria-describedby={`${id}-name-help`} onChange={(event) => setName(event.target.value)} />
      <small id={`${id}-name-help`} className="muted-copy">{text.nameHelp}</small></div>
    <div className="field"><label htmlFor={`${id}-handle`}>{text.handle}</label>
      <input id={`${id}-handle`} required maxLength={40} value={handle} aria-describedby={`${id}-handle-help`} onChange={(event) => setHandle(event.target.value.toLowerCase())} />
      <small id={`${id}-handle-help`} className="muted-copy">{text.handleHelp} <span>{`${home}/${handle.trim() || '…'}`}</span></small></div>
    <span className="button-row">
      <button className="primary-action" type="submit" disabled={create.isPending || !name.trim() || !handle.trim()}>{create.isPending ? text.creating : text.create}</button>
      <button type="button" disabled={create.isPending} onClick={() => onDone(false)}>{text.cancel}</button>
    </span>
  </form>;
}
