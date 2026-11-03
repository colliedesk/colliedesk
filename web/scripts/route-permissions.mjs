import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

// routePermissions reads, from the contract, the permission each GET route needs (its x-permission), so the
// web app can skip a read the session may not make instead of drawing a 403 from the server.
export function routePermissions(contract = resolve(import.meta.dirname, '../../contracts/openapi.yaml')) {
  const rows = [];
  let path;
  let method;
  for (const line of readFileSync(contract, 'utf8').split('\n')) {
    const pathLine = /^ {2}(\/api\/v1\/\S*):\s*$/.exec(line);
    if (pathLine) { path = pathLine[1]; method = undefined; continue; }
    if (/^\S/.test(line)) { path = undefined; continue; }
    const methodLine = /^ {4}(get|put|post|patch|delete):\s*$/.exec(line);
    if (methodLine) { method = methodLine[1]; continue; }
    const permission = /^ {6}x-permission:\s*([a-z_]+:[a-z_]+)\s*$/.exec(line);
    if (permission && path && method === 'get') rows.push([path, permission[1]]);
  }
  return rows.sort(([a], [b]) => a.localeCompare(b));
}

export function routePermissionsModule(rows = routePermissions()) {
  return `// Generated from contracts/openapi.yaml by scripts/route-permissions.mjs (corepack pnpm generate:api). Do not edit.
// The permission each GET route needs: a read the session may not make is skipped, never sent for a 403.
export const routePermissions: ReadonlyArray<readonly [string, string]> = [
${rows.map(([path, permission]) => `  ['${path}', '${permission}'],`).join('\n')}
];
`;
}
