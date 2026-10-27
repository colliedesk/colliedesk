import { afterEach, describe, expect, it } from 'vitest';
import { githubReturn } from './autofix-settings';

afterEach(() => window.history.replaceState(null, '', '/'));

describe('githubReturn', () => {
  it('says what GitHub’s redirect brought back and cleans the address', () => {
    window.history.replaceState(null, '', '/settings/autofix?github=requested&tab=x');
    expect(githubReturn()).toEqual({ notice: expect.stringContaining('request to an owner') });
    expect(window.location.search).toBe('?tab=x');
    window.history.replaceState(null, '', '/settings/autofix?github_error=forbidden');
    expect(githubReturn()).toEqual({ problem: 'Only an admin of this workspace can connect GitHub.' });
    window.history.replaceState(null, '', '/settings/autofix?github_error=something_new');
    expect(githubReturn()?.problem).toMatch(/without finishing/);
    window.history.replaceState(null, '', '/settings/autofix');
    expect(githubReturn()).toBeUndefined();
  });
});
