import { expect, test } from '@playwright/test';
import type { Locator, Page } from '@playwright/test';
import { servers } from './support/cli';
import { redeemLink } from './support/browser';
import { createWorkspaceWithSlug } from './support/workspace';

// The inbox panes keep to their own columns (Paper "15 Inbox": I5-a nothing open, I1-a a conversation, M-I2 a
// conversation on a phone): nothing open shows no reply box and no details, the setup guide stays over the list,
// and the conversation's header keeps its labels on one line.
const base = servers.main;
const headers = { Origin: base };

async function newWorkspace(page: Page, name: string): Promise<string> {
  const suffix = `${Date.now()}-${process.pid}`;
  await redeemLink(page, createWorkspaceWithSlug(`${name} ${suffix}`, `owner-${suffix}@example.com`).url);
  return suffix;
}

// Two web forms, so each row and thread names its form; every customer writes through one of them.
async function formConversations(page: Page, suffix: string, count: number): Promise<void> {
  const site = 'https://panes.example';
  const paths: string[] = [];
  for (const name of ['Website form', 'Pricing page form']) {
    const form = await page.request.post(`${base}/api/v1/channels`, { headers, data: { type: 'web_form', name, config: { allowed_origins: [site] } } });
    expect(form.status()).toBe(201);
    paths.push((await form.json() as { config: { submit_path: string } }).config.submit_path);
  }
  for (let index = 0; index < count; index++) {
    const email = `customer-with-a-long-address-${index}-${suffix}@example.com`;
    const posted = await page.request.post(`${base}${paths[index % 2]}`, { headers: { Origin: site, 'Content-Type': 'application/json' },
      data: { name: `Customer ${index}`, email, message: `Question number ${index}: the export button does nothing on Safari.` } });
    expect(posted.status()).toBe(202);
  }
  await expect.poll(async () => (await (await page.request.get(`${base}/api/v1/conversations?page_size=50`)).json() as { items: unknown[] }).items.length,
    { timeout: 30_000 }).toBeGreaterThanOrEqual(count);
}

// The placeholder's box of an input, from the browser's own shadow tree (Chromium): its width and how it cuts its words.
async function placeholderOf(page: Page, label: string): Promise<{ width: number; textOverflow: string }> {
  type Node = { nodeId: number; attributes?: string[]; children?: Node[]; shadowRoots?: Node[] };
  const cdp = await page.context().newCDPSession(page);
  const { root } = await cdp.send('DOM.getDocument', { depth: -1, pierce: true }) as { root: Node };
  const attribute = (node: Node, name: string) => { const list = node.attributes ?? []; const at = list.indexOf(name); return at % 2 === 0 ? list[at + 1] : undefined; };
  const find = (node: Node, match: (node: Node) => boolean): Node | undefined => {
    if (match(node)) return node;
    for (const next of [...(node.shadowRoots ?? []), ...(node.children ?? [])]) { const found = find(next, match); if (found) return found; }
    return undefined;
  };
  const input = find(root, (node) => attribute(node, 'aria-label') === label);
  const placeholder = input && find(input, (node) => attribute(node, 'pseudo') === '-webkit-input-placeholder');
  expect(placeholder, 'the input has a placeholder box').toBeDefined();
  const { model } = await cdp.send('DOM.getBoxModel', { nodeId: placeholder!.nodeId }) as { model: { content: number[] } };
  await cdp.send('CSS.enable');
  const { computedStyle } = await cdp.send('CSS.getComputedStyleForNode', { nodeId: placeholder!.nodeId }) as { computedStyle: Array<{ name: string; value: string }> };
  await cdp.detach();
  return { width: model.content[2] - model.content[0], textOverflow: computedStyle.find((entry) => entry.name === 'text-overflow')?.value ?? '' };
}

const box = async (locator: Locator) => {
  const found = await locator.boundingBox();
  expect(found, 'the element is on screen').not.toBeNull();
  return found!;
};

// The text of an element in lines: a wrapped address or a pill squeezed to two lines has more than one.
const linesOf = (locator: Locator) => locator.evaluate((element) => {
  const range = document.createRange();
  range.selectNodeContents(element);
  return new Set(Array.from(range.getClientRects()).filter((rect) => rect.width > 0).map((rect) => Math.round(rect.top))).size;
});

test.describe('at 1440 px', () => {
  test.use({ viewport: { width: 1440, height: 900 } });

  test('with nothing open the inbox and Mentions show no reply box and no details, and the open setup guide stays over the list', async ({ page }) => {
    await newWorkspace(page, 'Empty Panes');
    await page.goto(`${base}/`);
    const guide = page.getByRole('region', { name: 'Finish setting up' });
    await expect(guide).toBeVisible();
    const center = page.locator('.conversation-center');
    await expect(center).toContainText('Nothing to open here.');
    await expect(page.getByLabel('Message body')).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Send reply' })).toHaveCount(0);
    await expect(page.getByRole('complementary', { name: 'Conversation details' })).toHaveCount(0);
    // The thread pane takes the room the details would have had.
    const list = await box(page.locator('.list-pane'));
    expect.soft(Math.round((await box(center)).x + (await box(center)).width), 'the thread pane runs to the right edge of the inbox')
      .toBe(Math.round((await box(page.locator('.inbox-shell'))).x + (await box(page.locator('.inbox-shell'))).width));
    const open = await box(guide);
    expect.soft(open.x + open.width, 'the open guide ends inside the list column').toBeLessThanOrEqual(list.x + list.width + 0.5);
    await page.screenshot({ path: test.info().outputPath('inbox-nothing-open-1440.png') });

    await page.goto(`${base}/mentions`);
    await expect(page.getByRole('region', { name: 'Mentions' })).toBeVisible();
    await expect(page.getByLabel('Message body')).toHaveCount(0);
    await expect(page.getByRole('complementary', { name: 'Conversation details' })).toHaveCount(0);
    await page.screenshot({ path: test.info().outputPath('mentions-nothing-open-1440.png') });
  });

  test('a conversation keeps its form and address on one line, the guide never hides a row, and the search box shows its whole placeholder', async ({ page }) => {
    const suffix = await newWorkspace(page, 'Header Panes');
    await formConversations(page, suffix, 10);
    await page.goto(`${base}/`);
    const rows = page.locator('.conversation-scroll > .conversation-item');
    await expect(rows).toHaveCount(10);

    // The search box reads "Search conversations" in full: its placeholder's box (inside the input) is as wide as
    // the words.
    const search = page.getByRole('searchbox', { name: 'Search conversations' });
    const words = await search.evaluate((input: HTMLInputElement) => {
      const style = getComputedStyle(input);
      const context = document.createElement('canvas').getContext('2d')!;
      context.font = `${style.fontStyle} ${style.fontWeight} ${style.fontSize} ${style.fontFamily}`;
      return context.measureText(input.placeholder).width;
    });
    expect.soft(words, 'the placeholder fits its box').toBeLessThanOrEqual((await placeholderOf(page, 'Search conversations')).width);
    // The narrower list of a 1280 px laptop cuts it with an ellipsis, never mid-word without a mark.
    await page.setViewportSize({ width: 1280, height: 900 });
    const narrow = await placeholderOf(page, 'Search conversations');
    expect.soft(words > narrow.width ? narrow.textOverflow : 'ellipsis', 'a cut placeholder ends with an ellipsis').toBe('ellipsis');
    await page.screenshot({ path: test.info().outputPath('search-1280.png'), clip: { x: 0, y: 0, width: 640, height: 160 } });
    await page.setViewportSize({ width: 1440, height: 900 });

    // The header of the first conversation: the form's name in its pill and the customer's address on one line.
    const meta = page.locator('.timeline-meta');
    const pill = meta.locator('.channel-pill');
    const contact = meta.locator('.timeline-contact');
    await expect(pill).toBeVisible();
    expect.soft(await linesOf(pill), 'the form name stays on one line').toBe(1);
    expect.soft(await linesOf(contact), 'the address stays on one line').toBe(1);
    const pillBox = await box(pill);
    const contactBox = await box(contact);
    expect.soft(pillBox.height, 'the pill is one line high, not stretched to an oval').toBeLessThanOrEqual(22);
    expect.soft(Math.abs((pillBox.y + pillBox.height / 2) - (contactBox.y + contactBox.height / 2)), 'the address sits beside the pill').toBeLessThanOrEqual(2);

    // Folded, the guide sits under the list's last row, never over a row.
    const launcher = page.getByRole('button', { name: /Finish setting up/ });
    await expect(launcher).toBeVisible();
    const scroll = await box(page.locator('.conversation-scroll'));
    expect.soft((await box(launcher)).y, 'the folded guide sits below the list rows').toBeGreaterThanOrEqual(scroll.y + scroll.height - 0.5);
    await page.screenshot({ path: test.info().outputPath('conversation-1440.png') });

    // Open, it stays inside the list column.
    await launcher.click();
    const guide = page.getByRole('region', { name: 'Finish setting up' });
    await expect(guide).toBeVisible();
    const list = await box(page.locator('.list-pane'));
    const open = await box(guide);
    expect.soft(open.x + open.width, 'the open guide ends inside the list column').toBeLessThanOrEqual(list.x + list.width + 0.5);
    await page.screenshot({ path: test.info().outputPath('conversation-guide-open-1440.png') });
  });
});

test.describe('at 390 px', () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test('a conversation cuts a long address with an ellipsis and shows the whole notice that replies are not emailed', async ({ page }) => {
    const suffix = await newWorkspace(page, 'Phone Panes');
    await formConversations(page, suffix, 10);
    const list = await (await page.request.get(`${base}/api/v1/conversations?page_size=50`)).json() as { items: Array<{ id: string }> };
    await page.goto(`${base}/conversations/${list.items[0].id}`);
    const meta = page.locator('.timeline-meta');
    const contact = meta.locator('.timeline-contact');
    await expect(contact).toBeVisible();
    const cut = await contact.evaluate((link) => {
      const style = getComputedStyle(link);
      const own = link.getBoundingClientRect();
      const row = link.parentElement!.getBoundingClientRect();
      return { ellipsis: style.textOverflow === 'ellipsis' && link.scrollWidth > link.clientWidth, inside: own.right <= row.right + 0.5 };
    });
    expect.soft(cut.inside, 'the address ends inside its row').toBe(true);
    expect.soft(cut.ellipsis, 'the cut address ends with an ellipsis').toBe(true);

    const notice = page.getByRole('status').filter({ hasText: 'not emailed' });
    await expect(notice).toBeVisible();
    // Every line of its words lies inside the notice: nothing runs past its edge under an ellipsis.
    expect.soft(await notice.evaluate((line) => {
      const range = document.createRange();
      range.selectNodeContents(line.querySelector('.composer-notice-text')!);
      const edge = line.getBoundingClientRect();
      return Array.from(range.getClientRects()).every((rect) => rect.right <= edge.right + 0.5 && rect.bottom <= edge.bottom + 0.5);
    }), 'the notice shows all its words').toBe(true);
    await page.screenshot({ path: test.info().outputPath('conversation-390.png') });

    // The list: once scrolled to its end, the folded guide leaves the last row in sight.
    await page.goto(`${base}/`);
    const rows = page.locator('.conversation-scroll > .conversation-item');
    await expect(rows).toHaveCount(10);
    await page.screenshot({ path: test.info().outputPath('inbox-list-390.png') });
    const launcher = page.getByRole('button', { name: /Finish setting up/ });
    await expect(launcher).toBeVisible();
    await rows.last().scrollIntoViewIfNeeded();
    for (let turn = 0; turn < 4; turn++) await page.mouse.wheel(0, 2000);
    await expect.poll(async () => (await box(rows.last())).y + (await box(rows.last())).height, { message: 'the last row ends above the folded guide' })
      .toBeLessThanOrEqual((await box(launcher)).y + 0.5);
    await page.screenshot({ path: test.info().outputPath('inbox-list-end-390.png') });
  });
});
