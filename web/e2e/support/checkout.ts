import { createHash } from 'node:crypto';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

// Two checkouts on one host (worktrees, parallel agents) run end-to-end tests at the same time.
// Ports, databases, binaries and temp files therefore derive from the checkout path so they never clobber
// each other. Each checkout has a block of 20 ports; COLLIEDESK_E2E_PORT_BASE overrides where it starts.
export const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), '../../..');
export const checkoutID = createHash('sha256').update(repoRoot).digest('hex').slice(0, 8);
const id = checkoutID;

export const portBase = Number(process.env.COLLIEDESK_E2E_PORT_BASE ?? 20_000 + (parseInt(id, 16) % 400) * 20);
export const ports = { main: portBase, mail: portBase + 2, fresh: portBase + 3, cloud: portBase + 4, jwks: portBase + 5, freshdeskAPI: portBase + 6, freshdeskFiles: portBase + 7,
  microsoft: portBase + 8, microsoftStandIn: portBase + 9, autofix: portBase + 10, githubStandIn: portBase + 11, cursorStandIn: portBase + 12,
  operator: portBase + 13, operatorHost: portBase + 14, operatorTrial: portBase + 15, features: portBase + 16 };
export const scratchDir = join(tmpdir(), `colliedesk-e2e-${id}`);
