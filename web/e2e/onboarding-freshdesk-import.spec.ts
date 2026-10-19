import { mkdtempSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { expect, test } from '@playwright/test';
import { colliedesk, colliedeskAsync, servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';
import { FakeFreshdesk, fileBody, fixture } from './support/fake-freshdesk';
import { createWorkspaceWithSlug } from './support/workspace';

// Journey 4: docs/user/import-freshdesk.md against a local Freshdesk API v2 (the only fake).
const baseURL = servers.main;
const apiKey = 'fd-e2e-key-never-printed';
const headers = { Origin: baseURL };

interface Report {
  status: string;
  contacts: { source: number; imported: number; failed: number };
  tickets: { source: number; imported: number; failed: number; archived: number };
  messages: { source: number; imported: number };
  attachments: { source: number; imported: number; source_bytes: number; imported_bytes: number; checksum_verified: number };
  time_entries: { source: number; imported: number };
  saved_replies: { source: number; imported: number; failed: number; not_available: boolean; personal_not_imported: string[]; unmapped_placeholders: Record<string, string[]> };
  verification: { tickets: { verified: boolean; detail: string }; contacts: { verified: boolean }; saved_replies: { verified: boolean } };
  failures: unknown[];
}

async function importFreshdesk(fake: FakeFreshdesk, slug: string, extra: string[] = [], key = apiKey) {
  const reportDir = mkdtempSync(join(tmpdir(), 'colliedesk-import-report-'));
  const result = await colliedeskAsync(['import', 'freshdesk', '--workspace', slug, '--domain', 'acme.freshdesk.com', '--report-dir', reportDir, ...extra],
    { tags: 'e2e', env: { ...fake.env(), FRESHDESK_API_KEY: key } });
  const jsonPath = result.stdout.match(/Report: (\S+\.json)/)?.[1];
  const report = jsonPath ? JSON.parse(readFileSync(jsonPath, 'utf8')) as Report : undefined;
  expect(result.stdout + result.stderr).not.toContain(key);
  return { ...result, report };
}

test('freshdesk import: reconcile OK with exact counts, tickets searchable with original timestamps, undo removes exactly the import', { tag: '@onboarding' }, async ({ page }) => {
  test.setTimeout(240_000);
  const suffix = uniqueSuffix();
  const adminEmail = `alice-${suffix}@acme.example`;
  const workspace = createWorkspaceWithSlug(`Import Desk ${suffix}`, adminEmail);
  const fake = new FakeFreshdesk({ apiKey, agentEmail: adminEmail });
  await fake.start();
  try {
    await redeemLink(page, workspace.url);
    // Data the team had before the import must survive undo.
    const contact = await (await page.request.post(`${baseURL}/api/v1/contacts`, { headers, data: { name: 'Walk-in', email: `walkin-${suffix}@example.com` } })).json() as { id: string };
    expect((await page.request.post(`${baseURL}/api/v1/conversations`, { headers, data: { contact_id: contact.id, subject: `Pre-import ${suffix}` } })).status()).toBe(201);

    const dry = await importFreshdesk(fake, workspace.slug, ['--dry-run']);
    expect(dry.status, dry.stderr).toBe(0);
    expect(dry.report?.status).toBe('DRY_RUN');
    expect(fake.count('/api/v2/contacts')).toBeGreaterThanOrEqual(2); // 429 + Retry-After honoured, then retried

    const first = await importFreshdesk(fake, workspace.slug);
    expect(first.status, first.stderr).toBe(0);
    expect(first.stdout).toContain('Status: OK');
    expect(first.report).toMatchObject({
      status: 'OK', failures: [],
      contacts: { source: 120, imported: 120, failed: 0 },
      tickets: { source: 300, imported: 300, failed: 0, archived: 1 },
      messages: { source: 960, imported: 960 },
      attachments: { source: 40, imported: 40, checksum_verified: 40 },
      time_entries: { source: 15, imported: 15 },
      verification: { tickets: { verified: true }, contacts: { verified: true }, saved_replies: { verified: true } },
      // The personal folder is listed, not imported; a placeholder Colliedesk has no variable for is reported.
      saved_replies: { source: fixture.savedReplies, imported: fixture.savedReplies, failed: 0, not_available: false, personal_not_imported: ['9103'], unmapped_placeholders: { 'ticket.url': ['9102'] } },
    });
    const savedReplies = await (await page.request.get(`${baseURL}/api/v1/saved-replies`)).json() as { items: Array<{ title: string; body: string }> };
    expect(savedReplies.items.map((item) => item.title)).toEqual(['General / Greeting', 'General / Track your parcel']);
    expect(savedReplies.items[0].body).toBe('Hi {{customer.name}}, thanks for writing.\n{{agent.name}}');
    expect(first.report?.attachments.imported_bytes).toBe(first.report?.attachments.source_bytes);
    expect(fake.count(`/api/v2/tickets/archived/${fixture.archivedTicket}/conversations`)).toBeGreaterThanOrEqual(1);

    // Running it again copies nothing twice.
    const again = await importFreshdesk(fake, workspace.slug);
    expect(again.status, again.stderr).toBe(0);
    expect(again.report).toMatchObject({ status: 'OK', tickets: { source: 300, imported: 300 }, messages: { imported: 960 } });
    const all = await (await page.request.get(`${baseURL}/api/v1/conversations?page_size=100&page=4`)).json() as { items: unknown[] };
    expect(all.items).toHaveLength(1); // 300 imported + 1 pre-import = 301 = 3 full pages + 1

    // In the inbox: search finds the ticket, the row shows the original date with its year.
    await page.reload();
    await page.getByRole('searchbox', { name: 'Search conversations' }).fill(fixture.searchableWord);
    const row = page.getByRole('button', { name: /Fixture ticket 42: parcel stuck in Rotterdam/ });
    await expect(row).toHaveCount(1);
    await expect(row.locator('time')).toHaveAttribute('dateTime', '2024-01-02T18:30:00Z');
    await expect(row.locator('time')).toContainText('2024');
    await row.click();
    // Imported HTML is shown formatted, inside the locked frame.
    await expect(page.frameLocator('iframe.email-frame').first().getByText('Description of ticket 42.')).toBeVisible();
    await expect(page.frameLocator('iframe.email-frame').nth(1).getByText('Customer follow-up on 42')).toBeVisible();
    const found = await (await page.request.get(`${baseURL}/api/v1/conversations?search=${fixture.searchableWord}`)).json() as { items: Array<{ id: string; created_at: string }> };
    expect(found.items.map((item) => item.created_at)).toEqual(['2024-01-02T18:00:00Z']);

    // The archived ticket came over with its attachment, byte for byte.
    await page.getByRole('searchbox', { name: 'Search conversations' }).fill('Fixture ticket 150:');
    await page.getByRole('button', { name: /Fixture ticket 150:/ }).click();
    await expect(page.frameLocator('iframe.email-frame').first().getByText('Description of ticket 150.')).toBeVisible();
    const invoice = page.getByRole('link', { name: 'invoice-150.pdf' });
    expect(Buffer.from(await (await page.request.get(`${baseURL}${await invoice.getAttribute('href')}`)).body())).toEqual(fileBody(900_150));

    // The team answers ticket 42 after the import: undo keeps that conversation.
    await page.getByRole('searchbox', { name: 'Search conversations' }).fill(fixture.searchableWord);
    await page.getByRole('button', { name: /Fixture ticket 42:/ }).click();
    await page.getByLabel('Message body').last().fill('Answered in Colliedesk after the import.');
    await page.getByRole('button', { name: 'Send reply' }).click();
    await expect(page.getByText('Answered in Colliedesk after the import.')).toBeVisible();

    // Undo is what a person runs right after importing into the wrong workspace: it must not stall.
    const startedUndo = Date.now();
    const preview = colliedesk(['import', 'undo', '--workspace', workspace.slug, '--dry-run']);
    expect(preview.status, preview.stderr).toBe(0);
    expect(Date.now() - startedUndo, 'undo --dry-run right after an import').toBeLessThan(10_000);
    expect(preview.stdout).toContain('Would remove from freshdesk:account:4242: 299 conversations, 119 contacts, 2 tags, 2 saved replies, 40 stored files.');
    const undo = colliedesk(['import', 'undo', '--workspace', workspace.slug]);
    expect(undo.status, undo.stderr).toBe(0);
    expect(undo.stdout).toContain('Removed from freshdesk:account:4242: 299 conversations, 119 contacts, 2 tags, 2 saved replies, 40 stored files.');
    expect(undo.stdout).toContain('(Freshdesk ticket ids): 42');
    expect((await (await page.request.get(`${baseURL}/api/v1/saved-replies`)).json() as { items: unknown[] }).items).toEqual([]);

    const left = await (await page.request.get(`${baseURL}/api/v1/conversations?page_size=100`)).json() as { items: Array<{ subject: string }> };
    expect(left.items.map((item) => item.subject).sort()).toEqual([`Fixture ticket 42: parcel stuck in ${fixture.searchableWord}`, `Pre-import ${suffix}`]);
    const contacts = await (await page.request.get(`${baseURL}/api/v1/contacts`)).json() as { items: Array<{ email: string }> };
    expect(contacts.items.map((item) => item.email).sort()).toEqual(['customer42@shop6.example', `walkin-${suffix}@example.com`]);
  } finally {
    await fake.stop();
  }
});

test('freshdesk import: an account without canned responses still migrates its tickets and says so', { tag: '@onboarding' }, async () => {
  test.setTimeout(180_000);
  const suffix = uniqueSuffix();
  const workspace = createWorkspaceWithSlug(`No Canned ${suffix}`, `alice-${suffix}@acme.example`);
  const fake = new FakeFreshdesk({ apiKey, agentEmail: `alice-${suffix}@acme.example`, cannedUnavailable: true });
  await fake.start();
  try {
    const run = await importFreshdesk(fake, workspace.slug);
    expect(run.status, run.stderr).toBe(0);
    expect(run.stdout).toContain('Status: OK');
    expect(run.report).toMatchObject({ status: 'OK', saved_replies: { source: 0, imported: 0, not_available: true }, verification: { saved_replies: { verified: true } } });
  } finally {
    await fake.stop();
  }
});

test('freshdesk import: a wrong API key stops before writing anything and says why', { tag: '@onboarding' }, async ({ page }) => {
  const suffix = uniqueSuffix();
  const workspace = createWorkspaceWithSlug(`Wrong Key ${suffix}`, `alice-${suffix}@acme.example`);
  const fake = new FakeFreshdesk({ apiKey, agentEmail: `alice-${suffix}@acme.example` });
  await fake.start();
  try {
    const run = await importFreshdesk(fake, workspace.slug, [], 'not-the-key');
    expect(run.status).not.toBe(0);
    expect(run.stderr).toContain('Freshdesk rejected the API key');
    expect(run.stdout).not.toContain('Status: OK');
    expect(run.report).toBeUndefined();
    await redeemLink(page, workspace.url);
    expect((await (await page.request.get(`${baseURL}/api/v1/conversations`)).json() as { items: unknown[] }).items).toEqual([]);
  } finally {
    await fake.stop();
  }
});

test('freshdesk import: an archive refused with 403 is reported UNVERIFIED, never OK', { tag: '@onboarding' }, async () => {
  test.setTimeout(180_000);
  const suffix = uniqueSuffix();
  const workspace = createWorkspaceWithSlug(`No Archive ${suffix}`, `alice-${suffix}@acme.example`);
  const fake = new FakeFreshdesk({ apiKey, agentEmail: `alice-${suffix}@acme.example`, archiveForbidden: true });
  await fake.start();
  try {
    const run = await importFreshdesk(fake, workspace.slug);
    expect(run.status).not.toBe(0);
    expect(run.stdout).toContain('Status: UNVERIFIED');
    expect(run.stderr).toContain('import UNVERIFIED');
    expect(run.report?.status).toBe('UNVERIFIED');
    expect(run.report?.verification.tickets.verified).toBe(false);
    expect(run.report?.verification.tickets.detail).toContain('archived-ticket endpoint was refused');
  } finally {
    await fake.stop();
  }
});
