import { z } from 'zod';
import type { Diagnostics } from '../lib/diagnostics';

const createdSchema = z.object({ reference: z.string() });

// NotConfiguredError means this server has no support desk for problem reports (every self-hosted
// server by default): the dialog offers a GitHub issue instead.
export class ProblemReportsNotConfiguredError extends Error {
  constructor() {
    super('problem reports are not configured');
    this.name = 'ProblemReportsNotConfiguredError';
  }
}

// ProblemReportRefusedError carries the server's own words (too many reports, an invalid field).
export class ProblemReportRefusedError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'ProblemReportRefusedError';
  }
}

async function refusal(response: Response): Promise<Error> {
  let body: { error?: { code?: unknown; message?: unknown; fields?: Record<string, string> } } = {};
  try {
    body = await response.json();
  } catch {
    // An unreadable body is an ordinary failure.
  }
  const { code, message, fields } = body.error ?? {};
  if (response.status === 404 && code === 'not_configured') return new ProblemReportsNotConfiguredError();
  if (response.status === 422 && fields) return new ProblemReportRefusedError(Object.values(fields).join(' '));
  if (response.status === 429 && typeof message === 'string' && message) return new ProblemReportRefusedError(message);
  return new Error(`API ${response.status}`);
}

// sendProblemReport files the report and answers its reference. With screenshots the body is multipart,
// otherwise JSON.
export async function sendProblemReport(description: string, diagnostics: Diagnostics, screenshots: File[], key: string): Promise<string> {
  let init: RequestInit;
  if (screenshots.length > 0) {
    const form = new FormData();
    form.set('description', description);
    form.set('diagnostics', JSON.stringify(diagnostics));
    for (const file of screenshots) form.append('screenshots', file, file.name);
    init = { method: 'POST', headers: { 'Idempotency-Key': key }, body: form };
  } else {
    init = { method: 'POST', headers: { 'Content-Type': 'application/json', 'Idempotency-Key': key }, body: JSON.stringify({ description, diagnostics }) };
  }
  const response = await fetch('/api/v1/support/problem-reports', { credentials: 'same-origin', ...init });
  // 200 answers a repeat of the same press: the report the first one sent.
  if (response.status !== 201 && response.status !== 200) throw await refusal(response);
  return createdSchema.parse(await response.json()).reference;
}
