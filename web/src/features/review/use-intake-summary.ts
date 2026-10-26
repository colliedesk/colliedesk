import { useQuery } from '@tanstack/react-query';
import { intakeSummary } from '../../api/intake-summary';

// The same poll interval as agent requests: an open tab learns of a card waiting in Review within 20 seconds.
const pollMilliseconds = 20_000;

export const intakeSummaryKey = ['intake-summary'] as const;

// useIntakeSummary is whether the workspace has a chat channel that collects messages, how many cards wait
// for a person, and the channels and scopes to filter by; shared by the rail link and the Review page.
export function useIntakeSummary(enabled = true) {
  return useQuery({ queryKey: intakeSummaryKey, queryFn: intakeSummary, enabled, refetchInterval: pollMilliseconds, refetchIntervalInBackground: true });
}
