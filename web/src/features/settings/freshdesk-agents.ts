import type { z } from 'zod';
import type { importJobSchema } from '../../api/imports';

type ImportJob = z.infer<typeof importJobSchema>;

// freshdeskAgentCount is how many active agents the Freshdesk account of the newest import has, read at its
// preview. undefined when no import read an account yet, or when it found no active agent.
export function freshdeskAgentCount(jobs: readonly ImportJob[]): number | undefined {
  const newest = [...jobs]
    .filter((job) => job.kind === 'import' && job.state !== 'discarded' && job.preview !== null)
    .sort((a, b) => Date.parse(b.created_at) - Date.parse(a.created_at))[0];
  const active = newest?.preview?.agents.filter((agent) => agent.active).length ?? 0;
  return active > 0 ? active : undefined;
}
