import { expect, test } from '@playwright/test';
import type { Locator, Page } from '@playwright/test';
import { servers, uniqueSuffix } from './support/cli';
import { redeemLink } from './support/browser';
import { createWorkspaceWithSlug } from './support/workspace';

// Pages outside the inbox, drawn by a real browser: Knowledge base → Settings fits a phone, a settings page says
// its title once, and the sidebar's resize handle shows where the keyboard is.

async function newDesk(page: Page, label: string): Promise<void> {
  const suffix = uniqueSuffix();
  await redeemLink(page, createWorkspaceWithSlug(`${label} ${suffix}`, `owner-${suffix}@example.com`).url);
}

// visibleHeadings lists the h1 to h3 a sighted person sees in scope; a heading kept for screen readers only
// (one pixel, clipped) is not on screen.
async function visibleHeadings(scope: Locator): Promise<string[]> {
  return scope.evaluate((root) => Array.from(root.querySelectorAll<HTMLElement>('h1, h2, h3')).flatMap((heading) => {
    const box = heading.getBoundingClientRect();
    return box.width > 1 && box.height > 1 && getComputedStyle(heading).visibility !== 'hidden' ? [heading.textContent?.trim() ?? ''] : [];
  }));
}

test('on a 390 px screen Knowledge base → Settings fits the screen, with nothing to scroll sideways', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await newDesk(page, 'Narrow Help Center');
  await page.goto(`${servers.main}/knowledge/settings`);
  const card = page.getByRole('region', { name: 'Help center' });
  await expect(card.locator('.settings-body input').first()).toBeVisible();
  const widths = await page.evaluate(() => {
    const main = document.querySelector('main')!;
    return { page: document.documentElement.scrollWidth, main: main.scrollWidth, mainBox: main.clientWidth, screen: window.innerWidth };
  });
  expect(widths.page, 'the page is no wider than the screen').toBeLessThanOrEqual(widths.screen);
  expect(widths.main, 'the knowledge base frame does not scroll sideways').toBeLessThanOrEqual(widths.mainBox);
  const right = await card.evaluate((section) => Math.max(...Array.from(section.querySelectorAll<HTMLElement>('h2, label, input, select, textarea, button'))
    .map((element) => element.getBoundingClientRect().right)));
  expect(right, 'every field of the help center card ends on screen').toBeLessThanOrEqual(390);
  await page.screenshot({ path: test.info().outputPath('kb-settings-390.png'), fullPage: true });
});

test('General and Saved replies say their title once, and General carries no note about where the help center went', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await newDesk(page, 'Plain Settings');
  const headers = { Origin: servers.main };
  expect((await page.request.post(`${servers.main}/api/v1/saved-replies`, { headers, data: { title: 'Refund issued', body: 'We have issued the refund.' } })).status()).toBe(201);

  await page.goto(`${servers.main}/settings/general`);
  const general = page.locator('main.settings-page');
  await expect(general.getByRole('textbox', { name: 'Workspace name' })).toBeVisible();
  const generalHeadings = await visibleHeadings(general);
  expect(generalHeadings.filter((text) => text === 'General'), 'General is said once').toHaveLength(1);
  await expect(general.getByText('Now in Knowledge.')).toHaveCount(0);
  await expect(general.getByRole('link', { name: 'Open Help center settings' })).toHaveCount(0);
  // The anchor old links use still lands on the section.
  await expect(general.locator('#general-heading')).toHaveCount(1);
  await page.screenshot({ path: test.info().outputPath('settings-general-1440.png') });

  await page.goto(`${servers.main}/settings/saved-replies`);
  const replies = page.locator('main.settings-page');
  await expect(replies.getByText('Refund issued')).toBeVisible();
  const replyHeadings = await visibleHeadings(replies);
  expect(replyHeadings.filter((text) => text === 'Saved replies'), 'Saved replies is said once').toHaveLength(1);
  const lead = 'Text your whole team inserts into a reply.';
  const leads = await replies.evaluate((root, lead) => Array.from(root.querySelectorAll<HTMLElement>('p'))
    .filter((p) => p.getBoundingClientRect().height > 1 && (p.textContent ?? '').includes(lead)).length, lead);
  expect(leads, 'the line under the title is said once').toBe(1);
  await expect(replies.locator('#saved-replies-heading')).toHaveCount(1);
  await page.screenshot({ path: test.info().outputPath('settings-saved-replies-1440.png') });
});

const post = { Origin: servers.main };

// seedHelpCenter adds a collection with a published and a draft article and returns the draft's id.
async function seedHelpCenter(page: Page): Promise<string> {
  const collection = await (await page.request.post(`${servers.main}/api/v1/kb/collections`, { headers: post, data: { name: 'Orders and shipping' } })).json() as { id: string };
  const draft = await (await page.request.post(`${servers.main}/api/v1/kb/articles`, { headers: post, data: { collection_id: collection.id, title: 'Change the address on an order', body_md: 'Write to us.', status: 'draft' } })).json() as { id: string };
  expect((await page.request.post(`${servers.main}/api/v1/kb/articles`, { headers: post, data: { collection_id: collection.id, title: 'Where is my order?', body_md: 'Open Orders.', status: 'published' } })).status()).toBe(201);
  return draft.id;
}

// frameOf measures what a person sees of a knowledge base page's frame: where the title and the tabs sit, how wide
// the tabs run, and the horizontal rules between the tabs and the first control of the page.
async function frameOf(page: Page) {
  return page.evaluate(() => {
    const box = (element: Element) => { const { x, y, width } = element.getBoundingClientRect(); return { x, y, width }; };
    const tabs = document.querySelector<HTMLElement>('main nav.kb-sections')!;
    const first = Array.from(document.querySelectorAll<HTMLElement>('main :is(input, select, textarea, [role="search"], .kb-art-layout)')).find((control) => control.getBoundingClientRect().width > 1)!;
    const tabsBottom = tabs.getBoundingClientRect().bottom;
    const rules = Array.from(document.querySelectorAll<HTMLElement>('main *')).filter((element) => {
      const rect = element.getBoundingClientRect();
      const style = getComputedStyle(element);
      const drawn = parseFloat(style.borderBottomWidth) > 0 && style.borderBottomStyle !== 'none' || parseFloat(style.borderTopWidth) > 0 && style.borderTopStyle !== 'none';
      return drawn && rect.width >= 600 && rect.height < 500 && rect.bottom <= first.getBoundingClientRect().top + 1 && rect.top >= tabsBottom - 40;
    }).length;
    return { title: box(document.querySelector('main h1')!), tabs: box(tabs), rules };
  });
}

test('Knowledge base → Settings sits in the same frame as Articles: same title, same tabs, one rule under them, a form no wider than the page', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await newDesk(page, 'Same Frame');
  await seedHelpCenter(page);

  await page.goto(`${servers.main}/knowledge`);
  await expect(page.getByRole('link', { name: 'Where is my order?' })).toBeVisible();
  const articles = await frameOf(page);
  await page.goto(`${servers.main}/knowledge/settings`);
  await expect(page.getByRole('textbox', { name: 'Title' }).first()).toBeVisible();
  const settings = await frameOf(page);

  expect(settings.title.x, 'the title starts at the same left edge').toBe(articles.title.x);
  expect(settings.title.y, 'the title sits at the same height').toBe(articles.title.y);
  expect(settings.tabs.x, 'the tabs start at the same left edge').toBe(articles.tabs.x);
  expect(settings.tabs.y, 'the tabs sit at the same height').toBe(articles.tabs.y);
  expect(settings.tabs.width, 'the tabs run as wide as on Articles').toBe(articles.tabs.width);
  expect(settings.rules, 'one rule under the tabs, as on Articles').toBe(articles.rules);
  expect(settings.rules).toBe(1);
  const widest = await page.evaluate(() => Math.max(...Array.from(document.querySelectorAll<HTMLElement>('main .settings-card :is(input:not([type="checkbox"]):not([type="radio"]), textarea, select)'))
    .map((field) => field.getBoundingClientRect().width)));
  expect(widest, 'a field keeps the readable width other settings cards use').toBeLessThanOrEqual(800);
  await page.screenshot({ path: test.info().outputPath('kb-settings-1440.png') });
});

for (const width of [1440, 1280]) {
  test(`the status of an article reads in full at ${width} px, while the help center is off and once it is on`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await newDesk(page, `Status ${width}`);
    await seedHelpCenter(page);
    const clipped = () => page.evaluate(() => Array.from(document.querySelectorAll<HTMLElement>('.kb-article-status, .kb-article-status > *'))
      .filter((element) => element.scrollWidth > element.clientWidth || element.scrollHeight > element.clientHeight + 1)
      .map((element) => `${element.textContent} (${element.scrollWidth}x${element.scrollHeight} in ${element.clientWidth}x${element.clientHeight})`));

    await page.goto(`${servers.main}/knowledge`);
    const published = page.locator('.kb-article-row', { hasText: 'Where is my order?' });
    await expect(published).toContainText('not public: help center off');
    expect(await clipped(), 'nothing of a status is cut off').toEqual([]);
    await page.screenshot({ path: test.info().outputPath(`kb-status-off-${width}.png`) });

    expect((await page.request.put(`${servers.main}/api/v1/help-center`, { headers: post, data: { enabled: true, title: '', locale: 'en' } })).status()).toBe(200);
    await page.goto(`${servers.main}/knowledge`);
    await expect(published).toContainText('public');
    await expect(published).not.toContainText('not public');
    expect(await clipped(), 'nothing of a status is cut off with the help center on').toEqual([]);
  });
}

test('on a 390 px screen an article opens on its title and text; visibility, AI use and placement follow', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await newDesk(page, 'Narrow Article');
  const id = await seedHelpCenter(page);
  await page.goto(`${servers.main}/knowledge/articles/${id}`);
  const title = page.getByRole('textbox', { name: 'Title', exact: true });
  await expect(title).toBeVisible();
  const ai = page.getByRole('heading', { name: /Used by AI|Not used by AI/ });
  await expect(ai).toBeVisible();
  const order = await page.evaluate(() => {
    const field = document.querySelector('.kb-title-field input')!;
    const heading = Array.from(document.querySelectorAll('h3')).find((each) => /AI/.test(each.textContent ?? ''))!;
    const editor = document.querySelector('.kb-editor-card')!;
    return { titleFirst: !!(heading.compareDocumentPosition(field) & Node.DOCUMENT_POSITION_PRECEDING), editorFirst: !!(heading.compareDocumentPosition(editor) & Node.DOCUMENT_POSITION_PRECEDING) };
  });
  expect(order.titleFirst, 'the title comes before the AI box in the page').toBe(true);
  expect(order.editorFirst, 'the editor comes before the AI box in the page').toBe(true);
  const [titleBox, aiBox] = [await title.boundingBox(), await ai.boundingBox()];
  expect(titleBox!.y, 'the title is higher on screen than the AI box').toBeLessThan(aiBox!.y);
  expect(await page.evaluate(() => document.documentElement.scrollWidth), 'nothing scrolls sideways').toBeLessThanOrEqual(390);
  await page.screenshot({ path: test.info().outputPath('article-390.png'), fullPage: true });
});

test('Settings → Data: the Export card is one row, the title and what it does beside the button, with no empty rule under it', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await newDesk(page, 'Export Row');
  await page.goto(`${servers.main}/settings/data`);
  const card = page.locator('.data-export');
  await expect(card.getByRole('button', { name: 'Export all data', exact: true })).toBeVisible();
  const row = await card.evaluate((section) => {
    const rect = (element: Element) => element.getBoundingClientRect();
    // The heading's tip is a button too; the row's button is the one that exports.
    const button = Array.from(section.querySelectorAll('button')).find((candidate) => candidate.textContent?.trim() === 'Export all data')!;
    const title = section.querySelector('h2')!;
    const rules = Array.from(section.querySelectorAll<HTMLElement>('*')).filter((element) => {
      const style = getComputedStyle(element);
      return rect(element).width > 200 && (parseFloat(style.borderBottomWidth) > 0 && style.borderBottomStyle !== 'none' || parseFloat(style.borderTopWidth) > 0 && style.borderTopStyle !== 'none');
    }).length;
    return { rules, buttonLeft: rect(button).left, titleRight: rect(title).right, buttonMid: (rect(button).top + rect(button).bottom) / 2, titleTop: rect(title).top, cardTop: rect(section).top, cardBottom: rect(section).bottom };
  });
  expect(row.rules, 'no rule inside the card').toBe(0);
  expect(row.buttonLeft, 'the button stands to the right of the title').toBeGreaterThan(row.titleRight);
  expect(row.buttonMid, 'and level with the card, not under the title').toBeLessThan(row.cardBottom);
  expect(row.buttonMid - row.cardTop, 'the card is one row tall: the button is near its top').toBeLessThan(80);
  await page.screenshot({ path: test.info().outputPath('data-export-1440.png') });
});

for (const [width, least] of [[1440, 32], [390, 44]] as const) {
  test(`Settings → Members: the role buttons are as tall as the buttons around them at ${width} px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await newDesk(page, `Role Buttons ${width}`);
    await page.goto(`${servers.main}/settings/members`);
    const actions = page.locator('.role-row-actions a');
    await expect(actions.first()).toBeVisible();
    const heights = await actions.evaluateAll((links) => links.map((link) => ({ name: link.textContent ?? '', height: link.getBoundingClientRect().height })));
    expect(heights.length).toBeGreaterThan(1);
    for (const { name, height } of heights) expect(height, `${name} is at least ${least}px tall`).toBeGreaterThanOrEqual(least);
    await page.screenshot({ path: test.info().outputPath(`members-${width}.png`), fullPage: true });
  });
}

test('on a 390 px screen the knowledge base pages, an article, Data and Members do not scroll sideways', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await newDesk(page, 'Narrow Pages');
  const id = await seedHelpCenter(page);
  for (const path of ['/knowledge', '/knowledge/settings', `/knowledge/articles/${id}`, '/settings/data', '/settings/members']) {
    await page.goto(`${servers.main}${path}`);
    await expect(page.locator('main').first()).toBeVisible();
    await page.waitForTimeout(500);
    const widths = await page.evaluate(() => ({ page: document.documentElement.scrollWidth, main: Math.max(...Array.from(document.querySelectorAll('main')).map((main) => main.scrollWidth - main.clientWidth)) }));
    expect(widths.page, `${path}: the page is no wider than the screen`).toBeLessThanOrEqual(390);
    expect(widths.main, `${path}: main does not scroll sideways`).toBeLessThanOrEqual(0);
  }
});

test('the sidebar resize handle shows the focus ring other controls show when the keyboard reaches it', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await newDesk(page, 'Focus Desk');
  await page.goto(`${servers.main}/settings/account`);
  const handle = page.getByRole('separator', { name: 'Resize sidebar' });
  await expect(handle).toBeAttached();
  for (let step = 0; step < 60 && !(await handle.evaluate((element) => element === document.activeElement)); step += 1) await page.keyboard.press('Tab');
  await expect(handle).toBeFocused();
  const ring = await handle.evaluate((element) => {
    const probe = document.createElement('span');
    probe.style.color = 'var(--focus)';
    document.body.append(probe);
    const focus = getComputedStyle(probe).color;
    probe.remove();
    const style = getComputedStyle(element);
    return { style: style.outlineStyle, width: parseFloat(style.outlineWidth), color: style.outlineColor, focus };
  });
  expect(ring.style, 'an outline is drawn').not.toBe('none');
  expect(ring.width, 'as wide as other controls draw it').toBeGreaterThanOrEqual(2);
  expect(ring.color, 'in the focus colour').toBe(ring.focus);
  await page.screenshot({ path: test.info().outputPath('sidebar-resizer-focus-1440.png'), clip: { x: 0, y: 0, width: 600, height: 400 } });
});
