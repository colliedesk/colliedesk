import { useMutation, useQueryClient } from '@tanstack/react-query';
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
function Connect({ canManage, create }: ConnectProps) {
  const save = useMutation({ mutationFn: (origins: string[]) => create({ enabled: true, config: { allowed_origins: origins } }) });
  return <WebFormSection part="embed" canManage={canManage} save={saveState(save)} onSave={(_enabled, origins) => save.mutate(origins)} />;
}

export const Tabs = { embed: Embed };
export { Connect };
