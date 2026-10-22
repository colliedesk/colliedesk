import { mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { expect, test } from '@playwright/test';
import { redeemLink } from './support/browser';
import { colliedeskAsync, servers } from './support/cli';
import { FakeFreshdesk } from './support/fake-freshdesk';
import { createWorkspaceWithSlug } from './support/workspace';

interface LinksReport {
  status: string;
  help_center: string;
  old_addresses: { found: number; redirected: number; unmapped: unknown[] };
  rewritten: { articles: number; saved_replies: number; links: number };
  left: unknown[];
}

// docs/user/import-freshdesk.md, "Old help center links": after the import every address of the old
// Freshdesk help center leads to its new page, and the links inside the articles point there too.
test('freshdesk import: old help center addresses and links lead to the new help center pages', async ({ page }) => {
  test.setTimeout(180_000);
  const suffix = `${Date.now()}-${process.pid}`;
  const adminEmail = `admin-${suffix}@acme.example`;
  const { url, slug } = createWorkspaceWithSlug(`e2e-old-links ${suffix}`, adminEmail);
  const apiKey = 'fd-e2e-old-links-key';
  const fake = new FakeFreshdesk({ apiKey, agentEmail: adminEmail, knowledgeBase: true, links: true });
  await fake.start();
  const reportDir = mkdtempSync(join(tmpdir(), 'colliedesk-old-links-report-'));
  try {
    await redeemLink(page, url);
    const headers = { Origin: servers.main };
    expect((await page.request.put(`${servers.main}/api/v1/help-center`, { headers, data: { enabled: true, title: 'Acme help', locale: 'en' } })).status()).toBe(200);

    const run = await colliedeskAsync(['import', 'freshdesk', '--workspace', slug, '--domain', 'acme.freshdesk.com', '--report-dir', reportDir],
      { tags: 'e2e', env: { ...fake.env(), FRESHDESK_API_KEY: apiKey } });
    expect(run.status, run.stderr).toBe(0);
    const reportPath = run.stdout.match(/Report: (\S+\.json)/)?.[1] ?? '';
    const report = JSON.parse(readFileSync(reportPath, 'utf8')) as { status: string; links: LinksReport };
    const home = `${servers.main}/help/${slug}`;
    // Old addresses, by hand: home, solutions, category 7001, folder 7101, articles 7201 and 7203
    // (7202 is a draft), each also below /en/ and /es/; two links rewritten, none left.
    expect(report).toMatchObject({
      status: 'OK',
      links: { status: 'OK', help_center: home, old_addresses: { found: 18, redirected: 18, unmapped: [] },
        rewritten: { articles: 2, saved_replies: 0, links: 2 }, left: [] },
    });

    type Summary = { id: string; number: number; title: string; slug: string };
    const list = await (await page.request.get(`${servers.main}/api/v1/kb/articles`)).json() as { items: Summary[] };
    const byTitle = Object.fromEntries(list.items.map((item) => [item.title, item]));
    const started = byTitle['Getting started with Acme'];
    const reset = byTitle['Reset your password'];
    const articleURL = (item: Summary) => `${home}/a/${item.number}-${item.slug}`;
    const body = (await (await page.request.get(`${servers.main}/api/v1/kb/articles/${started.id}`)).json() as { body_md: string }).body_md;
    expect(body).toContain(`[Reset your password](${articleURL(reset)})`);
    expect(body).not.toContain('acme.freshdesk.com');

    const moves: Array<[string, number, string]> = [
      ['/support/home', 301, home],
      ['/support/solutions', 301, home],
      ['/support/solutions/7001', 301, `${home}/c/help`],
      ['/support/solutions/folders/7101', 301, `${home}/c/help/start`],
      ['/support/solutions/articles/7201-getting-started-with-acme', 301, articleURL(started)],
      ['/support/solutions/articles/7203-reset-your-password', 301, articleURL(reset)],
      ['/support/solutions/articles/7202-coming-soon', 302, `${home}/search?q=coming+soon`],
      ['/support/solutions/articles/9999-billing-faq', 302, `${home}/search?q=billing+faq`],
    ];
    for (const [path, status, location] of moves) {
      for (const prefix of ['', '/es']) {
        const response = await page.request.get(`${home}${prefix}${path}`, { maxRedirects: 0 });
        expect(response.status(), prefix + path).toBe(status);
        expect(response.headers().location, prefix + path).toBe(location);
      }
    }

    // A visitor following an old Freshdesk article address lands on the new article.
    await page.goto(`${home}/es/support/solutions/articles/7203-reset-your-password`);
    await expect(page).toHaveURL(articleURL(reset));
    await expect(page.getByRole('heading', { level: 1, name: 'Reset your password' })).toBeVisible();
    await page.goto(articleURL(started));
    // The link inside the article is the absolute new address (the list beside it is relative).
    await page.locator(`a[href="${articleURL(reset)}"]`).click();
    await expect(page).toHaveURL(articleURL(reset));
  } finally {
    await fake.stop();
    rmSync(reportDir, { recursive: true, force: true });
  }
});
