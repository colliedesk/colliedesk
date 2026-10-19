import type { components } from '../api/generated';

// What "Report a problem" can attach, kept in memory only. Nothing is stored in the browser and nothing
// is sent until the person presses Send in the dialog: this module only remembers the last few script
// errors, failed API calls (with the server's request id) and product event names, and reads the page,
// browser and window when a report is being made. Reloading the tab forgets all of it.
export type Diagnostics = components['schemas']['ProblemReportDiagnostics'];
type ClientError = NonNullable<Diagnostics['client_errors']>[number];
type FailedCall = NonNullable<Diagnostics['failed_api_calls']>[number];

const MAX_ERRORS = 20;
const MAX_CALLS = 20;
const MAX_EVENTS = 30;
const MAX_MESSAGE = 500;
const UUID = /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/gi;

let errors: ClientError[] = [];
let calls: FailedCall[] = [];
let events: string[] = [];
let installed = false;

function keep<T>(list: T[], item: T, max: number): T[] {
  return [...list, item].slice(-max);
}

// pathOf drops the query string and fragment, which can hold a search term, and turns record ids into {id}.
export function pathOf(address: string): string {
  return address.split(/[?#]/)[0].replace(UUID, '{id}').slice(0, 200);
}

export function recordClientError(source: string, reason: unknown): void {
  const text = reason instanceof Error ? `${reason.name}: ${reason.message}` : String(reason);
  errors = keep(errors, { message: text.slice(0, MAX_MESSAGE), source, at: new Date().toISOString() }, MAX_ERRORS);
}

export function recordFailedCall(method: string, address: string, status: number, requestID: string): void {
  calls = keep(calls, { method, route: pathOf(address), status, request_id: requestID, at: new Date().toISOString() }, MAX_CALLS);
}

export function recordProductEvent(name: string): void {
  events = keep(events, name, MAX_EVENTS);
}

// installDiagnostics starts listening for script errors once.
export function installDiagnostics(): void {
  if (installed) return;
  installed = true;
  window.addEventListener('error', (event) => recordClientError('window.error', event.error ?? event.message));
  window.addEventListener('unhandledrejection', (event) => recordClientError('unhandledrejection', event.reason));
}

// collectDiagnostics is what would be sent, as of now.
export function collectDiagnostics(appVersion?: string): Diagnostics {
  return {
    app_version: appVersion || 'dev',
    user_agent: navigator.userAgent,
    viewport: { width: window.innerWidth, height: window.innerHeight },
    page_path: pathOf(window.location.pathname),
    client_errors: errors,
    failed_api_calls: calls,
    product_events: events,
  };
}

export function resetDiagnostics(): void {
  errors = [];
  calls = [];
  events = [];
}
