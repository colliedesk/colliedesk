import { useId, useState } from 'react';
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
  const [edited, setEdited] = useState(false);
  const save = useMutation({
    mutationFn: (origins: string[]) => create({ ...(name.trim() ? { name: name.trim() } : {}), enabled: true, config: { allowed_origins: origins } }),
    onMutate: () => setEdited(false),
  });
  const errorID = useId();
  const hintID = useId();
  const state = saveState(save);
  const taken = save.error instanceof ConflictError && save.error.detail?.code === 'channel_name_taken' && !edited ? state : undefined;
  const nameField = <>
    <label className="field"><span>{formMessages.name}</span>
      <input value={name} maxLength={80} aria-invalid={taken ? true : undefined} aria-describedby={taken ? `${errorID} ${hintID}` : hintID}
        onChange={(event) => { setEdited(true); setName(event.target.value); }} /></label>
    <small id={hintID} className="muted-copy">{formMessages.nameHint}</small>
    {taken && <p role="alert" id={errorID} className="error-copy">{taken.error}{' '}
      {taken.conflict && <a href={`/settings/channels/${encodeURIComponent(taken.conflict.channel_id)}`}>{formMessages.open(taken.conflict.channel_name)}</a>}</p>}
  </>;
  return <WebFormSection part="embed" canManage={canManage} save={taken ? { ...state, error: undefined } : state} nameField={nameField}
    onSave={(_enabled, origins) => save.mutate(origins)} />;
}

const formMessages = {
  name: 'Name',
  nameHint: 'Tells this form apart in the inbox and in Settings. Leave it empty to number it.',
  open: (name: string) => `Open ${name}`,
};

export const Tabs = { embed: Embed };
export { Connect };
