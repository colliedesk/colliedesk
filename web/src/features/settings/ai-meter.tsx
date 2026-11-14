import type { AISettings } from '../../api/ai';
import { formatDay } from '../../lib/format-date';
import { aiMessages as m } from './ai-messages';
import { money } from './ai-level-dialogs';

export function Meter({ allowance }: { allowance: NonNullable<AISettings['allowance']> }) {
  if (allowance.limit_micro_usd === 0) return <div className="ai-meter"><strong className="ai-meter-none">{m.noneIncluded}</strong></div>;
  const used = Math.min(allowance.used_micro_usd, allowance.limit_micro_usd);
  const full = allowance.used_micro_usd >= allowance.limit_micro_usd;
  const percent = allowance.limit_micro_usd > 0 ? Math.round((used / allowance.limit_micro_usd) * 100) : 100;
  return <div className="ai-meter">
    <div className="ai-meter-top"><strong className={full ? 'error-copy' : undefined}>{m.allowanceUsed(money(allowance.used_micro_usd), money(allowance.limit_micro_usd))}</strong>
      <span>{m.resets(formatDay(allowance.resets_at, 'UTC'))}</span></div>
    <div className={`ai-bar${full ? ' ai-bar-full' : ''}`} role="meter" aria-label={m.includedUsed} aria-valuemin={0} aria-valuemax={allowance.limit_micro_usd}
      aria-valuenow={used} aria-valuetext={m.allowanceUsed(money(allowance.used_micro_usd), money(allowance.limit_micro_usd))}><i style={{ width: `${percent}%` }} /></div>
    {allowance.drafts_left !== null && !full && <span className="ai-meter-left">{m.draftsLeft(allowance.drafts_left)}</span>}
  </div>;
}
