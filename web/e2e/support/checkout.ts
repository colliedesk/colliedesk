import { createHash } from 'node:crypto';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

// Two checkouts on one host (worktrees, parallel agents) run end-to-end tests at the same time.
// Ports, binaries and temp files therefore derive from the checkout path so they never clobber
// each other. COLLIEDESK_E2E_PORT_BASE overrides the port block.
export const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), '../../..');
const id = createHash('sha256').update(repoRoot).digest('hex').slice(0, 8);

export const portBase = Number(process.env.COLLIEDESK_E2E_PORT_BASE ?? 20_000 + (parseInt(id, 16) % 400) * 10);
export const ports = { main: portBase, mail: portBase + 2, fresh: portBase + 3, cloud: portBase + 4, jwks: portBase + 5, freshdeskAPI: portBase + 6, freshdeskFiles: portBase + 7,
  microsoft: portBase + 8, microsoftStandIn: portBase + 9 };
export const scratchDir = join(tmpdir(), `colliedesk-e2e-${id}`);
