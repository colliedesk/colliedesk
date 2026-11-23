import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { resultsApi } from '../../api/results';
import type { Results, WindowKey } from '../../api/results';
import { helpCenterName, helpCentersApi, helpCentersKey } from '../kb/help-centers-api';
import { helpCentersMessages } from '../kb/help-centers-messages';
import { ResultsHelpCenter } from './results-help-center';
import type { HelpCenterLinks } from './results-help-center';
import { helpCenterMessages as m } from './results-help-center-messages';

// ResultsHelpCenterChoice is the help center section, for the whole workspace or, once it has two help
// centers, for the one chosen: its articles and their views, and the requests that came in through it. Its
// requests open the inbox on the same help center (help_center:<handle>, help_center:primary).
export function ResultsHelpCenterChoice({ workspace, window, links, readsHelpCenters }: {
  workspace: Results['help_center']; window: WindowKey; links: HelpCenterLinks; readsHelpCenters: boolean;
}) {
  const [chosen, setChosen] = useState('');
  const list = useQuery({ queryKey: helpCentersKey, queryFn: helpCentersApi.list, enabled: readsHelpCenters });
  const centers = list.data?.help_centers ?? [];
  const center = centers.find((item) => item.id === chosen);
  // While the next help center loads, the numbers shown stay on screen.
  const one = useQuery({ queryKey: ['results', 'help-center', chosen], queryFn: () => resultsApi.get(chosen), enabled: Boolean(center),
    placeholderData: (previous) => previous });
  const help = (center && one.data ? one.data.help_center : workspace).windows[window];
  const word = center && `help_center:${center.primary ? 'primary' : center.handle ?? ''}`;
  const choice = centers.length > 1 && <span className="results-helpcenter-choice">
    <select aria-label={m.heading} value={center ? chosen : ''} aria-busy={one.isFetching} onChange={(event) => setChosen(event.target.value)}>
      <option value="">{m.allHelpCenters}</option>
      {centers.map((item) => <option key={item.id} value={item.id}>{helpCenterName(item, helpCentersMessages.primaryName)}</option>)}
    </select>
    {center && one.isError && <span role="alert" className="error-copy">{m.oneFailed}</span>}
  </span>;
  return <ResultsHelpCenter key={window} help={help} choice={choice || undefined}
    links={{ ...links, requests: (range, children) => links.requests(range, children, word) }} />;
}
