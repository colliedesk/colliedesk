import { useEffect, useId, useRef, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { ConflictError } from '../../api/client';
import { channelsApi } from '../../api/channels';
import type { WebFormSettings } from '../../api/schemas';
import { WebFormSection } from '../../features/settings/web-form-section';
import { saveState } from '../../features/settings/save-feedback';
import type { ChannelPanelProps, ConnectProps } from '../registry';

// Embed changes only the allowed origins: the channel's switch is the one place that turns the form on or off.
function Embed({ channel, canManage }: ChannelPanelProps) {
  const client = useQueryClient();
  const save = useMutation({
    mutationFn: (origins: string[]) => channelsApi.update(channel.id, { config: { allowed_origins: origins } }),
    onSuccess: () => Promise.all([client.invalidateQueries({ queryKey: ['channels'] }), client.invalidateQueries({ queryKey: ['web-form'] })]),
  });
  return <WebFormSection part="embed" settings={channel.config as WebFormSettings} canManage={canManage} save={saveState(save)}
    onSave={(_enabled, origins) => save.mutate(origins)} />;
}

// Connect adds the form, on, with the sites allowed to post to it.
// A name left empty is the type's (Web form, Web form 2, …); one another form has is refused at the field.
function Connect({ canManage, create }: ConnectProps) {
  const [name, setName] = useState('');
  // The name a refusal is about: its words show while the field holds it.
  const [sent, setSent] = useState('');
  const save = useMutation({
    mutationFn: (origins: string[]) => create({ ...(name.trim() ? { name: name.trim() } : {}), enabled: true, config: { allowed_origins: origins } }),
    onMutate: () => setSent(name),
  });
  const errorID = useId();
  const hintID = useId();
  const state = saveState(save);
  // A refused name is said at the name, never by the origins' Save.
  const nameRefused = save.error instanceof ConflictError && save.error.detail?.code === 'channel_name_taken';
  const taken = nameRefused && name === sent ? state : undefined;
  const field = useRef<HTMLInputElement>(null);
  const refused = Boolean(taken);
  useEffect(() => { if (refused) field.current?.focus(); }, [refused]);
  const nameField = <>
    <label className="field"><span>{formMessages.name}</span>
      <input ref={field} value={name} maxLength={80} aria-invalid={taken ? true : undefined} aria-describedby={taken ? `${errorID} ${hintID}` : hintID}
        onChange={(event) => setName(event.target.value)} /></label>
    <small id={hintID} className="muted-copy">{formMessages.nameHint}</small>
    {taken && <p role="alert" id={errorID} className="error-copy">{taken.error}{' '}
      {taken.conflict && <a href={`/settings/channels/${encodeURIComponent(taken.conflict.channel_id)}`}>{formMessages.open(taken.conflict.channel_name)}</a>}</p>}
  </>;
  return <WebFormSection part="embed" canManage={canManage} save={nameRefused ? { ...state, error: undefined } : state} nameField={nameField}
    onSave={(_enabled, origins) => save.mutate(origins)} />;
}

const formMessages = {
  name: 'Name',
  nameHint: 'Tells this form apart in the inbox and in Settings. Leave it empty to number it.',
  open: (name: string) => `Open ${name}`,
};

export const Tabs = { embed: Embed };
export { Connect };
