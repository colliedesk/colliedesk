import { useId } from 'react';
import { useQuery } from '@tanstack/react-query';
import { api } from '../../../api/client';
import type { ResponseTargets } from '../../../api/response-targets';
import { useSaveFeedback } from '../save-feedback';
import { targetsMessages as m } from './targets-messages';
import { turnOn } from './targets-start';
import { ZoneNote, cardState, fieldErrors, useSave } from './targets-shared';

// While measuring is off the page is this one card (with the editor under it only after a turn-on the server refused): what measuring is, what turning it on starts with, and the
// button. Hours, holidays and targets are not shown, so nothing looks live that is not.
export function OffCard({ doc, canManage, workspaceZone, myZone, repairing, onRefused }: {
  doc: ResponseTargets; canManage: boolean; workspaceZone: string; myZone: string; repairing?: boolean; onRefused: (fields: Record<string, string>) => void;
}) {
  const save = useSave();
  const feedback = useSaveFeedback(cardState(save));
  // The hours and targets a first turn-on starts with come from the server, as in the setup flow.
  const setup = useQuery({ queryKey: ['setup'], queryFn: api.setupProgress, enabled: canManage });
  const id = useId();
  const start = setup.data ? turnOn(doc, setup.data, workspaceZone) : undefined;
  return <section className="tg-off" aria-labelledby={id}>
    <h2 id={id}>{m.offHeading}</h2>
    <p>{m.offLead}</p>
    {canManage && <>
      {repairing && <p className="error-copy">{m.fixFirst}</p>}
      {start && <p className="muted-copy">{start.line}</p>}
      {/* While repairing, the editor below says it. */}
      {!repairing && <ZoneNote clock={doc.timezone ?? workspaceZone} mine={myZone} />}
      {setup.isError && <p role="alert" className="error-copy">{m.startLoadFailed} <button type="button" className="link-action" onClick={() => void setup.refetch()}>{m.tryAgain}</button></p>}
      <span className="button-row"><button type="button" className="primary-action tg-turn-on" disabled={!start || save.isPending}
        onClick={() => {
          if (!start) return;
          feedback.touch();
          // Refused with the fields that are wrong (a channel deleted since, holidays that close every day): the editor opens.
          save.mutate(start.change, { onError: (error) => { const fields = fieldErrors(error); if (Object.keys(fields).length > 0) onRefused(fields); } });
        }}>{m.turnOn}</button>{feedback.status}</span>
    </>}
  </section>;
}
