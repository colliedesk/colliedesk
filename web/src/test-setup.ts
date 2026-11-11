import '@testing-library/jest-dom/vitest';
import { cleanup } from '@testing-library/react';
import { afterEach, beforeEach } from 'vitest';
import { knowPermissions } from './api/client';

afterEach(cleanup);
// No session is known until a test answers GET /api/v1/auth/me: reads then go out as the test expects.
beforeEach(() => knowPermissions(null));

// The browser's clock reads UTC here, as on CI, whatever zone the machine running the tests is in: a stamp
// names its timezone when it differs from the browser's, so the tests must not depend on the host.
process.env.TZ = 'UTC';
