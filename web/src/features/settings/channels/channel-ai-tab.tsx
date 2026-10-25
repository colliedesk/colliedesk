import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { InvalidRequestError } from '../../../api/client';
import { aiApi } from '../../../api/ai';
import type { AILevel, AISettings, AISlotName, ChannelAI } from '../../../api/ai';
import type { ChannelDetail } from '../../../api/channels';
import { AutoDraftDialog, AutoTriageDialog } from '../ai-level-dialogs';
import { featureName, levelMeaning, levelName, levelsOf, scopeName } from '../ai-levels';
import { channelsMessages as m } from './channels-messages';

// A channel's AI tab: what each AI feature does by itself on the channel and on each scope of its type,
// over the workspace's default (docs/user/ai.md "What AI does by itself, per channel"). The levels are
// the AI settings', not the channel's: raising one asks the same as on Settings → Built-in AI.

// The features this tab sets. Intake (chat channels only) comes with its settings, later.
const features: AISlotName[] = ['triage', 'draft'];

type Change = { slot: AISlotName; scope: string; level: AILevel | null };

// configured is what a scope asks of a feature before the workspace's consent: its own level, the
// channel's, then the workspace's default.
function configured(levels: ChannelAI, settings: AISettings | undefined, scope: string, slot: AISlotName): AILevel {
  const own = (at: string) => levels.scopes.find((entry) => entry.scope === at)?.slots.find((item) => item.slot === slot)?.level ?? null;
  return own(scope) ?? (scope === '' ? null : own('')) ?? settings?.slots.find((item) => item.slot === slot)?.default_level ?? 'off';
}

export function ChannelAITab({ channel, isAdmin }: { channel: ChannelDetail; isAdmin: boolean }) {
  const client = useQueryClient();
  const levels = useQuery({ queryKey: ['channel-ai', channel.id], queryFn: () => aiApi.channel(channel.id) });
  const settings = useQuery({ queryKey: ['ai-settings'], queryFn: aiApi.settings });
  const [asking, setAsking] = useState<Change>();
  const save = useMutation({
    mutationFn: (change: Change) => aiApi.setChannel(channel.id, change.slot, { scope: change.scope, level: change.level }),
    onSuccess: (next) => {
      client.setQueryData(['channel-ai', channel.id], next);
      return client.invalidateQueries({ predicate: (query) => ['ai-settings', 'channel-events'].includes(String(query.queryKey[0])) });
    },
  });
  if (levels.isError) return <p role="alert" className="error-copy">{m.aiLoadFailed} <button type="button" className="link-action" onClick={() => void levels.refetch()}>{m.tryAgain}</button></p>;
  if (!levels.data) return <div className="ch-loading" aria-busy="true"><span className="sr-only">{m.loading}</span></div>;
  const data = levels.data;
  const slotOf = (slot: AISlotName) => settings.data?.slots.find((item) => item.slot === slot);
  // Raising triage to auto, or drafts to written by themselves, asks first, as on Built-in AI.
  const pick = (change: Change) => {
    const raises = change.level !== null && change.level !== configured(data, settings.data, change.scope, change.slot)
      && ((change.slot === 'triage' && change.level === 'auto') || (change.slot === 'draft' && change.level === 'suggest'));
    if (raises && slotOf(change.slot)) setAsking(change);
    else save.mutate(change);
  };
  const scopes = data.scopes;
  const askedSlot = asking ? slotOf(asking.slot) : undefined;
  return <div className="ch-ai">
    <p className="ch-ai-lead">{m.aiLead}</p>
    {!isAdmin && <p className="notice notice-plain" role="note"><span>{m.aiAdminsOnly}</span></p>}
    <table className="ch-ai-table">
      <thead><tr><th scope="col">{m.aiFeature}</th>{scopes.map((entry) => <th key={entry.scope} scope="col">{scopeName(entry.scope)}</th>)}</tr></thead>
      <tbody>{features.map((slot) => <tr key={slot}>
        <th scope="row"><span className="ch-ai-feature">{featureName[slot]}</span></th>
        {scopes.map((entry) => {
          const item = entry.slots.find((candidate) => candidate.slot === slot);
          if (!item) return <td key={entry.scope} data-scope={scopeName(entry.scope)} />;
          const followed = entry.scope === '' ? settings.data?.slots.find((candidate) => candidate.slot === slot)?.default_level ?? 'off'
            : configured(data, settings.data, '', slot);
          const follow = entry.scope === '' ? m.aiFollowWorkspace(levelName[followed]) : m.aiFollowChannel(levelName[followed]);
          const label = `${featureName[slot]} · ${scopeName(entry.scope)}`;
          const asked = configured(data, settings.data, entry.scope, slot);
          return <td key={entry.scope} data-scope={scopeName(entry.scope)}>
            <select aria-label={label} value={item.level ?? ''} disabled={!isAdmin || save.isPending}
              onChange={(event) => pick({ slot, scope: entry.scope, level: event.target.value === '' ? null : event.target.value as AILevel })}>
              <option value="">{follow}</option>
              {levelsOf(slot).map((level) => <option key={level} value={level}>{levelName[level]}</option>)}
            </select>
            {item.off_reason === 'workspace_off'
              ? <small className="ch-ai-why">{m.aiWorkspaceOff(featureName[slot])} {isAdmin && <a href="/settings/ai">{m.aiTurnOn}</a>}</small>
              : <small className="ch-ai-why muted-copy">{levelMeaning[slot][asked]}</small>}
          </td>;
        })}
      </tr>)}</tbody>
    </table>
    {save.isError && <p role="alert" className="error-copy">{save.error instanceof InvalidRequestError ? save.error.message : m.actionFailed}</p>}
    <p className="help">{m.aiHelp}</p>
    {asking && askedSlot?.slot === 'triage' && <AutoTriageDialog slot={askedSlot} onClose={() => setAsking(undefined)}
      onTurnOn={() => { save.mutate(asking); setAsking(undefined); }} />}
    {asking && askedSlot?.slot === 'draft' && settings.data && <AutoDraftDialog slot={askedSlot} settings={settings.data} onClose={() => setAsking(undefined)}
      onTurnOn={() => { save.mutate(asking); setAsking(undefined); }} />}
  </div>;
}
