import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { InvalidRequestError } from '../../../api/client';
import { aiApi } from '../../../api/ai';
import { intakeApi } from '../../../api/intake';
import type { AILevel, AISettings, AISlotName, ChannelAI, ChannelAIInput } from '../../../api/ai';
import type { ChannelDetail } from '../../../api/channels';
import { formatDay } from '../../../lib/format-date';
import { AutoDraftDialog, AutoTriageDialog } from '../ai-level-dialogs';
import { featureName, levelMeaning, levelName, levelsOf, scopeName } from '../ai-levels';
import { IntakeSettings } from './channel-intake-settings';
import { IntakeAutoDialog } from './intake-auto-dialog';
import { channelsMessages as m } from './channels-messages';

// A channel's AI tab: what each AI feature does by itself on the channel and on each scope of its type,
// over the workspace's default (docs/user/ai.md "What AI does by itself, per channel"). The levels are
// the AI settings', not the channel's: raising one asks the same as on Settings → Built-in AI.

// The features this tab sets; intake shows only on a chat type's intake scopes, with its settings.
const allFeatures: AISlotName[] = ['triage', 'draft', 'intake'];

type Change = { slot: AISlotName; scope: string; level: AILevel | null };

// configured is what a scope asks of a feature before the workspace's consent: its own level, the
// channel's, then the workspace's default.
function configured(levels: ChannelAI, settings: AISettings | undefined, scope: string, slot: AISlotName): AILevel {
  const own = (at: string) => levels.scopes.find((entry) => entry.scope === at)?.slots.find((item) => item.slot === slot)?.level ?? null;
  return own(scope) ?? (scope === '' ? null : own('')) ?? settings?.slots.find((item) => item.slot === slot)?.default_level ?? 'off';
}

export function ChannelAITab({ channel, isAdmin, timezone = 'UTC' }: { channel: ChannelDetail; isAdmin: boolean; timezone?: string }) {
  const client = useQueryClient();
  const levels = useQuery({ queryKey: ['channel-ai', channel.id], queryFn: () => aiApi.channel(channel.id) });
  const settings = useQuery({ queryKey: ['ai-settings'], queryFn: aiApi.settings });
  const [asking, setAsking] = useState<Change>();
  const [intakeProblem, setIntakeProblem] = useState<string>();
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
  // What a scope follows when its own level goes: the channel's, or for the channel the workspace's.
  const followed = (scope: string, slot: AISlotName): AILevel => (scope === '' ? slotOf(slot)?.default_level ?? 'off' : configured(data, settings.data, '', slot));
  // Raising triage to auto, drafts to written by themselves or intake to deciding alone asks first, as on
  // Built-in AI, also when the scope gets there by following a default that is there.
  const pick = (change: Change) => {
    const target = change.level ?? followed(change.scope, change.slot);
    const raises = target !== configured(data, settings.data, change.scope, change.slot)
      && ((change.slot === 'triage' && target === 'auto') || (change.slot === 'draft' && target === 'suggest') || (change.slot === 'intake' && target === 'auto'));
    if (raises && slotOf(change.slot)) setAsking(change);
    else save.mutate(change);
  };
  // Intake's level and settings go in one change: the server takes the settings whole. A refusal rejects:
  // the settings form says it under itself, the Auto dialog (gone by then) through the tab.
  const saveIntake = async (input: ChannelAIInput) => {
    const next = await aiApi.setChannel(channel.id, 'intake', input);
    client.setQueryData(['channel-ai', channel.id], next);
    await client.invalidateQueries({ predicate: (query) => ['ai-settings', 'channel-events', 'intake-readiness'].includes(String(query.queryKey[0])) });
  };
  const turnOnAuto = async (input: ChannelAIInput) => {
    setIntakeProblem(undefined);
    try {
      await saveIntake(input);
    } catch (error) {
      setIntakeProblem(error instanceof InvalidRequestError ? error.message : m.actionFailed);
    }
  };
  const scopes = data.scopes;
  const features = allFeatures.filter((slot) => scopes.some((entry) => entry.slots.some((item) => item.slot === slot)));
  // Intake's settings show where it does more than the channel's rules.
  const intakeScopes = scopes.flatMap((entry) => {
    const item = entry.slots.find((candidate) => candidate.slot === 'intake');
    return item && configured(data, settings.data, entry.scope, 'intake') !== 'off' ? [{ scope: entry.scope, item }] : [];
  });
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
          const inherited = followed(entry.scope, slot);
          const follow = entry.scope === '' ? m.aiFollowWorkspace(levelName[inherited]) : m.aiFollowChannel(levelName[inherited]);
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
    {scopes.flatMap((entry) => {
      const lowered = entry.slots.find((item) => item.slot === 'intake')?.lowered;
      return lowered ? [<p key={entry.scope} className="notice ch-intake-lowered" role="status"><span>{m.intakeLowered(scopeName(entry.scope),
        formatDay(lowered.at, timezone), m.intakeLoweredWhy[lowered.reason]())}</span>
        {isAdmin && <span className="notice-actions"><button type="button" onClick={() => pick({ slot: 'intake', scope: entry.scope, level: 'auto' })}>{m.intakeTurnAutoBack}</button></span>}</p>] : [];
    })}
    {intakeScopes.length > 0 && <IntakeWeek channelID={channel.id} scope={intakeScopes[0].scope} />}
    {intakeScopes.map(({ scope, item }) => <IntakeSettings key={scope} scope={scope} level={item.level} options={item.options}
      settings={settings.data} isAdmin={isAdmin} onSave={saveIntake} />)}
    {intakeProblem && <p role="alert" className="error-copy">{intakeProblem}</p>}
    {save.isError && <p role="alert" className="error-copy">{save.error instanceof InvalidRequestError ? save.error.message : m.actionFailed}</p>}
    <p className="help">{m.aiHelp}</p>
    {asking && asking.slot === 'intake' && <IntakeAutoDialog channelID={channel.id} scope={asking.scope}
      options={data.scopes.find((entry) => entry.scope === asking.scope)?.slots.find((item) => item.slot === 'intake')?.options ?? null} settings={settings.data}
      onClose={() => setAsking(undefined)} onTurnOn={(input) => { setAsking(undefined); void turnOnAuto(input); }} />}
    {asking && askedSlot?.slot === 'triage' && <AutoTriageDialog slot={askedSlot} onClose={() => setAsking(undefined)}
      onTurnOn={() => { save.mutate(asking); setAsking(undefined); }} />}
    {asking && askedSlot?.slot === 'draft' && settings.data && <AutoDraftDialog slot={askedSlot} settings={settings.data} onClose={() => setAsking(undefined)}
      onTurnOn={() => { save.mutate(asking); setAsking(undefined); }} />}
  </div>;
}

// IntakeWeek is how intake went on the channel in the last 7 days (the counts of Review), with the way to
// its cards.
function IntakeWeek({ channelID, scope }: { channelID: string; scope: string }) {
  const stats = useQuery({ queryKey: ['intake-stats', channelID], queryFn: () => intakeApi.stats(channelID) });
  const href = `/review?channel=${channelID}&scope=${scope}`;
  if (stats.isError) return null;
  const data = stats.data;
  return <section className="ch-intake-week" aria-label={m.intakeWeek}>
    <strong>{m.intakeWeek}</strong>
    <span>{!data ? m.loading : data.gatherings === 0 ? m.intakeWeekNone
      : m.intakeWeekLine(data.gatherings, data.decided_by.ai ?? 0, data.ai_corrected, data.ai_decisions, data.by_state.review ?? 0)}</span>
    <a href={href}>{m.intakeOpenReview}</a>
  </section>;
}
