import type { Diagnostics } from '../../lib/diagnostics';

const NEW_ISSUE = 'https://github.com/colliedesk/colliedesk/issues/new';
// GitHub refuses addresses over about 8 KB; stay well under.
const MAX_ADDRESS = 7000;
const EMAIL = /[^\s@<>()"']+@[^\s@<>()"']+\.[^\s@<>()"']+/g;

// withoutEmails keeps an address someone typed or an error echoed out of a public issue.
const withoutEmails = (text: string) => text.replace(EMAIL, '[email removed]');

function describe(diagnostics: Diagnostics): string {
  const errors = (diagnostics.client_errors ?? []).map((item) => `  - ${item.at} ${item.source}: ${item.message}`);
  const calls = (diagnostics.failed_api_calls ?? []).map((item) =>
    `  - ${item.at} ${item.method} ${item.route} ${item.status} (request id ${item.request_id || 'none'})`);
  const view = diagnostics.viewport;
  return [
    '### Diagnostics',
    `- App version: ${diagnostics.app_version ?? 'unknown'}`,
    `- Browser: ${diagnostics.user_agent ?? 'unknown'}`,
    `- Window size: ${view ? `${view.width}x${view.height}` : 'unknown'}`,
    `- Page: ${diagnostics.page_path ?? 'unknown'}`,
    `- Recent errors (${errors.length}):`, ...errors,
    `- Recent failed requests (${calls.length}):`, ...calls,
    `- Recent actions: ${(diagnostics.product_events ?? []).join(', ') || 'none'}`,
  ].join('\n');
}

// issueURL is a new GitHub issue for the Colliedesk project with the description and diagnostics filled
// in, for a server that has no support desk. Email addresses are removed; the person reads it on GitHub
// and decides whether to submit.
export function issueURL(description: string, diagnostics: Diagnostics): string {
  const title = withoutEmails(`Problem: ${description.split('\n')[0].slice(0, 70)}`);
  let body = withoutEmails(`${description.slice(0, 1500)}\n\n${describe(diagnostics)}`);
  const build = () => `${NEW_ISSUE}?${new URLSearchParams({ title, body }).toString()}`;
  while (build().length > MAX_ADDRESS && body.length > 200) body = `${body.slice(0, Math.floor(body.length * 0.8))}…`;
  return build();
}
