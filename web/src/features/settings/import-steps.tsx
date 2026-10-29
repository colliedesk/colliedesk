import type { ImportJob, ImportReport } from '../../api/imports';
import { importMessages as m } from './import-messages';

// The importer's progress lines, grouped into the steps a person recognises, in the order the importer runs them.
// Stage 0 is the first pass over the recent tickets; the other stages belong to the pass over the whole account.
const stageOfProgress: Record<string, number> = {
  'listing knowledge base': 1, 'sizing attachments': 1, 'loading agents': 1, 'loading companies': 1, contacts: 1, 'importing contacts': 1,
  'importing saved replies': 2,
  'listing tickets': 3, 'checking ticket ids': 3, 'verifying totals': 3,
  'importing knowledge base': 4, 'importing knowledge base articles': 4, 'rewriting help center links': 4, 'reading Search Console tokens': 4,
  'importing tickets': 5,
};
const firstStageOfWholeAccount = 1;

type StepState = 'done' | 'current' | 'waiting';

// Steps lists what the import does, each done, under way or waiting, with the count of the one under way.
export function ImportSteps({ job, report, recentDays }: { job: ImportJob; report?: ImportReport; recentDays: number }) {
  const progress = job.progress;
  const wholeAccount = job.phase === 'full' || recentDays === 0;
  const current = wholeAccount ? (progress ? stageOfProgress[progress.step] ?? firstStageOfWholeAccount : firstStageOfWholeAccount) : 0;
  const running = job.state === 'running';
  const names = m.stepNames(recentDays);
  const percent = progress && progress.total > 0 && progress.done < progress.total ? Math.round((progress.done / progress.total) * 100) : undefined;
  return <ol className="import-steps" aria-label={m.phases}>
    {names.map((name, stage) => {
      if (stage === 0 && recentDays === 0) return null;
      const state: StepState = stage < current ? 'done' : stage === current ? 'current' : 'waiting';
      const recentTickets = report?.interim ? report.tickets?.imported : undefined;
      const detail = state === 'current' && running && progress ? m.stepProgress(progress.done, progress.total)
        : state === 'done' && stage === 0 && recentTickets !== undefined ? m.ticketCount(recentTickets) : '';
      return <li key={name} data-state={state} aria-current={state === 'current' ? 'step' : undefined}>
        <span className="import-step-mark" aria-hidden="true" />
        <span className="import-step-body">
          <span><span data-step-name>{name}</span>{' '}<span className="import-step-state">{m.stepStates[state]}</span></span>
          {detail && <small className="muted-copy">{detail}</small>}
          {state === 'current' && running && percent !== undefined
            && <div className="usage-bar" role="progressbar" aria-label={name} aria-valuemin={0} aria-valuemax={100} aria-valuenow={percent}><span style={{ width: `${percent}%` }} /></div>}
        </span>
      </li>;
    })}
  </ol>;
}
