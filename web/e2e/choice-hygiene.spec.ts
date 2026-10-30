import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';
import { servers } from './support/cli';
import { redeemLink } from './support/browser';
import { createWorkspaceWithSlug } from './support/workspace';

// docs/internals/design.md (Rules): a select is shown only when it offers at least two real
// choices, and one control never lists "Default (X)" beside X. Run on the main pages of a new
// workspace, then with a conversation and a collection, on a wide and a phone screen.
// Not visited yet: the Built-in AI page (/settings/ai), whose provider and model selects are being
// reworked on their own; add it here when they show only real choices.
const pages = ['/', '/requests', '/knowledge', '/knowledge/media', '/results', '/fixes', '/settings', '/settings/channels/type/email', '/settings/autofix'];

interface Scan { selects: number; problems: string[] }

// scan lists every visible select that offers fewer than two real choices (a disabled, hidden, empty or
// "—" option is not one) and every "Default (X)" listed beside X.
function scan(page: Page): Promise<Scan> {
  return page.evaluate(() => {
    const problems: string[] = [];
    const shown = Array.from(document.querySelectorAll('select')).filter((select) => {
      const box = select.getBoundingClientRect();
      return box.width > 0 && box.height > 0 && getComputedStyle(select).visibility !== 'hidden';
    });
    for (const select of shown) {
      const name = select.getAttribute('aria-label') || select.labels?.[0]?.textContent?.trim() || select.id || 'a select';
      const choices = Array.from(select.options).filter((option) => !option.disabled && !option.hidden)
        .map((option) => option.text.trim()).filter((text) => text !== '' && text !== '—');
      if (choices.length < 2) problems.push(`${name}: ${choices.length} real choice(s) [${choices.join(', ')}]`);
      for (const choice of choices) {
        const inner = /^Default \((.+)\)$/i.exec(choice)?.[1];
        if (inner && choices.some((other) => other.toLowerCase() === inner.toLowerCase())) problems.push(`${name}: "${choice}" beside "${inner}"`);
      }
    }
    return { selects: shown.length, problems };
  });
}

// visit opens each page once nothing is loading and returns how many selects it checked.
async function visit(page: Page, paths: string[], viewport: number): Promise<number> {
  let selects = 0;
  for (const path of paths) {
    await page.goto(`${servers.main}${path}`, { waitUntil: 'networkidle' });
    const found = await scan(page);
    expect(found.problems, `${path} at ${viewport}px`).toEqual([]);
    selects += found.selects;
  }
  return selects;
}

for (const viewport of [{ width: 1440, height: 900 }, { width: 390, height: 844 }]) {
  test(`choice hygiene: every visible select on the main pages offers real choices (${viewport.width}px)`, async ({ page }) => {
    const suffix = `${Date.now()}-${process.pid}-${viewport.width}`;
    await page.setViewportSize(viewport);
    await redeemLink(page, createWorkspaceWithSlug(`e2e-choices-${suffix}`, `owner-${suffix}@example.com`).url);
    // A new workspace: no collection yet, so a new one is not asked where it goes.
    let selects = await visit(page, pages, viewport.width);

    const headers = { Origin: servers.main };
    const contact = await page.request.post(`${servers.main}/api/v1/contacts`, { headers, data: { name: 'Choice Customer', email: `customer-${suffix}@example.com` } });
    expect(contact.status()).toBe(201);
    const { id: contactId } = await contact.json() as { id: string };
    const conversation = await page.request.post(`${servers.main}/api/v1/conversations`, { headers, data: { contact_id: contactId, subject: `Choices ${suffix}` } });
    expect(conversation.status()).toBe(201);
    const { id } = await conversation.json() as { id: string };
    const collection = await page.request.post(`${servers.main}/api/v1/kb/collections`, { headers, data: { name: 'Getting started', parent_id: null } });
    expect(collection.status()).toBe(201);
    selects += await visit(page, ['/', `/conversations/${id}`, '/knowledge'], viewport.width);
    // The check saw selects (timezones, roles, fonts, a conversation's properties), so an empty pass means something.
    expect(selects).toBeGreaterThan(3);
  });
}
