import { writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { routePermissionsModule } from './route-permissions.mjs';

writeFileSync(resolve(import.meta.dirname, '../src/api/route-permissions.ts'), routePermissionsModule());
