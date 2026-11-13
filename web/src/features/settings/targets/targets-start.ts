import type { ResponseTargets, ResponseTargetsChange } from '../../../api/response-targets';
import type { SetupProgress } from '../../../api/schemas';
import { messages } from '../../../messages';
import { hoursText, startingHours } from '../../../lib/business-hours';
import { targetText } from '../../onboarding/reply-target-time';
import { targetsMessages as m } from './targets-messages';

const priorities = ['urgent', 'high', 'normal', 'low'] as const;

// What turning measuring on starts with, and the sentence that says it. The hours and the preset targets are
// the ones the setup flow saves (the server names them in the setup progress), so the two screens agree.
// Hours or targets saved earlier are kept, never replaced: the sentence then names those.
export function turnOn(doc: ResponseTargets, progress: SetupProgress, workspaceZone: string): { change: ResponseTargetsChange; line: string } {
  const { hours, kept } = startingHours(doc.hours, progress.reply_target_hours.hours);
  const savedTargets = priorities.some((priority) => doc.targets[priority].first_response_minutes !== null || doc.targets[priority].resolution_minutes !== null);
  const wanted = progress.reply_target_suggestion?.preset ?? 'same_day';
  const preset = progress.reply_target_presets.find((item) => item.key === wanted) ?? progress.reply_target_presets[0];
  const targets = savedTargets || !preset ? doc.targets : preset.targets;
  const day = progress.reply_target_hours.day_minutes;
  const replies = priorities.map((priority) => {
    const minutes = targets[priority].first_response_minutes;
    return `${messages.priorities[priority]} ${minutes ? targetText(minutes, day) : m.noTargetShort}`;
  }).join(' · ');
  const { enabled_at: _enabled, my_alert_email: _mine, ...rest } = doc;
  // Only a workspace that never saved gets the preset's reminder; a saved 0 is its choice of none.
  const warn = doc.updated_at === null ? 30 : doc.warn_before_minutes;
  return {
    change: { ...rest, enabled: true, hours, targets, warn_before_minutes: warn },
    line: m.startsWith(kept, hoursText(hours), doc.timezone ?? workspaceZone, replies),
  };
}
