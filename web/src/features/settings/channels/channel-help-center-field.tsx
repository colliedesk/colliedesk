import { useId } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { InvalidRequestError } from '../../../api/client';
import { channelsApi } from '../../../api/channels';
import type { ChannelDetail } from '../../../api/channels';
import { helpCenterName, helpCentersApi, helpCentersKey } from '../../kb/help-centers-api';
import { helpCentersMessages } from '../../kb/help-centers-messages';
import { channelsMessages as m } from './channels-messages';

// ChannelHelpCenterField chooses the help center a channel serves (a help chat or a web form: the
// channels whose help_center_name is set). A workspace with one help center has nothing to choose.
// The primary is saved as null, which follows whichever help center is primary.
export function ChannelHelpCenterField({ channel }: { channel: ChannelDetail }) {
  const id = useId();
  const client = useQueryClient();
  const serves = channel.help_center_name != null;
  const list = useQuery({ queryKey: helpCentersKey, queryFn: helpCentersApi.list, enabled: serves });
  const move = useMutation({
    mutationFn: (helpCenterID: string | null) => channelsApi.update(channel.id, { help_center_id: helpCenterID }),
    onSuccess: () => client.invalidateQueries({ predicate: (query) => ['channels', 'channel', 'channel-events'].includes(String(query.queryKey[0])) }),
  });
  const centers = list.data?.help_centers ?? [];
  if (!serves || centers.length < 2) return null;
  const primary = centers.find((center) => center.primary);
  const value = channel.help_center_id ?? primary?.id ?? '';
  return <div className="ch-help-center">
    <label className="field" htmlFor={id}><span>{m.helpCenterLabel}</span>
      <select id={id} value={value} disabled={move.isPending} aria-describedby={`${id}-help`}
        onChange={(event) => move.mutate(event.target.value === primary?.id ? null : event.target.value)}>
        {centers.map((center) => <option key={center.id} value={center.id}>{helpCenterName(center, helpCentersMessages.primaryName)}</option>)}
      </select></label>
    <small id={`${id}-help`} className="muted-copy">{m.helpCenterHelp}</small>
    {move.isError && <p role="alert" className="error-copy">{move.error instanceof InvalidRequestError ? move.error.message : m.actionFailed}</p>}
  </div>;
}
