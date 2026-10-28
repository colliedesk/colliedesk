import '@testing-library/jest-dom/vitest';
import { cleanup } from '@testing-library/react';
import { afterEach } from 'vitest';

afterEach(cleanup);

// The browser's clock reads UTC here, as on CI, whatever zone the machine running the tests is in: a stamp
// names its timezone when it differs from the browser's, so the tests must not depend on the host.
process.env.TZ = 'UTC';
